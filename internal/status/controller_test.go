package status_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
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
	transit := &fakeTransit{profile: profileForLatest(1, clock.Now())}
	stateStore := &fakeStateStore{loadErr: keyregistry.ErrStateNotFound}
	controller := newTestController(t, clock, store, observer, transit, stateStore)

	if err := controller.ProbeOnce(context.Background()); err != nil {
		t.Fatalf("probe once: %v", err)
	}
	if stateStore.saveCalls != 1 {
		t.Fatalf("expected initial state save, got %d", stateStore.saveCalls)
	}
	current, err := store.Current(context.Background())
	if err != nil {
		t.Fatalf("current status: %v", err)
	}
	if current.Healthz != kmsv2.HealthUnhealthy || current.KeyID != "" {
		t.Fatalf("expected unhealthy status before the first deep probe, got %+v", current)
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
	current, err = store.Current(context.Background())
	if err != nil {
		t.Fatalf("current status after deep probe: %v", err)
	}
	if current.Healthz != kmsv2.HealthOK || current.KeyID == "" {
		t.Fatalf("expected healthy status after both probes, got %+v", current)
	}
}

func TestControllerDeepProbeRejectsOversizedTransitCiphertext(t *testing.T) {
	clock := newFakeClock()
	store := newTestStore(t, clock)
	observer := newTestObserver(t, clock, 3, 2*time.Minute)
	transit := &fakeTransit{
		profile: profileForLatest(1, clock.Now()),
		deepProbeResult: openbao.ProbeResult{
			Ciphertext: bytes.Repeat([]byte("c"), kmsv2.MaxKMSCiphertextBytes),
			KeyVersion: 1,
		},
	}
	stateStore := &fakeStateStore{loadErr: keyregistry.ErrStateNotFound}
	controller := newTestController(t, clock, store, observer, transit, stateStore)

	if err := controller.ProbeOnce(context.Background()); err != nil {
		t.Fatalf("metadata probe: %v", err)
	}
	err := controller.DeepProbeOnce(context.Background())
	if !errors.Is(err, status.ErrProbeFailed) {
		t.Fatalf("expected deep probe failure, got %v", err)
	}
	current, currentErr := store.Current(context.Background())
	if currentErr != nil {
		t.Fatalf("current status: %v", currentErr)
	}
	if current.Healthz != kmsv2.HealthUnhealthy || current.KeyID != "" {
		t.Fatalf("expected oversized deep probe to mark unhealthy without key_id, got %+v", current)
	}
}

func TestControllerDeepProbeRejectsUnexpectedTransitKeyVersion(t *testing.T) {
	clock := newFakeClock()
	store := newTestStore(t, clock)
	observer := newTestObserver(t, clock, 3, 2*time.Minute)
	transit := &fakeTransit{
		profile: profileForLatest(1, clock.Now()),
		deepProbeResult: openbao.ProbeResult{
			Ciphertext: []byte("vault:v2:test"),
			KeyVersion: 2,
		},
	}
	stateStore := &fakeStateStore{loadErr: keyregistry.ErrStateNotFound}
	controller := newTestController(t, clock, store, observer, transit, stateStore)

	if err := controller.ProbeOnce(context.Background()); err != nil {
		t.Fatalf("metadata probe: %v", err)
	}
	err := controller.DeepProbeOnce(context.Background())
	if !errors.Is(err, status.ErrProbeFailed) {
		t.Fatalf("expected deep probe failure, got %v", err)
	}
	current, currentErr := store.Current(context.Background())
	if currentErr != nil {
		t.Fatalf("current status: %v", currentErr)
	}
	if current.Healthz != kmsv2.HealthUnhealthy || current.KeyID != "" {
		t.Fatalf("expected version mismatch to mark unhealthy without key_id, got %+v", current)
	}
}

func TestControllerProbeFailureRemainsUnhealthyUntilSameProbeRecovers(t *testing.T) {
	tests := []struct {
		name       string
		failedKind status.ProbeKind
	}{
		{name: "deep failure survives metadata success", failedKind: status.ProbeKindDeep},
		{name: "metadata failure survives deep success", failedKind: status.ProbeKindMetadata},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := newFakeClock()
			store := newTestStore(t, clock)
			observer := newTestObserver(t, clock, 3, 2*time.Minute)
			transit := &fakeTransit{profile: profileForLatest(1, clock.Now())}
			stateStore := &fakeStateStore{loadErr: keyregistry.ErrStateNotFound}
			controller := newTestController(t, clock, store, observer, transit, stateStore)

			if err := controller.ProbeOnce(context.Background()); err != nil {
				t.Fatalf("initial metadata probe: %v", err)
			}
			if err := controller.DeepProbeOnce(context.Background()); err != nil {
				t.Fatalf("initial deep probe: %v", err)
			}

			switch tt.failedKind {
			case status.ProbeKindDeep:
				transit.deepProbeErr = errors.New("Transit data path unavailable")
				if err := controller.DeepProbeOnce(context.Background()); !errors.Is(err, status.ErrProbeFailed) {
					t.Fatalf("expected deep probe failure, got %v", err)
				}
				if err := controller.ProbeOnce(context.Background()); err != nil {
					t.Fatalf("metadata probe after deep failure: %v", err)
				}
				transit.deepProbeErr = nil
			case status.ProbeKindMetadata:
				transit.readErr = errors.New("Transit metadata unavailable")
				if err := controller.ProbeOnce(context.Background()); !errors.Is(err, status.ErrProbeFailed) {
					t.Fatalf("expected metadata probe failure, got %v", err)
				}
				if err := controller.DeepProbeOnce(context.Background()); err != nil {
					t.Fatalf("deep probe after metadata failure: %v", err)
				}
				transit.readErr = nil
			}
			assertStoreHealth(t, store, kmsv2.HealthUnhealthy)

			if tt.failedKind == status.ProbeKindDeep {
				if err := controller.DeepProbeOnce(context.Background()); err != nil {
					t.Fatalf("deep probe recovery: %v", err)
				}
			} else if err := controller.ProbeOnce(context.Background()); err != nil {
				t.Fatalf("metadata probe recovery: %v", err)
			}
			assertStoreHealth(t, store, kmsv2.HealthOK)
		})
	}
}

func TestControllerRotationRequiresDeepProbeForPromotedKey(t *testing.T) {
	clock := newFakeClock()
	store := newTestStore(t, clock)
	observer := newTestObserver(t, clock, 1, 0)
	transit := &fakeTransit{profile: profileForLatest(1, clock.Now())}
	stateStore := &fakeStateStore{loadErr: keyregistry.ErrStateNotFound}
	controller := newTestController(t, clock, store, observer, transit, stateStore)

	if err := controller.ProbeOnce(context.Background()); err != nil {
		t.Fatalf("initial metadata probe: %v", err)
	}
	if err := controller.DeepProbeOnce(context.Background()); err != nil {
		t.Fatalf("initial deep probe: %v", err)
	}
	assertStoreHealth(t, store, kmsv2.HealthOK)

	transit.profile = profileForLatest(2, clock.Now())
	if err := controller.ProbeOnce(context.Background()); err != nil {
		t.Fatalf("rotation metadata probe: %v", err)
	}
	assertStoreHealth(t, store, kmsv2.HealthUnhealthy)

	if err := controller.DeepProbeOnce(context.Background()); err != nil {
		t.Fatalf("promoted-key deep probe: %v", err)
	}
	if transit.lastProbe.KeyVersion != 2 {
		t.Fatalf("unexpected promoted deep-probe version: %d", transit.lastProbe.KeyVersion)
	}
	assertStoreHealth(t, store, kmsv2.HealthOK)
}

func TestControllerMetadataFailureMarksUnhealthyWithoutPromoting(t *testing.T) {
	clock := newFakeClock()
	store := newTestStore(t, clock)
	observer := newTestObserver(t, clock, 1, 0)
	transit := &fakeTransit{profile: profileForLatest(1, clock.Now())}
	stateStore := &fakeStateStore{loadErr: keyregistry.ErrStateNotFound}
	controller := newTestController(t, clock, store, observer, transit, stateStore)

	if err := controller.ProbeOnce(context.Background()); err != nil {
		t.Fatalf("initial probe: %v", err)
	}
	if err := controller.DeepProbeOnce(context.Background()); err != nil {
		t.Fatalf("initial deep probe: %v", err)
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
	transit := &fakeTransit{profile: profileForLatest(4, clock.Now())}
	stateStore := &fakeStateStore{loadErr: keyregistry.ErrStateNotFound}
	controller := newTestController(t, clock, store, observer, transit, stateStore)

	err := controller.ProbeOnce(context.Background())
	if !errors.Is(err, status.ErrStateUnavailable) {
		t.Fatalf("expected missing-state recovery failure, got %v", err)
	}
	if !strings.Contains(err.Error(), "latest_version=4") {
		t.Fatalf("expected bootstrap denial reason in error, got %v", err)
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

func TestControllerDisableUpsertFailureSkipsTransitMetadata(t *testing.T) {
	tests := []struct {
		name                       string
		implicitKeyCreationEnabled bool
		readErr                    error
		wantMessage                string
		wantBreakerFailures        int
	}{
		{
			name:                       "implicit key creation enabled",
			implicitKeyCreationEnabled: true,
			wantMessage:                "disable_upsert is false",
		},
		{
			name:                "mount configuration unavailable",
			readErr:             errors.New("mount configuration unavailable"),
			wantMessage:         "disable_upsert read failed",
			wantBreakerFailures: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := newFakeClock()
			store := newTestStore(t, clock)
			observer := newTestObserver(t, clock, 3, 2*time.Minute)
			transit := &fakeTransit{
				profile:                    profileForLatest(1, clock.Now()),
				implicitKeyCreationEnabled: tt.implicitKeyCreationEnabled,
				disableUpsertErr:           tt.readErr,
			}
			stateStore := &fakeStateStore{loadErr: keyregistry.ErrStateNotFound}
			controller := newTestController(t, clock, store, observer, transit, stateStore)

			err := controller.ProbeOnce(context.Background())
			if !errors.Is(err, status.ErrProbeFailed) {
				t.Fatalf("expected probe failure, got %v", err)
			}
			if !strings.Contains(err.Error(), tt.wantMessage) {
				t.Fatalf("expected error to contain %q, got %v", tt.wantMessage, err)
			}
			if transit.disableUpsertCalls != 1 || transit.readCalls != 0 {
				t.Fatalf(
					"unexpected Transit calls: disable_upsert=%d metadata=%d",
					transit.disableUpsertCalls,
					transit.readCalls,
				)
			}
			if stateStore.saveCalls != 0 {
				t.Fatalf("unsafe mount configuration saved registry state %d times", stateStore.saveCalls)
			}
			assertStoreHealth(t, store, kmsv2.HealthUnhealthy)
			diagnostics, diagnosticsErr := store.Diagnostics(context.Background())
			if diagnosticsErr != nil {
				t.Fatalf("diagnostics: %v", diagnosticsErr)
			}
			if diagnostics.CircuitBreaker.ConsecutiveFailures != tt.wantBreakerFailures {
				t.Fatalf(
					"unexpected breaker failures: want %d got %d",
					tt.wantBreakerFailures,
					diagnostics.CircuitBreaker.ConsecutiveFailures,
				)
			}
			if tt.implicitKeyCreationEnabled {
				transit.implicitKeyCreationEnabled = false
				if err := controller.ProbeOnce(context.Background()); err != nil {
					t.Fatalf("metadata probe after disable_upsert recovery: %v", err)
				}
				if err := controller.DeepProbeOnce(context.Background()); err != nil {
					t.Fatalf("deep probe after disable_upsert recovery: %v", err)
				}
				assertStoreHealth(t, store, kmsv2.HealthOK)
			}
		})
	}
}

func TestControllerCircuitBreakerSkipsProbesWhileOpen(t *testing.T) {
	clock := newFakeClock()
	store := newTestStore(t, clock)
	observer := newTestObserver(t, clock, 1, 0)
	transit := &fakeTransit{
		profile: profileForLatest(1, clock.Now()),
		readErr: errors.New("metadata unavailable"),
	}
	stateStore := &fakeStateStore{loadErr: keyregistry.ErrStateNotFound}
	controller := newTestControllerWithOptions(t, status.ControllerOptions{
		Clock:      clock,
		Store:      store,
		Observer:   observer,
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
	if transit.readCalls != 2 {
		t.Fatalf("expected two attempted probes, got %d reads", transit.readCalls)
	}

	if err := controller.ProbeOnce(context.Background()); !errors.Is(err, status.ErrCircuitBreakerOpen) {
		t.Fatalf("expected open circuit breaker, got %v", err)
	}
	if transit.readCalls != 2 {
		t.Fatalf("open circuit breaker should skip probe, got %d reads", transit.readCalls)
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

func TestControllerMetadataSuccessDoesNotResetDeepProbeCircuitBreaker(t *testing.T) {
	clock := newFakeClock()
	store := newTestStore(t, clock)
	observer := newTestObserver(t, clock, 1, 0)
	transit := &fakeTransit{profile: profileForLatest(1, clock.Now())}
	stateStore := &fakeStateStore{loadErr: keyregistry.ErrStateNotFound}
	controller := newTestControllerWithOptions(t, status.ControllerOptions{
		Clock:      clock,
		Store:      store,
		Observer:   observer,
		Transit:    transit,
		StateStore: stateStore,
		MountPath:  "transit",
		KeyName:    "k8s-workload-a-etcd",
		Breaker: status.CircuitBreakerOptions{
			FailureThreshold: 2,
			OpenDuration:     time.Minute,
		},
	})

	if err := controller.ProbeOnce(context.Background()); err != nil {
		t.Fatalf("initial metadata probe: %v", err)
	}
	if err := controller.DeepProbeOnce(context.Background()); err != nil {
		t.Fatalf("initial deep probe: %v", err)
	}

	transit.deepProbeErr = errors.New("Transit data path unavailable")
	for attempt := 1; attempt <= 2; attempt++ {
		if err := controller.DeepProbeOnce(context.Background()); !errors.Is(err, status.ErrProbeFailed) {
			t.Fatalf("expected deep probe failure %d, got %v", attempt, err)
		}
		if err := controller.ProbeOnce(context.Background()); err != nil {
			t.Fatalf("metadata probe between deep failures: %v", err)
		}
	}
	deepCalls := transit.deepProbeCalls
	if err := controller.DeepProbeOnce(context.Background()); !errors.Is(err, status.ErrCircuitBreakerOpen) {
		t.Fatalf("expected open deep-probe circuit breaker, got %v", err)
	}
	if transit.deepProbeCalls != deepCalls {
		t.Fatalf("open deep-probe circuit breaker made a Transit call: before=%d after=%d", deepCalls, transit.deepProbeCalls)
	}
	if err := controller.ProbeOnce(context.Background()); err != nil {
		t.Fatalf("deep-probe breaker blocked metadata probe: %v", err)
	}

	diagnostics, err := store.Diagnostics(context.Background())
	if err != nil {
		t.Fatalf("diagnostics: %v", err)
	}
	if diagnostics.CircuitBreaker.State != status.CircuitBreakerOpen {
		t.Fatalf("expected aggregate breaker to be open, got %s", diagnostics.CircuitBreaker.State)
	}

	clock.Advance(time.Minute)
	transit.deepProbeErr = nil
	if err := controller.DeepProbeOnce(context.Background()); err != nil {
		t.Fatalf("deep probe after breaker window: %v", err)
	}
	assertStoreHealth(t, store, kmsv2.HealthOK)
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
	transit := &fakeTransit{profile: profileForLatest(2, clock.Now())}
	stateStore := &fakeStateStore{state: pending.State}
	controller := newTestController(t, clock, store, observer, transit, stateStore)

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
	transit *fakeTransit,
	stateStore *fakeStateStore,
) *status.Controller {
	t.Helper()

	return newTestControllerWithOptions(t, status.ControllerOptions{
		Clock:      clock,
		Store:      store,
		Observer:   observer,
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

func assertStoreHealth(t testing.TB, store *status.Store, want string) {
	t.Helper()

	current, err := store.Current(context.Background())
	if err != nil {
		t.Fatalf("current status: %v", err)
	}
	if current.Healthz != want {
		t.Fatalf("unexpected health: want %s got %s", want, current.Healthz)
	}
	if want == kmsv2.HealthOK && current.KeyID == "" {
		t.Fatal("healthy status did not include key_id")
	}
	if want != kmsv2.HealthOK && current.KeyID != "" {
		t.Fatalf("unhealthy status included key_id %s", current.KeyID)
	}
}

type fakeTransit struct {
	profile                    openbao.KeyProfile
	readErr                    error
	disableUpsertErr           error
	implicitKeyCreationEnabled bool
	deepProbeErr               error
	deepProbeResult            openbao.ProbeResult
	deepProbeSignal            chan openbao.ProbeRequest
	readCalls                  int
	disableUpsertCalls         int
	deepProbeCalls             int
	lastProbe                  openbao.ProbeRequest
}

func (f *fakeTransit) ReadDisableUpsert(context.Context, string) (bool, error) {
	f.disableUpsertCalls++
	if f.disableUpsertErr != nil {
		return false, f.disableUpsertErr
	}
	return !f.implicitKeyCreationEnabled, nil
}

func (f *fakeTransit) ReadKeyProfile(context.Context, string, string) (openbao.KeyProfile, error) {
	f.readCalls++
	if f.readErr != nil {
		return openbao.KeyProfile{}, f.readErr
	}
	return f.profile, nil
}

func (f *fakeTransit) ProbeEncryptDecrypt(_ context.Context, req openbao.ProbeRequest) (openbao.ProbeResult, error) {
	f.deepProbeCalls++
	f.lastProbe = req
	if f.deepProbeSignal != nil {
		f.deepProbeSignal <- req
	}
	return f.deepProbeResult, f.deepProbeErr
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
