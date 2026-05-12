package status_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/kmsv2"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/status"
)

func TestControllerProbeBuildsPersistsAndDeepProbes(t *testing.T) {
	clock := newFakeClock()
	store := newTestStore(t, clock)
	observer := newTestObserver(t, clock, 3, 2*time.Minute)
	auth := &fakeAuth{}
	transit := &fakeTransit{profile: profileForLatest(1, clock.Now())}
	stateStore := &fakeStateStore{loadErr: keyregistry.ErrStateNotFound}
	controller := newTestController(t, clock, store, observer, auth, transit, stateStore)

	if err := controller.ProbeOnce(context.Background()); err != nil {
		t.Fatalf("probe once: %v", err)
	}
	if auth.refreshCalls != 1 {
		t.Fatalf("expected one auth refresh, got %d", auth.refreshCalls)
	}
	if stateStore.saveCalls != 1 {
		t.Fatalf("expected initial state save, got %d", stateStore.saveCalls)
	}
	current, err := store.Current(context.Background())
	if err != nil {
		t.Fatalf("current status: %v", err)
	}
	if current.Healthz != kmsv2.HealthOK || current.KeyID == "" {
		t.Fatalf("expected healthy status with key ID, got %+v", current)
	}

	if err := controller.DeepProbeOnce(context.Background()); err != nil {
		t.Fatalf("deep probe once: %v", err)
	}
	if transit.deepProbeCalls != 1 {
		t.Fatalf("expected one deep probe, got %d", transit.deepProbeCalls)
	}
	if transit.lastProbe.KeyVersion != 1 {
		t.Fatalf("unexpected deep probe key version: %d", transit.lastProbe.KeyVersion)
	}
	if string(transit.lastProbe.AssociatedData) != "openbao-kubernetes-kms/status-probe/v1" {
		t.Fatalf("unexpected deep probe AAD: %q", string(transit.lastProbe.AssociatedData))
	}
}

func TestControllerMetadataFailureMarksUnhealthyWithoutPromoting(t *testing.T) {
	clock := newFakeClock()
	store := newTestStore(t, clock)
	observer := newTestObserver(t, clock, 1, 0)
	auth := &fakeAuth{}
	transit := &fakeTransit{profile: profileForLatest(1, clock.Now())}
	stateStore := &fakeStateStore{loadErr: keyregistry.ErrStateNotFound}
	controller := newTestController(t, clock, store, observer, auth, transit, stateStore)

	if err := controller.ProbeOnce(context.Background()); err != nil {
		t.Fatalf("initial probe: %v", err)
	}
	initial, err := store.Current(context.Background())
	if err != nil {
		t.Fatalf("initial current status: %v", err)
	}

	transit.readErr = errors.New("metadata unavailable")
	if err := controller.ProbeOnce(context.Background()); !errors.Is(err, status.ErrProbeFailed) {
		t.Fatalf("expected probe failure, got %v", err)
	}
	current, err := store.Current(context.Background())
	if err != nil {
		t.Fatalf("current status after failure: %v", err)
	}
	if current.Healthz != kmsv2.HealthUnhealthy {
		t.Fatalf("expected unhealthy status after metadata failure, got %s", current.Healthz)
	}
	if current.KeyID != "" {
		t.Fatalf("expected unhealthy status to hide key ID, got %s", current.KeyID)
	}
	active, ok := store.Active()
	if !ok {
		t.Fatal("expected active snapshot to remain loaded")
	}
	if active.KubernetesKeyID != initial.Active.KubernetesKeyID {
		t.Fatal("metadata failure changed active snapshot")
	}
	if stateStore.saveCalls != 1 {
		t.Fatalf("metadata failure should not save state, got %d saves", stateStore.saveCalls)
	}
}

func TestControllerFailsClosedWhenStateMissingAfterTransitRotation(t *testing.T) {
	clock := newFakeClock()
	store := newTestStore(t, clock)
	observer := newTestObserver(t, clock, 3, 2*time.Minute)
	auth := &fakeAuth{}
	transit := &fakeTransit{profile: profileForLatest(4, clock.Now())}
	stateStore := &fakeStateStore{loadErr: keyregistry.ErrStateNotFound}
	controller := newTestController(t, clock, store, observer, auth, transit, stateStore)

	err := controller.ProbeOnce(context.Background())
	if !errors.Is(err, status.ErrStateUnavailable) {
		t.Fatalf("expected missing-state recovery failure, got %v", err)
	}
	if stateStore.saveCalls != 0 {
		t.Fatalf("missing rotated state should not be rebuilt, got %d saves", stateStore.saveCalls)
	}
	current, currentErr := store.Current(context.Background())
	if currentErr != nil {
		t.Fatalf("current status: %v", currentErr)
	}
	if current.Healthz != kmsv2.HealthUnhealthy || current.KeyID != "" {
		t.Fatalf("expected unhealthy status without key ID, got %+v", current)
	}
}

func TestControllerAuthRefreshFailureSkipsTransitMetadata(t *testing.T) {
	clock := newFakeClock()
	store := newTestStore(t, clock)
	observer := newTestObserver(t, clock, 3, 2*time.Minute)
	auth := &fakeAuth{err: errors.New("fresh auth failed")}
	transit := &fakeTransit{profile: profileForLatest(1, clock.Now())}
	stateStore := &fakeStateStore{loadErr: keyregistry.ErrStateNotFound}
	controller := newTestController(t, clock, store, observer, auth, transit, stateStore)

	err := controller.ProbeOnce(context.Background())
	if !errors.Is(err, status.ErrProbeFailed) {
		t.Fatalf("expected auth probe failure, got %v", err)
	}
	if transit.readCalls != 0 {
		t.Fatalf("auth failure should skip Transit metadata, got %d reads", transit.readCalls)
	}
	current, currentErr := store.Current(context.Background())
	if currentErr != nil {
		t.Fatalf("current status: %v", currentErr)
	}
	if current.Healthz != kmsv2.HealthUnhealthy {
		t.Fatalf("expected unhealthy status after auth failure, got %s", current.Healthz)
	}
}

func TestControllerCircuitBreakerSkipsProbesWhileOpen(t *testing.T) {
	clock := newFakeClock()
	store := newTestStore(t, clock)
	observer := newTestObserver(t, clock, 1, 0)
	auth := &fakeAuth{}
	transit := &fakeTransit{
		profile: profileForLatest(1, clock.Now()),
		readErr: errors.New("metadata unavailable"),
	}
	stateStore := &fakeStateStore{loadErr: keyregistry.ErrStateNotFound}
	controller := newTestControllerWithOptions(t, status.ControllerOptions{
		Clock:      clock,
		Store:      store,
		Observer:   observer,
		Auth:       auth,
		Transit:    transit,
		StateStore: stateStore,
		MountPath:  "transit",
		KeyName:    "k8s-workload-a-etcd",
		Breaker: status.CircuitBreakerOptions{
			FailureThreshold: 2,
			OpenDuration:     time.Minute,
		},
	})

	for i := 0; i < 2; i++ {
		if err := controller.ProbeOnce(context.Background()); !errors.Is(err, status.ErrProbeFailed) {
			t.Fatalf("expected probe failure %d, got %v", i+1, err)
		}
	}
	if auth.refreshCalls != 2 || transit.readCalls != 2 {
		t.Fatalf("expected two attempted probes, got auth=%d read=%d", auth.refreshCalls, transit.readCalls)
	}

	if err := controller.ProbeOnce(context.Background()); !errors.Is(err, status.ErrCircuitBreakerOpen) {
		t.Fatalf("expected open circuit breaker, got %v", err)
	}
	if auth.refreshCalls != 2 || transit.readCalls != 2 {
		t.Fatalf("open circuit breaker should skip probe, got auth=%d read=%d", auth.refreshCalls, transit.readCalls)
	}
	diagnostics, err := store.Diagnostics(context.Background())
	if err != nil {
		t.Fatalf("diagnostics: %v", err)
	}
	if diagnostics.CircuitBreaker.State != status.CircuitBreakerOpen {
		t.Fatalf("expected open circuit breaker diagnostics, got %s", diagnostics.CircuitBreaker.State)
	}

	clock.Advance(time.Minute)
	transit.readErr = nil
	if err := controller.ProbeOnce(context.Background()); err != nil {
		t.Fatalf("expected probe after breaker window to succeed, got %v", err)
	}
	diagnostics, err = store.Diagnostics(context.Background())
	if err != nil {
		t.Fatalf("diagnostics after recovery: %v", err)
	}
	if diagnostics.CircuitBreaker.State != status.CircuitBreakerClosed {
		t.Fatalf("expected closed circuit breaker after success, got %s", diagnostics.CircuitBreaker.State)
	}
}

func TestControllerLoadsPersistedPendingState(t *testing.T) {
	clock := newFakeClock()
	observer := newTestObserver(t, clock, 2, time.Minute)
	state := rebuildState(t, observer, profileForLatest(1, clock.Now()), clock.Now())
	pending, err := observer.Observe(state, profileForLatest(2, clock.Now()), clock.Now())
	if err != nil {
		t.Fatalf("observe pending: %v", err)
	}

	store := newTestStore(t, clock)
	auth := &fakeAuth{}
	transit := &fakeTransit{profile: profileForLatest(2, clock.Now())}
	stateStore := &fakeStateStore{state: pending.State}
	controller := newTestController(t, clock, store, observer, auth, transit, stateStore)

	if err := controller.ProbeOnce(context.Background()); err != nil {
		t.Fatalf("probe persisted pending state: %v", err)
	}
	saved := stateStore.saved
	assertPendingCount(t, saved, 2)

	clock.Advance(time.Minute)
	if err := controller.ProbeOnce(context.Background()); err != nil {
		t.Fatalf("probe after activation delay: %v", err)
	}
	assertActiveVersion(t, stateStore.saved, 2)
}

func newTestController(
	t *testing.T,
	clock *fakeClock,
	store *status.Store,
	observer *status.Observer,
	auth *fakeAuth,
	transit *fakeTransit,
	stateStore *fakeStateStore,
) *status.Controller {
	t.Helper()

	return newTestControllerWithOptions(t, status.ControllerOptions{
		Clock:      clock,
		Store:      store,
		Observer:   observer,
		Auth:       auth,
		Transit:    transit,
		StateStore: stateStore,
		MountPath:  "transit",
		KeyName:    "k8s-workload-a-etcd",
	})
}

func newTestControllerWithOptions(t *testing.T, opts status.ControllerOptions) *status.Controller {
	t.Helper()

	controller, err := status.NewController(opts)
	if err != nil {
		t.Fatalf("new controller: %v", err)
	}
	return controller
}

type fakeAuth struct {
	refreshCalls int
	err          error
}

func (f *fakeAuth) Refresh(context.Context) error {
	f.refreshCalls++
	return f.err
}

type fakeTransit struct {
	profile        openbao.KeyProfile
	readErr        error
	deepProbeErr   error
	readCalls      int
	deepProbeCalls int
	lastProbe      openbao.ProbeRequest
}

func (f *fakeTransit) ReadKeyProfile(context.Context, string, string) (openbao.KeyProfile, error) {
	f.readCalls++
	if f.readErr != nil {
		return openbao.KeyProfile{}, f.readErr
	}
	return f.profile, nil
}

func (f *fakeTransit) ProbeEncryptDecrypt(_ context.Context, req openbao.ProbeRequest) error {
	f.deepProbeCalls++
	f.lastProbe = req
	return f.deepProbeErr
}

type fakeStateStore struct {
	state     keyregistry.StateFile
	saved     keyregistry.StateFile
	loadErr   error
	saveErr   error
	saveCalls int
}

func (f *fakeStateStore) Load() (keyregistry.StateFile, error) {
	if f.loadErr != nil {
		return keyregistry.StateFile{}, f.loadErr
	}
	return f.state, nil
}

func (f *fakeStateStore) Save(state keyregistry.StateFile) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saveCalls++
	f.saved = state
	f.state = state
	return nil
}
