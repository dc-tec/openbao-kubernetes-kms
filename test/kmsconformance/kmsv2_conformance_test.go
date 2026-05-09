package kmsconformance_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/aad"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/kmsv2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	grpcstatus "google.golang.org/grpc/status"
	kmsapi "k8s.io/kms/apis/v2"
)

const (
	conformanceCiphertextPrefix = "vault:v%d:conformance-%d"
	conformancePluginVersion    = "v0.1.0-test"
	conformancePlaintext        = "kubernetes secret payload"
	conformanceRequestUID       = "safe-request-uid"
	conformanceSocketName       = "kms.sock"
	fakeAADMismatchError        = "AAD mismatch"
	fakeUnknownCiphertext       = "unknown ciphertext"
	mismatchedTransitVersion    = "999"
)

func TestKMSV2ProtocolOverUnixSocket(t *testing.T) {
	client, transit, active := startKMSV2Server(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	statusResponse := assertHealthyStatus(t, ctx, client, active)
	assertRepeatedStatusDoesNotCallTransit(t, ctx, client, transit)
	encrypted := assertEncryptOverSocket(t, ctx, client, statusResponse.GetKeyId())
	assertDecryptOverSocket(t, ctx, client, encrypted)
	assertInvalidDecryptRequestsDoNotReachTransit(t, ctx, client, transit, active, encrypted)
}

func assertHealthyStatus(
	t *testing.T,
	ctx context.Context,
	client kmsapi.KeyManagementServiceClient,
	active keyregistry.KeySnapshot,
) *kmsapi.StatusResponse {
	t.Helper()

	statusResponse, err := client.Status(ctx, &kmsapi.StatusRequest{})
	if err != nil {
		t.Fatalf("status over Unix socket: %v", err)
	}
	if statusResponse.GetVersion() != kmsv2.APIVersion {
		t.Fatalf("unexpected version: %s", statusResponse.GetVersion())
	}
	if statusResponse.GetHealthz() != kmsv2.HealthOK {
		t.Fatalf("unexpected healthz: %s", statusResponse.GetHealthz())
	}
	if statusResponse.GetKeyId() != active.KubernetesKeyID {
		t.Fatalf("unexpected key_id: %s", statusResponse.GetKeyId())
	}
	return statusResponse
}

func assertRepeatedStatusDoesNotCallTransit(
	t *testing.T,
	ctx context.Context,
	client kmsapi.KeyManagementServiceClient,
	transit *fakeTransit,
) {
	t.Helper()

	for range 3 {
		if _, err := client.Status(ctx, &kmsapi.StatusRequest{}); err != nil {
			t.Fatalf("repeated status: %v", err)
		}
	}
	if transit.encryptCalls() != 0 || transit.decryptCalls() != 0 {
		t.Fatalf("status called transit: encrypt=%d decrypt=%d", transit.encryptCalls(), transit.decryptCalls())
	}
}

func assertEncryptOverSocket(
	t *testing.T,
	ctx context.Context,
	client kmsapi.KeyManagementServiceClient,
	statusKeyID string,
) *kmsapi.EncryptResponse {
	t.Helper()

	encrypted, err := client.Encrypt(ctx, &kmsapi.EncryptRequest{
		Plaintext: []byte(conformancePlaintext),
		Uid:       conformanceRequestUID,
	})
	if err != nil {
		t.Fatalf("encrypt over Unix socket: %v", err)
	}
	if encrypted.GetKeyId() != statusKeyID {
		t.Fatalf("encrypt key_id %s does not match status key_id %s", encrypted.GetKeyId(), statusKeyID)
	}
	if len(encrypted.GetCiphertext()) == 0 {
		t.Fatal("encrypt returned empty ciphertext")
	}
	if len(encrypted.GetAnnotations()) == 0 {
		t.Fatal("encrypt returned no annotations")
	}
	return encrypted
}

func assertDecryptOverSocket(
	t *testing.T,
	ctx context.Context,
	client kmsapi.KeyManagementServiceClient,
	encrypted *kmsapi.EncryptResponse,
) {
	t.Helper()

	decrypted, err := client.Decrypt(ctx, &kmsapi.DecryptRequest{
		Ciphertext:  encrypted.GetCiphertext(),
		KeyId:       encrypted.GetKeyId(),
		Annotations: encrypted.GetAnnotations(),
	})
	if err != nil {
		t.Fatalf("decrypt over Unix socket: %v", err)
	}
	if string(decrypted.GetPlaintext()) != conformancePlaintext {
		t.Fatalf("unexpected plaintext: %q", decrypted.GetPlaintext())
	}
}

func assertInvalidDecryptRequestsDoNotReachTransit(
	t *testing.T,
	ctx context.Context,
	client kmsapi.KeyManagementServiceClient,
	transit *fakeTransit,
	active keyregistry.KeySnapshot,
	encrypted *kmsapi.EncryptResponse,
) {
	t.Helper()

	unknown := active
	unknown.TransitVersion++
	unknown.KubernetesKeyID = ""
	unknownKeyID, err := keyregistry.DeriveKeyID(unknown)
	if err != nil {
		t.Fatalf("derive unknown key_id: %v", err)
	}
	before := transit.decryptCalls()
	_, err = client.Decrypt(ctx, &kmsapi.DecryptRequest{
		Ciphertext:  encrypted.GetCiphertext(),
		KeyId:       unknownKeyID,
		Annotations: encrypted.GetAnnotations(),
	})
	assertCode(t, err, codes.NotFound)
	if transit.decryptCalls() != before {
		t.Fatalf("unknown key_id reached transit")
	}

	_, err = client.Decrypt(ctx, &kmsapi.DecryptRequest{
		Ciphertext:  encrypted.GetCiphertext(),
		KeyId:       encrypted.GetKeyId(),
		Annotations: nil,
	})
	assertCode(t, err, codes.InvalidArgument)

	modified := cloneProtoAnnotations(encrypted.GetAnnotations())
	modified[aad.KeyTransitKeyVersion] = []byte(mismatchedTransitVersion)
	_, err = client.Decrypt(ctx, &kmsapi.DecryptRequest{
		Ciphertext:  encrypted.GetCiphertext(),
		KeyId:       encrypted.GetKeyId(),
		Annotations: modified,
	})
	assertCode(t, err, codes.InvalidArgument)
}

func startKMSV2Server(t *testing.T) (kmsapi.KeyManagementServiceClient, *fakeTransit, keyregistry.KeySnapshot) {
	t.Helper()

	active := testSnapshot(t)
	registry, err := keyregistry.NewRegistry(active, nil)
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	transit := newFakeTransit()
	server, err := kmsv2.NewServer(kmsv2.Options{
		StatusCache: staticStatusCache{
			status: kmsv2.CachedStatus{
				Healthz: kmsv2.HealthOK,
				KeyID:   active.KubernetesKeyID,
				Active:  active,
			},
		},
		Registry:      registry,
		Transit:       transit,
		PluginVersion: conformancePluginVersion,
	})
	if err != nil {
		t.Fatalf("new KMS v2 server: %v", err)
	}

	socketPath := filepath.Join(t.TempDir(), conformanceSocketName)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen on Unix socket: %v", err)
	}

	grpcServer := grpc.NewServer()
	kmsv2.Register(grpcServer, server)
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- grpcServer.Serve(listener)
	}()
	t.Cleanup(func() {
		grpcServer.Stop()
		select {
		case <-serveDone:
		case <-time.After(time.Second):
			t.Fatal("gRPC server did not stop")
		}
	})

	conn, err := grpc.NewClient("unix://"+socketPath, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial Unix socket: %v", err)
	}
	t.Cleanup(func() {
		if err := conn.Close(); err != nil {
			t.Fatalf("close gRPC client: %v", err)
		}
	})

	return kmsapi.NewKeyManagementServiceClient(conn), transit, active
}

func testSnapshot(t *testing.T) keyregistry.KeySnapshot {
	t.Helper()

	snapshot, err := (keyregistry.KeySnapshot{
		ProviderName:            "openbao-kms-workload-a",
		ClusterID:               "workload-a",
		OpenBaoInstanceID:       "bao-prod-a",
		TransitMountID:          "transit-prod-primary",
		TransitKeyLineageID:     "01HXEXAMPLEKEYLINEAGEID",
		TransitVersion:          3,
		TransitVersionCreatedAt: time.Unix(1_700_000_000, 0).UTC(),
		State:                   keyregistry.StateActive,
		AADMode:                 keyregistry.AADModeRequired,
	}).Normalize()
	if err != nil {
		t.Fatalf("normalize snapshot: %v", err)
	}
	return snapshot
}

func assertCode(t *testing.T, err error, code codes.Code) {
	t.Helper()
	if got := grpcstatus.Code(err); got != code {
		t.Fatalf("unexpected gRPC code: want %s got %s err=%v", code, got, err)
	}
}

func cloneProtoAnnotations(annotations map[string][]byte) map[string][]byte {
	cloned := make(map[string][]byte, len(annotations))
	for key, value := range annotations {
		cloned[key] = bytes.Clone(value)
	}
	return cloned
}

type staticStatusCache struct {
	status kmsv2.CachedStatus
}

func (s staticStatusCache) Current(context.Context) (kmsv2.CachedStatus, error) {
	return s.status, nil
}

type fakeTransit struct {
	mu           sync.Mutex
	records      map[string]transitRecord
	encryptCount int
	decryptCount int
}

type transitRecord struct {
	plaintext      []byte
	associatedData []byte
}

func newFakeTransit() *fakeTransit {
	return &fakeTransit{records: make(map[string]transitRecord)}
}

func (f *fakeTransit) Encrypt(
	_ context.Context,
	request kmsv2.TransitEncryptRequest,
) (kmsv2.TransitEncryptResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.encryptCount++
	ciphertext := []byte(fmt.Sprintf(conformanceCiphertextPrefix, request.KeyVersion, f.encryptCount))
	f.records[string(ciphertext)] = transitRecord{
		plaintext:      bytes.Clone(request.Plaintext),
		associatedData: bytes.Clone(request.AssociatedData),
	}
	return kmsv2.TransitEncryptResponse{
		Ciphertext: ciphertext,
		KeyVersion: request.KeyVersion,
	}, nil
}

func (f *fakeTransit) Decrypt(
	_ context.Context,
	request kmsv2.TransitDecryptRequest,
) (kmsv2.TransitDecryptResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.decryptCount++
	record, ok := f.records[string(request.Ciphertext)]
	if !ok {
		return kmsv2.TransitDecryptResponse{}, errors.New(fakeUnknownCiphertext)
	}
	if !bytes.Equal(record.associatedData, request.AssociatedData) {
		return kmsv2.TransitDecryptResponse{}, errors.New(fakeAADMismatchError)
	}
	return kmsv2.TransitDecryptResponse{Plaintext: bytes.Clone(record.plaintext)}, nil
}

func (f *fakeTransit) encryptCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.encryptCount
}

func (f *fakeTransit) decryptCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.decryptCount
}
