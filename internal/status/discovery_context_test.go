package status_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/kmsv2"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/status"
)

type gatedDiscoveryTransit struct {
	*fakeTransit
	started chan struct{}
	release chan struct{}
}

func (f *gatedDiscoveryTransit) ReadKeyProfile(ctx context.Context, mount, key string) (openbao.KeyProfile, error) {
	close(f.started)
	select {
	case <-f.release:
		return f.fakeTransit.ReadKeyProfile(ctx, mount, key)
	case <-ctx.Done():
		return openbao.KeyProfile{}, ctx.Err()
	}
}

func discoveryFixture(t *testing.T, lifecycle context.Context, timeout time.Duration,
) (*status.Controller, *status.Store, *gatedDiscoveryTransit, string) {
	t.Helper()
	clock := newFakeClock()
	store := newTestStore(t, clock)
	observer := newTestObserver(t, clock, 1, 0)
	initial := rebuildState(t, observer, profileForLatest(1, clock.Now()), clock.Now())
	metadata := &gatedDiscoveryTransit{
		fakeTransit: &fakeTransit{profile: profileForLatest(2, clock.Now())},
		started:     make(chan struct{}), release: make(chan struct{}),
	}
	controller := newTestControllerWithOptions(t, status.ControllerOptions{
		Clock: clock, Store: store, Observer: observer, Transit: metadata,
		StateStore: &fakeStateStore{state: initial}, MountPath: "transit", KeyName: "key",
		LifecycleContext: lifecycle, DecryptRefreshTimeout: timeout,
		Breaker: status.CircuitBreakerOptions{FailureThreshold: 1},
	})
	if err := store.PublishHealthy(initial, clock.Read()); err != nil {
		t.Fatal(err)
	}
	keyID := rebuildState(t, observer, metadata.profile, clock.Now()).ActiveKeyID
	return controller, store, metadata, keyID
}

func TestCanceledDiscovererDoesNotPoisonSharedHealth(t *testing.T) {
	controller, store, metadata, keyID := discoveryFixture(t, t.Context(), time.Second)
	ctx, cancel := context.WithCancel(t.Context())
	first := make(chan error, 1)
	go func() { first <- controller.RefreshForDecrypt(ctx, keyID) }()
	<-metadata.started
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatalf("initiator did not return its cancellation: %v", err)
	}
	assertStoreHealth(t, store, kmsv2.HealthOK)
	if store.DiagnosticsSnapshot().CircuitBreaker.ConsecutiveFailures != 0 {
		t.Fatal("caller cancellation counted as backend failure")
	}
	close(metadata.release)
	if err := controller.RefreshForDecrypt(t.Context(), keyID); err != nil {
		t.Fatalf("shared discovery was canceled: %v", err)
	}
	if _, err := store.Lookup(keyID); err != nil {
		t.Fatal("independent caller did not receive the discovered key")
	}
	if metadata.readCalls != 1 {
		t.Fatal("callers did not share one discovery")
	}
	assertStoreHealth(t, store, kmsv2.HealthOK)
}

func TestDiscoveryUsesProviderDeadlineAndShutdown(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		lifecycle, cancel := context.WithCancel(t.Context())
		controller, store, metadata, keyID := discoveryFixture(t, lifecycle, 50*time.Millisecond)
		result := make(chan error, 1)
		go func() { result <- controller.RefreshForDecrypt(t.Context(), keyID) }()
		<-metadata.started
		if shutdown {
			cancel()
		}
		select {
		case err := <-result:
			if err == nil {
				t.Fatal("blocked discovery succeeded")
			}
		case <-time.After(time.Second):
			t.Fatal("discovery outlived its provider deadline or shutdown")
		}
		if !shutdown {
			assertStoreHealth(t, store, kmsv2.HealthUnhealthy)
			if store.DiagnosticsSnapshot().CircuitBreaker.State != status.CircuitBreakerOpen {
				t.Fatal("provider deadline was excluded from backend failure accounting")
			}
		}
		cancel()
	}
}
