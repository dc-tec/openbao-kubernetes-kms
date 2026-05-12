package kmsv2_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/aad"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/kmsv2"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
	"github.com/dc-tec/openbao-kubernetes-kms/test/fakes"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	kmsapi "k8s.io/kms/apis/v2"
)

const (
	concurrentRequestCount  = 25
	concurrentPayloadFormat = "payload-%d"
	malformedKeyID          = "not-a-key-id"
	mismatchedKeyIDSeed     = "different-key-id"
	pluginVersion           = "v0.1.0-test"
	statusCacheError        = "cache unavailable with sensitive internals"
	testCiphertext          = "ciphertext"
	testPlaintext           = "secret kube payload"
	testRequestUID          = "safe-request-uid"
)

func TestStatusReturnsCachedActiveKeyIDWithoutTransit(t *testing.T) {
	server, cache, transit, active := newTestServer(t)

	for range 5 {
		response, err := server.Status(context.Background(), &kmsapi.StatusRequest{})
		if err != nil {
			t.Fatalf("status: %v", err)
		}
		if response.GetVersion() != kmsv2.APIVersion {
			t.Fatalf("unexpected KMS API version: %s", response.GetVersion())
		}
		if response.GetHealthz() != kmsv2.HealthOK {
			t.Fatalf("unexpected healthz: %s", response.GetHealthz())
		}
		if response.GetKeyId() != active.KubernetesKeyID {
			t.Fatalf("unexpected key_id: %s", response.GetKeyId())
		}
	}

	if cache.Calls() != 5 {
		t.Fatalf("expected status cache to be read 5 times, got %d", cache.Calls())
	}
	if transit.EncryptCalls() != 0 || transit.DecryptCalls() != 0 {
		t.Fatalf("status called transit: encrypt=%d decrypt=%d", transit.EncryptCalls(), transit.DecryptCalls())
	}
}

func TestEncryptReturnsStatusKeyIDAndExplicitTransitVersion(t *testing.T) {
	server, _, transit, active := newTestServer(t)
	plaintext := []byte(testPlaintext)

	response, err := server.Encrypt(context.Background(), &kmsapi.EncryptRequest{
		Plaintext: plaintext,
		Uid:       testRequestUID,
	})
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if response.GetKeyId() != active.KubernetesKeyID {
		t.Fatalf("encrypt key_id does not match active status key_id: %s", response.GetKeyId())
	}
	if len(response.GetCiphertext()) == 0 {
		t.Fatal("encrypt returned empty ciphertext")
	}
	if len(response.GetAnnotations()) == 0 {
		t.Fatal("encrypt returned no annotations")
	}

	lastEncrypt := transit.LastEncrypt()
	if lastEncrypt.KeyVersion != active.TransitVersion {
		t.Fatalf("encrypt used key version %d, want %d", lastEncrypt.KeyVersion, active.TransitVersion)
	}
	if !bytes.Equal(lastEncrypt.Plaintext, plaintext) {
		t.Fatal("transit received wrong plaintext")
	}

	annotations := stringAnnotations(t, response.GetAnnotations())
	canonical, err := aad.BuildCanonical(active, annotations)
	if err != nil {
		t.Fatalf("build canonical AAD: %v", err)
	}
	if !bytes.Equal(lastEncrypt.AssociatedData, canonical) {
		t.Fatal("transit received wrong canonical AAD")
	}
}

func TestDecryptAcceptsEncryptOutput(t *testing.T) {
	server, _, _, _ := newTestServer(t)
	plaintext := []byte(testPlaintext)

	encrypted, err := server.Encrypt(context.Background(), &kmsapi.EncryptRequest{Plaintext: plaintext})
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	decrypted, err := server.Decrypt(context.Background(), &kmsapi.DecryptRequest{
		Ciphertext:  encrypted.GetCiphertext(),
		KeyId:       encrypted.GetKeyId(),
		Annotations: encrypted.GetAnnotations(),
	})
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if !bytes.Equal(decrypted.GetPlaintext(), plaintext) {
		t.Fatalf("decrypt plaintext mismatch:\nwant %q\ngot  %q", plaintext, decrypted.GetPlaintext())
	}
}

func TestDecryptRejectsUnknownKeyIDBeforeTransit(t *testing.T) {
	server, _, transit, active := newTestServer(t)
	unknown := active
	unknown.TransitVersion++
	unknown.KubernetesKeyID = ""
	unknownKeyID, err := keyregistry.DeriveKeyID(unknown)
	if err != nil {
		t.Fatalf("derive unknown key_id: %v", err)
	}

	_, err = server.Decrypt(context.Background(), &kmsapi.DecryptRequest{
		Ciphertext:  []byte(testCiphertext),
		KeyId:       unknownKeyID,
		Annotations: nil,
	})
	assertCode(t, err, codes.NotFound)
	if transit.DecryptCalls() != 0 {
		t.Fatalf("unknown key_id reached transit decrypt %d times", transit.DecryptCalls())
	}
}

func TestObserverReceivesRedactedValidationReasons(t *testing.T) {
	observer := &fakeObserver{}
	server, _, _, active := newTestServerWithOptions(t, kmsv2.Options{Observer: observer})
	unknown := active
	unknown.TransitVersion++
	unknown.KubernetesKeyID = ""
	unknownKeyID, err := keyregistry.DeriveKeyID(unknown)
	if err != nil {
		t.Fatalf("derive unknown key_id: %v", err)
	}

	_, err = server.Decrypt(context.Background(), &kmsapi.DecryptRequest{
		Ciphertext: []byte(testCiphertext),
		KeyId:      unknownKeyID,
	})
	assertCode(t, err, codes.NotFound)

	if len(observer.keyIDReasons) != 1 || observer.keyIDReasons[0] != "key_id_unknown" {
		t.Fatalf("unexpected key ID reasons: %#v", observer.keyIDReasons)
	}
	if len(observer.requests) != 1 {
		t.Fatalf("expected one request observation, got %d", len(observer.requests))
	}
	request := observer.requests[0]
	if request.Method != "decrypt" || request.Status != "not_found" || request.ErrorClass != "key_id_unknown" {
		t.Fatalf("unexpected request observation: %#v", request)
	}
	if request.KeyIDHash == "" || request.KeyIDHash == unknownKeyID {
		t.Fatalf("key ID hash was not redacted: %#v", request)
	}
}

func TestDecryptRejectsMalformedKeyIDBeforeTransit(t *testing.T) {
	server, _, transit, _ := newTestServer(t)

	_, err := server.Decrypt(context.Background(), &kmsapi.DecryptRequest{
		Ciphertext:  []byte(testCiphertext),
		KeyId:       malformedKeyID,
		Annotations: nil,
	})
	assertCode(t, err, codes.InvalidArgument)
	if transit.DecryptCalls() != 0 {
		t.Fatalf("malformed key_id reached transit decrypt %d times", transit.DecryptCalls())
	}
}

func TestDecryptRejectsMissingAndMismatchedAnnotationsBeforeTransit(t *testing.T) {
	server, _, transit, _ := newTestServer(t)
	encrypted, err := server.Encrypt(context.Background(), &kmsapi.EncryptRequest{Plaintext: []byte(testPlaintext)})
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	before := transit.DecryptCalls()
	_, err = server.Decrypt(context.Background(), &kmsapi.DecryptRequest{
		Ciphertext:  encrypted.GetCiphertext(),
		KeyId:       encrypted.GetKeyId(),
		Annotations: nil,
	})
	assertCode(t, err, codes.InvalidArgument)
	if transit.DecryptCalls() != before {
		t.Fatalf("missing annotations reached transit")
	}

	modified := cloneProtoAnnotations(encrypted.GetAnnotations())
	modified[aad.KeyKeyIDHash] = []byte(aad.HashValue(mismatchedKeyIDSeed))
	_, err = server.Decrypt(context.Background(), &kmsapi.DecryptRequest{
		Ciphertext:  encrypted.GetCiphertext(),
		KeyId:       encrypted.GetKeyId(),
		Annotations: modified,
	})
	assertCode(t, err, codes.InvalidArgument)
	if transit.DecryptCalls() != before {
		t.Fatalf("mismatched annotations reached transit")
	}
}

func TestEncryptFailsClosedWithoutHealthyActiveStatus(t *testing.T) {
	server, cache, _, active := newTestServer(t)
	cache.Set(kmsv2.CachedStatus{
		Healthz: kmsv2.HealthUnhealthy,
		KeyID:   active.KubernetesKeyID,
		Active:  active,
	})

	statusResponse, err := server.Status(context.Background(), &kmsapi.StatusRequest{})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if statusResponse.GetKeyId() != "" {
		t.Fatalf("unhealthy status should not expose key_id, got %s", statusResponse.GetKeyId())
	}

	_, err = server.Encrypt(context.Background(), &kmsapi.EncryptRequest{Plaintext: []byte(testPlaintext)})
	assertCode(t, err, codes.FailedPrecondition)
}

func TestStartupWithNoActiveSnapshotFailsClosed(t *testing.T) {
	server, cache, transit, _ := newTestServer(t)
	cache.Set(kmsv2.CachedStatus{Healthz: kmsv2.HealthOK})

	statusResponse, err := server.Status(context.Background(), &kmsapi.StatusRequest{})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if statusResponse.GetHealthz() == kmsv2.HealthOK {
		t.Fatalf("status should be unhealthy without an active snapshot")
	}
	if statusResponse.GetKeyId() != "" {
		t.Fatalf("status should not return key_id without an active snapshot")
	}

	_, err = server.Encrypt(context.Background(), &kmsapi.EncryptRequest{Plaintext: []byte(testPlaintext)})
	assertCode(t, err, codes.FailedPrecondition)
	if transit.EncryptCalls() != 0 {
		t.Fatalf("encrypt without active snapshot reached transit")
	}
}

func TestStatusReportsUnhealthyWhenCacheCannotLoad(t *testing.T) {
	server, cache, _, _ := newTestServer(t)
	cache.SetError(errors.New(statusCacheError))

	response, err := server.Status(context.Background(), &kmsapi.StatusRequest{})
	if err != nil {
		t.Fatalf("status should report unhealthy response, got error: %v", err)
	}
	if response.GetHealthz() != kmsv2.HealthUnhealthy {
		t.Fatalf("unexpected healthz: %s", response.GetHealthz())
	}
	if response.GetKeyId() != "" {
		t.Fatalf("unhealthy status should not expose key_id, got %s", response.GetKeyId())
	}
}

func TestRequestTimeoutCancelsTransitCall(t *testing.T) {
	server, _, transit, _ := newTestServerWithOptions(t, kmsv2.Options{RequestTimeout: 5 * time.Millisecond})
	transit.SetBlockEncrypt(true)

	_, err := server.Encrypt(context.Background(), &kmsapi.EncryptRequest{Plaintext: []byte(testPlaintext)})
	assertCode(t, err, codes.DeadlineExceeded)
	if transit.EncryptCalls() != 1 {
		t.Fatalf("expected one transit encrypt call, got %d", transit.EncryptCalls())
	}
}

func TestStatusUsesRequestTimeout(t *testing.T) {
	server, _, _, _ := newTestServerWithOptions(t, kmsv2.Options{
		RequestTimeout: 5 * time.Millisecond,
		StatusCache:    blockingStatusCache{},
	})

	_, err := server.Status(context.Background(), &kmsapi.StatusRequest{})
	assertCode(t, err, codes.DeadlineExceeded)
}

func TestTransitOpenBaoErrorsPreserveKMSBoundaryClasses(t *testing.T) {
	tests := []struct {
		name      string
		method    string
		class     openbao.ErrorClass
		code      codes.Code
		errorType string
	}{
		{
			name:      "auth failed",
			method:    "encrypt",
			class:     openbao.ErrorClassUnauthenticated,
			code:      codes.Unauthenticated,
			errorType: "auth_failed",
		},
		{
			name:      "policy denied",
			method:    "encrypt",
			class:     openbao.ErrorClassPermissionDenied,
			code:      codes.PermissionDenied,
			errorType: "transit_policy_denied",
		},
		{
			name:      "missing key",
			method:    "encrypt",
			class:     openbao.ErrorClassNotFound,
			code:      codes.NotFound,
			errorType: "transit_key_missing",
		},
		{
			name:      "rate limited",
			method:    "encrypt",
			class:     openbao.ErrorClassRateLimited,
			code:      codes.ResourceExhausted,
			errorType: "openbao_rate_limited",
		},
		{
			name:      "sealed",
			method:    "encrypt",
			class:     openbao.ErrorClassSealed,
			code:      codes.Unavailable,
			errorType: "openbao_sealed",
		},
		{
			name:      "decrypt failed",
			method:    "decrypt",
			class:     openbao.ErrorClassDecryptFailed,
			code:      codes.InvalidArgument,
			errorType: "aad_mismatch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			observer := &fakeObserver{}
			transitErr := &openbao.Error{
				Class:      tt.class,
				StatusCode: 403,
				Operation:  "transit/decrypt/sensitive-key",
			}
			transit := failingTransit{}
			if tt.method == "encrypt" {
				transit.encryptErr = transitErr
			} else {
				transit.decryptErr = transitErr
			}
			server, _, _, active := newTestServerWithOptions(t, kmsv2.Options{
				Transit:  transit,
				Observer: observer,
			})

			var err error
			if tt.method == "encrypt" {
				_, err = server.Encrypt(context.Background(), &kmsapi.EncryptRequest{
					Plaintext: []byte(testPlaintext),
				})
			} else {
				annotations, annotationsErr := aad.BuildAnnotations(active, pluginVersion)
				if annotationsErr != nil {
					t.Fatalf("build annotations: %v", annotationsErr)
				}
				_, err = server.Decrypt(context.Background(), &kmsapi.DecryptRequest{
					Ciphertext:  []byte(testCiphertext),
					KeyId:       active.KubernetesKeyID,
					Annotations: protoAnnotations(annotations),
				})
			}
			assertCode(t, err, tt.code)
			if strings.Contains(err.Error(), "sensitive-key") {
				t.Fatalf("KMS error leaked OpenBao operation path: %v", err)
			}
			if len(observer.requests) != 1 {
				t.Fatalf("expected one request observation, got %d", len(observer.requests))
			}
			if observer.requests[0].ErrorClass != tt.errorType {
				t.Fatalf("unexpected error class: %#v", observer.requests[0])
			}
		})
	}
}

func TestPanicRecoveryReturnsRedactedInternalError(t *testing.T) {
	server, _, transit, _ := newTestServer(t)
	transit.SetPanicEncrypt(true)

	_, err := server.Encrypt(context.Background(), &kmsapi.EncryptRequest{Plaintext: []byte(testPlaintext)})
	assertCode(t, err, codes.Internal)
	if err == nil || bytes.Contains([]byte(err.Error()), []byte(testPlaintext)) {
		t.Fatalf("panic recovery leaked sensitive detail: %v", err)
	}
}

func TestConcurrentEncryptDecrypt(t *testing.T) {
	server, _, _, _ := newTestServer(t)

	var wg sync.WaitGroup
	for i := range concurrentRequestCount {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			plaintext := []byte(fmt.Sprintf(concurrentPayloadFormat, index))
			encrypted, err := server.Encrypt(context.Background(), &kmsapi.EncryptRequest{Plaintext: plaintext})
			if err != nil {
				t.Errorf("encrypt %d: %v", index, err)
				return
			}
			decrypted, err := server.Decrypt(context.Background(), &kmsapi.DecryptRequest{
				Ciphertext:  encrypted.GetCiphertext(),
				KeyId:       encrypted.GetKeyId(),
				Annotations: encrypted.GetAnnotations(),
			})
			if err != nil {
				t.Errorf("decrypt %d: %v", index, err)
				return
			}
			if !bytes.Equal(decrypted.GetPlaintext(), plaintext) {
				t.Errorf("plaintext mismatch for %d", index)
			}
		}(i)
	}
	wg.Wait()
}

func newTestServer(t *testing.T) (*kmsv2.Server, *fakes.StatusCache, *fakes.KMSTransit, keyregistry.KeySnapshot) {
	t.Helper()
	return newTestServerWithOptions(t, kmsv2.Options{})
}

func newTestServerWithOptions(
	t *testing.T,
	overrides kmsv2.Options,
) (*kmsv2.Server, *fakes.StatusCache, *fakes.KMSTransit, keyregistry.KeySnapshot) {
	t.Helper()

	active := testSnapshot(t)
	registry, err := keyregistry.NewRegistry(active, nil)
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	cache := fakes.NewStatusCache(kmsv2.CachedStatus{
		Healthz: kmsv2.HealthOK,
		KeyID:   active.KubernetesKeyID,
		Active:  active,
	})
	transit := fakes.NewKMSTransit()
	options := kmsv2.Options{
		StatusCache:    cache,
		Registry:       registry,
		Transit:        transit,
		PluginVersion:  pluginVersion,
		RequestTimeout: overrides.RequestTimeout,
		Observer:       overrides.Observer,
	}
	if overrides.StatusCache != nil {
		options.StatusCache = overrides.StatusCache
	}
	if overrides.Registry != nil {
		options.Registry = overrides.Registry
	}
	if overrides.Transit != nil {
		options.Transit = overrides.Transit
	}
	if overrides.PluginVersion != "" {
		options.PluginVersion = overrides.PluginVersion
	}
	server, err := kmsv2.NewServer(options)
	if err != nil {
		t.Fatalf("new KMS v2 server: %v", err)
	}
	return server, cache, transit, active
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
		t.Fatalf("normalize test snapshot: %v", err)
	}
	return snapshot
}

func assertCode(t *testing.T, err error, code codes.Code) {
	t.Helper()
	if got := grpcstatus.Code(err); got != code {
		t.Fatalf("unexpected gRPC code: want %s got %s err=%v", code, got, err)
	}
}

func stringAnnotations(t *testing.T, annotations map[string][]byte) map[string]string {
	t.Helper()

	result := make(map[string]string, len(annotations))
	for key, value := range annotations {
		result[key] = string(value)
	}
	return result
}

func cloneProtoAnnotations(annotations map[string][]byte) map[string][]byte {
	cloned := make(map[string][]byte, len(annotations))
	for key, value := range annotations {
		cloned[key] = bytes.Clone(value)
	}
	return cloned
}

func protoAnnotations(annotations map[string]string) map[string][]byte {
	encoded := make(map[string][]byte, len(annotations))
	for key, value := range annotations {
		encoded[key] = []byte(value)
	}
	return encoded
}

type blockingStatusCache struct{}

func (blockingStatusCache) Current(ctx context.Context) (kmsv2.CachedStatus, error) {
	<-ctx.Done()
	return kmsv2.CachedStatus{}, ctx.Err()
}

type failingTransit struct {
	encryptErr error
	decryptErr error
}

func (f failingTransit) Encrypt(
	context.Context,
	kmsv2.TransitEncryptRequest,
) (kmsv2.TransitEncryptResponse, error) {
	return kmsv2.TransitEncryptResponse{}, f.encryptErr
}

func (f failingTransit) Decrypt(
	context.Context,
	kmsv2.TransitDecryptRequest,
) (kmsv2.TransitDecryptResponse, error) {
	return kmsv2.TransitDecryptResponse{}, f.decryptErr
}

type fakeObserver struct {
	requests     []kmsv2.RequestObservation
	aadReasons   []string
	keyIDReasons []string
}

func (f *fakeObserver) ObserveKMSRequest(_ context.Context, observation kmsv2.RequestObservation) {
	f.requests = append(f.requests, observation)
}

func (f *fakeObserver) ObserveAADValidationError(reason string) {
	f.aadReasons = append(f.aadReasons, reason)
}

func (f *fakeObserver) ObserveDecryptKeyIDError(reason string) {
	f.keyIDReasons = append(f.keyIDReasons, reason)
}
