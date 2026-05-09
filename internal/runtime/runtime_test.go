package runtime_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	kmsapi "k8s.io/kms/apis/v2"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/health"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/kmsv2"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/runtime"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/socket"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/status"
)

const (
	pluginVersion       = "v0.1.0-runtime-test"
	socketBaseName      = "kms.sock"
	parentMode          = os.FileMode(0o750)
	socketMode          = os.FileMode(0o660)
	dialDeadline        = 2 * time.Second
	shutdownDeadline    = 3 * time.Second
	healthLocalAddress  = "127.0.0.1:0"
	healthCacheAge      = 5 * time.Second
	transitVersionFixed = 3
)

func TestRunServesGRPCAndHealthAndShutsDownCleanly(t *testing.T) {
	dir := shortTempDir(t)
	if err := os.Chmod(dir, parentMode); err != nil {
		t.Fatalf("chmod parent: %v", err)
	}
	socketPath := filepath.Join(dir, socketBaseName)

	active := testSnapshot(t)
	cache := &fakeStatusCache{current: kmsv2.CachedStatus{
		Healthz: kmsv2.HealthOK,
		KeyID:   active.KubernetesKeyID,
		Active:  active,
	}}
	registry := mustRegistry(t, active)
	kmsServer := mustKMSServer(t, cache, registry)

	grpcServer := grpc.NewServer()
	kmsv2.Register(grpcServer, kmsServer)

	ready := &fakeReady{diagnostics: status.Diagnostics{
		Healthz:              kmsv2.HealthOK,
		ActiveKeyIDHash:      "redacted-active-hash",
		ActiveTransitVersion: transitVersionFixed,
		CacheAge:             healthCacheAge,
		RotationState:        status.RotationStateActive,
	}}

	rt, err := runtime.New(runtime.Options{
		Socket: socket.Options{
			Path: socketPath,
			Mode: socketMode,
			GID:  -1,
		},
		GRPCServer:      grpcServer,
		Readiness:       ready,
		HealthAddress:   healthLocalAddress,
		ShutdownTimeout: shutdownDeadline,
	})
	if err != nil {
		t.Fatalf("runtime.New: %v", err)
	}

	if rt.SocketPath() != socketPath {
		t.Fatalf("unexpected socket path: %q", rt.SocketPath())
	}
	healthAddr := rt.HealthAddr()
	if healthAddr == nil {
		t.Fatal("expected health listener address")
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	runErr := make(chan error, 1)
	go func() { runErr <- rt.Run(ctx) }()

	dialKMSAndCallStatus(t, socketPath, active.KubernetesKeyID)
	checkHealthEndpoint(t, healthAddr, health.PathLive, http.StatusOK)
	checkHealthEndpoint(t, healthAddr, health.PathReady, http.StatusOK)

	cancel()

	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run returned: %v", err)
		}
	case <-time.After(shutdownDeadline + time.Second):
		t.Fatal("Run did not return within shutdown deadline")
	}

	if _, err := os.Stat(socketPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket file should be unlinked after shutdown, stat err=%v", err)
	}

	conn, err := net.DialTimeout("tcp", healthAddr.String(), 200*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		t.Fatal("health listener still accepting connections after shutdown")
	}
}

func TestRunReadyReturns503AfterStartedButReadinessProbeStale(t *testing.T) {
	dir := shortTempDir(t)
	if err := os.Chmod(dir, parentMode); err != nil {
		t.Fatalf("chmod parent: %v", err)
	}
	socketPath := filepath.Join(dir, socketBaseName)

	active := testSnapshot(t)
	cache := &fakeStatusCache{current: kmsv2.CachedStatus{
		Healthz: kmsv2.HealthOK,
		KeyID:   active.KubernetesKeyID,
		Active:  active,
	}}
	registry := mustRegistry(t, active)
	kmsServer := mustKMSServer(t, cache, registry)

	grpcServer := grpc.NewServer()
	kmsv2.Register(grpcServer, kmsServer)

	ready := &fakeReady{diagnostics: status.Diagnostics{
		Healthz: kmsv2.HealthUnhealthy,
		Stale:   true,
	}}

	rt, err := runtime.New(runtime.Options{
		Socket: socket.Options{
			Path: socketPath,
			Mode: socketMode,
			GID:  -1,
		},
		GRPCServer:      grpcServer,
		Readiness:       ready,
		HealthAddress:   healthLocalAddress,
		ShutdownTimeout: shutdownDeadline,
	})
	if err != nil {
		t.Fatalf("runtime.New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runErr := make(chan error, 1)
	go func() { runErr <- rt.Run(ctx) }()

	checkHealthEndpoint(t, rt.HealthAddr(), health.PathLive, http.StatusOK)
	checkHealthEndpoint(t, rt.HealthAddr(), health.PathReady, http.StatusServiceUnavailable)

	cancel()
	select {
	case <-runErr:
	case <-time.After(shutdownDeadline + time.Second):
		t.Fatal("Run did not return within shutdown deadline")
	}
}

func TestRunReturnsWhenGRPCServerStopsUnexpectedly(t *testing.T) {
	dir := shortTempDir(t)
	if err := os.Chmod(dir, parentMode); err != nil {
		t.Fatalf("chmod parent: %v", err)
	}
	socketPath := filepath.Join(dir, socketBaseName)

	active := testSnapshot(t)
	cache := &fakeStatusCache{current: kmsv2.CachedStatus{
		Healthz: kmsv2.HealthOK,
		KeyID:   active.KubernetesKeyID,
		Active:  active,
	}}
	registry := mustRegistry(t, active)
	kmsServer := mustKMSServer(t, cache, registry)

	grpcServer := grpc.NewServer()
	kmsv2.Register(grpcServer, kmsServer)

	rt, err := runtime.New(runtime.Options{
		Socket: socket.Options{
			Path: socketPath,
			Mode: socketMode,
			GID:  -1,
		},
		GRPCServer:      grpcServer,
		ShutdownTimeout: shutdownDeadline,
	})
	if err != nil {
		t.Fatalf("runtime.New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	runErr := make(chan error, 1)
	go func() { runErr <- rt.Run(ctx) }()

	dialKMSAndCallStatus(t, socketPath, active.KubernetesKeyID)

	// Force the gRPC server to stop on its own. Run must observe this exit,
	// initiate shutdown, and return without waiting for the caller to cancel.
	grpcServer.Stop()

	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run returned unexpected error: %v", err)
		}
	case <-time.After(shutdownDeadline + time.Second):
		t.Fatal("Run did not return after grpc server stopped")
	}

	if _, err := os.Stat(socketPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket file should be unlinked after shutdown, stat err=%v", err)
	}
}

func TestNewRejectsNilGRPCServer(t *testing.T) {
	dir := shortTempDir(t)
	if err := os.Chmod(dir, parentMode); err != nil {
		t.Fatalf("chmod parent: %v", err)
	}
	_, err := runtime.New(runtime.Options{
		Socket: socket.Options{Path: filepath.Join(dir, socketBaseName), Mode: socketMode, GID: -1},
	})
	if !errors.Is(err, runtime.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}

func TestNewRejectsHealthAddressWithoutReadiness(t *testing.T) {
	dir := shortTempDir(t)
	if err := os.Chmod(dir, parentMode); err != nil {
		t.Fatalf("chmod parent: %v", err)
	}
	_, err := runtime.New(runtime.Options{
		Socket:        socket.Options{Path: filepath.Join(dir, socketBaseName), Mode: socketMode, GID: -1},
		GRPCServer:    grpc.NewServer(),
		HealthAddress: healthLocalAddress,
	})
	if !errors.Is(err, runtime.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}

func TestNewPropagatesSocketErrors(t *testing.T) {
	_, err := runtime.New(runtime.Options{
		Socket:     socket.Options{Path: "kms.sock", Mode: socketMode, GID: -1},
		GRPCServer: grpc.NewServer(),
	})
	if !errors.Is(err, socket.ErrInvalidConfig) {
		t.Fatalf("want socket.ErrInvalidConfig, got %v", err)
	}
}

func TestRunWithoutHealthAddress(t *testing.T) {
	dir := shortTempDir(t)
	if err := os.Chmod(dir, parentMode); err != nil {
		t.Fatalf("chmod parent: %v", err)
	}
	socketPath := filepath.Join(dir, socketBaseName)

	active := testSnapshot(t)
	cache := &fakeStatusCache{current: kmsv2.CachedStatus{
		Healthz: kmsv2.HealthOK,
		KeyID:   active.KubernetesKeyID,
		Active:  active,
	}}
	registry := mustRegistry(t, active)
	kmsServer := mustKMSServer(t, cache, registry)

	grpcServer := grpc.NewServer()
	kmsv2.Register(grpcServer, kmsServer)

	rt, err := runtime.New(runtime.Options{
		Socket: socket.Options{
			Path: socketPath,
			Mode: socketMode,
			GID:  -1,
		},
		GRPCServer:      grpcServer,
		ShutdownTimeout: shutdownDeadline,
	})
	if err != nil {
		t.Fatalf("runtime.New: %v", err)
	}
	if rt.HealthAddr() != nil {
		t.Fatal("expected no health address")
	}

	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- rt.Run(ctx) }()

	dialKMSAndCallStatus(t, socketPath, active.KubernetesKeyID)
	cancel()
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run returned: %v", err)
		}
	case <-time.After(shutdownDeadline + time.Second):
		t.Fatal("Run did not return within shutdown deadline")
	}
}

func dialKMSAndCallStatus(t *testing.T, socketPath, expectedKeyID string) {
	t.Helper()

	conn, err := grpc.NewClient(
		"unix://"+socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	client := kmsapi.NewKeyManagementServiceClient(conn)
	deadline := time.Now().Add(dialDeadline)
	var lastErr error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		response, callErr := client.Status(ctx, &kmsapi.StatusRequest{})
		cancel()
		if callErr != nil {
			lastErr = callErr
			time.Sleep(20 * time.Millisecond)
			continue
		}
		if response.GetKeyId() != expectedKeyID {
			t.Fatalf("unexpected key_id: %q", response.GetKeyId())
		}
		return
	}
	t.Fatalf("kms Status never succeeded: %v", lastErr)
}

func checkHealthEndpoint(t *testing.T, addr net.Addr, path string, wantCode int) {
	t.Helper()
	if addr == nil {
		t.Fatal("nil health address")
	}
	url := "http://" + addr.String() + path
	deadline := time.Now().Add(dialDeadline)
	var lastErr error
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
		if err != nil {
			t.Fatalf("build request: %v", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(20 * time.Millisecond)
			continue
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != wantCode {
			t.Fatalf("%s status: want %d got %d", path, wantCode, resp.StatusCode)
		}
		return
	}
	t.Fatalf("%s never reachable: %v", path, lastErr)
}

func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "kmsrt-")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func mustKMSServer(t *testing.T, cache kmsv2.StatusCache, registry keyregistry.Registry) *kmsv2.Server {
	t.Helper()
	server, err := kmsv2.NewServer(kmsv2.Options{
		StatusCache:   cache,
		Registry:      registry,
		Transit:       &fakeTransit{},
		PluginVersion: pluginVersion,
	})
	if err != nil {
		t.Fatalf("kmsv2.NewServer: %v", err)
	}
	return server
}

func mustRegistry(t *testing.T, active keyregistry.KeySnapshot) keyregistry.Registry {
	t.Helper()
	registry, err := keyregistry.NewRegistry(active, nil)
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	return registry
}

func testSnapshot(t *testing.T) keyregistry.KeySnapshot {
	t.Helper()
	snapshot, err := (keyregistry.KeySnapshot{
		ProviderName:            "openbao-kms-runtime",
		ClusterID:               "runtime-cluster",
		OpenBaoInstanceID:       "bao-runtime-a",
		TransitMountID:          "transit-runtime",
		TransitKeyLineageID:     "01HXRUNTIMEKEYLINEAGE",
		TransitVersion:          transitVersionFixed,
		TransitVersionCreatedAt: time.Unix(1_700_000_000, 0).UTC(),
		State:                   keyregistry.StateActive,
		AADMode:                 keyregistry.AADModeRequired,
	}).Normalize()
	if err != nil {
		t.Fatalf("normalize snapshot: %v", err)
	}
	return snapshot
}

type fakeStatusCache struct {
	current kmsv2.CachedStatus
}

func (f *fakeStatusCache) Current(_ context.Context) (kmsv2.CachedStatus, error) {
	return f.current, nil
}

type fakeReady struct {
	diagnostics status.Diagnostics
}

func (f *fakeReady) Ready(_ context.Context) (status.Diagnostics, error) {
	return f.diagnostics, nil
}

type fakeTransit struct{}

func (fakeTransit) Encrypt(
	_ context.Context,
	_ kmsv2.TransitEncryptRequest,
) (kmsv2.TransitEncryptResponse, error) {
	return kmsv2.TransitEncryptResponse{}, errors.New("transit not exercised in this test")
}

func (fakeTransit) Decrypt(
	_ context.Context,
	_ kmsv2.TransitDecryptRequest,
) (kmsv2.TransitDecryptResponse, error) {
	return kmsv2.TransitDecryptResponse{}, errors.New("transit not exercised in this test")
}
