package main

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	kmsapi "k8s.io/kms/apis/v2"
)

const (
	probeTestPeerMismatch = "peer-mismatch"
	probeTestKeyID        = "obk2.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	probeTestSensitive    = "sensitive-probe-fixture"
)

func TestLiveSocketProbe(t *testing.T) {
	for _, scenario := range []string{
		"healthy", "unhealthy", "wrong-api", "bad-key", "encrypt-error", "empty-ciphertext",
		"key-changed", "decrypt-error", "wrong-plaintext", probeTestPeerMismatch, "timeout",
	} {
		t.Run(scenario, func(t *testing.T) {
			server := &probeFixtureServer{scenario: scenario}
			socket := startProbeFixture(t, server)
			args := []string{"probe", "--socket", socket, "--output", "json", "--timeout", "1s"}
			switch scenario {
			case probeTestPeerMismatch:
				args = append(args, "--expected-key-id", "obk2.BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBA")
			case "healthy":
				args = append(args, "--expected-key-id", probeTestKeyID)
			}
			started := time.Now()
			output, err := executeCommand(t, args...)
			if scenario == "healthy" {
				if err != nil || !strings.Contains(output, probeTestKeyID) || !strings.Contains(output, "kms.decrypt") {
					t.Fatalf("live socket probe failed: %v: %s", err, output)
				}
				if !strings.Contains(output, "caller owns the socket") {
					t.Fatal("owner probe did not disclose that group access was untested")
				}
			} else if err == nil {
				t.Fatalf("invalid provider passed the probe: %s", output)
			}
			if strings.Contains(output, probeTestSensitive) {
				t.Fatalf("probe exposed a provider response: %s", output)
			}
			if scenario == "timeout" && time.Since(started) > 3*time.Second {
				t.Fatal("probe ignored its deadline")
			}
			server.mu.Lock()
			defer server.mu.Unlock()
			if scenario == probeTestPeerMismatch && server.encryptCalls != 0 {
				t.Fatal("probe encrypted after peer key mismatch")
			}
		})
	}
}

func TestProbeRejectsMissingFilesAndSymlinks(t *testing.T) {
	dir := t.TempDir()
	regular := filepath.Join(dir, "regular")
	if err := os.WriteFile(regular, []byte("not a socket"), 0o600); err != nil {
		t.Fatal(err)
	}
	socket := startProbeFixture(t, &probeFixtureServer{scenario: "healthy"})
	link := filepath.Join(dir, "link")
	if err := os.Symlink(socket, link); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{regular, link, filepath.Join(dir, "missing")} {
		output, err := executeCommand(t, "probe", "--socket", path)
		if err == nil || !strings.Contains(output, "[fail] socket.path") {
			t.Fatalf("accepted an invalid socket: %v: %s", err, output)
		}
	}
	for _, args := range [][]string{{"--socket", "relative"}, {"--timeout", "0s"}, {"--expected-key-id", "invalid"}} {
		if output, err := executeCommand(t, append([]string{"probe"}, args...)...); err == nil {
			t.Fatalf("accepted invalid probe options: %s", output)
		}
	}
}

func startProbeFixture(t *testing.T, fixture *probeFixtureServer) string {
	t.Helper()
	// Keep the Unix path below the platform limit even when a test name is long.
	dir, err := os.MkdirTemp("", "kms-probe-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "kms.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	kmsapi.RegisterKeyManagementServiceServer(server, fixture)
	t.Cleanup(server.Stop)
	go func() { _ = server.Serve(listener) }()
	return socket
}

type probeFixtureServer struct {
	kmsapi.UnimplementedKeyManagementServiceServer
	mu           sync.Mutex
	plaintext    []byte
	scenario     string
	encryptCalls int
}

func (f *probeFixtureServer) Status(ctx context.Context, _ *kmsapi.StatusRequest) (*kmsapi.StatusResponse, error) {
	response := &kmsapi.StatusResponse{Version: "v2", Healthz: "ok", KeyId: probeTestKeyID}
	switch f.scenario {
	case "timeout":
		<-ctx.Done()
		return nil, ctx.Err()
	case "unhealthy":
		response.Healthz = probeTestSensitive
	case "wrong-api":
		response.Version = "v1"
	case "bad-key":
		response.KeyId = probeTestSensitive
	}
	return response, nil
}

func (f *probeFixtureServer) Encrypt(_ context.Context, req *kmsapi.EncryptRequest) (*kmsapi.EncryptResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.encryptCalls++
	if f.scenario == "encrypt-error" {
		return nil, grpcstatus.Error(codes.PermissionDenied, probeTestSensitive)
	}
	if f.scenario == probeTestPeerMismatch {
		return nil, errors.New("unexpected Encrypt after peer mismatch")
	}
	f.plaintext = append([]byte(nil), req.GetPlaintext()...)
	response := &kmsapi.EncryptResponse{
		KeyId: probeTestKeyID, Ciphertext: []byte(probeTestSensitive),
		Annotations: map[string][]byte{"probe.example": []byte("fixture")},
	}
	if f.scenario == "empty-ciphertext" {
		response.Ciphertext = nil
	}
	if f.scenario == "key-changed" {
		response.KeyId = probeTestSensitive
	}
	return response, nil
}

func (f *probeFixtureServer) Decrypt(_ context.Context, req *kmsapi.DecryptRequest) (*kmsapi.DecryptResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.scenario == "decrypt-error" {
		return nil, grpcstatus.Error(codes.Internal, probeTestSensitive)
	}
	if string(req.GetCiphertext()) != probeTestSensitive || req.GetKeyId() != probeTestKeyID ||
		string(req.GetAnnotations()["probe.example"]) != "fixture" {
		return nil, errors.New("probe did not preserve its own Encrypt response")
	}
	if f.scenario == "wrong-plaintext" {
		return &kmsapi.DecryptResponse{Plaintext: []byte(probeTestSensitive)}, nil
	}
	return &kmsapi.DecryptResponse{Plaintext: f.plaintext}, nil
}
