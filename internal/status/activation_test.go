package status_test

import (
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/status"
)

func TestActivationWaitUsesFullPrecisionAndElapsedTime(t *testing.T) {
	for _, jump := range []time.Duration{-time.Hour, 0, time.Hour} {
		t.Run(jump.String(), func(t *testing.T) {
			f := newDiagnosticFixture(t, 1, time.Minute)
			base := f.clock.Now()
			f.probeHealthy(t)
			f.clock.Advance(900 * time.Millisecond)
			f.transit.profile = profileForLatest(2, base)
			probeActiveVersion(t, f.controller, f.store, 1)
			f.clock.JumpWall(jump)
			f.clock.Advance(time.Minute - time.Nanosecond)
			probeActiveVersion(t, f.controller, f.store, 1)
			f.clock.Advance(time.Nanosecond)
			probeActiveVersion(t, f.controller, f.store, 2)
		})
	}
}

func TestRestartedStableCandidateWaitsFullDelayAndKeepsDecryptCoverage(t *testing.T) {
	f := newPendingPersistenceFixture(t, 3)
	before, _ := f.store.State()
	pendingID := pendingKeyID(t, before)
	// A new process gets a new elapsed-time origin. Downtime and a forward
	// correction cannot provide credit towards the new process's delay.
	clock := &fakeClock{now: f.clock.Now().Add(time.Hour)}
	store := newTestStore(t, clock)
	restarted := newTestControllerWithOptions(t, status.ControllerOptions{
		Clock: clock, Store: store, Observer: newTestObserver(t, clock, 3, time.Minute),
		Transit: f.transit, StateStore: f.disk, MountPath: "transit", KeyName: "key",
	})
	if _, err := store.Lookup(pendingID); err != nil {
		t.Fatal("restart lost retained pending decrypt identity", err)
	}
	probeActiveVersion(t, restarted, store, 1)
	clock.Advance(time.Minute - time.Nanosecond)
	probeActiveVersion(t, restarted, store, 1)
	waiting, _ := store.State()
	if waiting.CurrentHash != before.CurrentHash {
		t.Fatal("waiting rewrote persisted timestamps")
	}
	clock.Advance(time.Nanosecond)
	probeActiveVersion(t, restarted, store, 2)
}

func TestActivationWaitIsScopedToCandidateAndNotDiscovery(t *testing.T) {
	f := newDiagnosticFixture(t, 1, time.Minute)
	base := f.clock.Now()
	f.probeHealthy(t)
	f.transit.profile = profileForLatest(2, base)
	peer := rebuildState(t, newTestObserver(t, f.clock, 1, 0), f.transit.profile, base)
	if err := f.controller.RefreshForDecrypt(t.Context(), peer.ActiveKeyID); err != nil {
		t.Fatal(err)
	}
	f.clock.Advance(2 * time.Minute)
	probeActiveVersion(t, f.controller, f.store, 1)
	f.clock.Advance(30 * time.Second)
	f.transit.profile = profileForLatest(3, base)
	probeActiveVersion(t, f.controller, f.store, 1)
	f.clock.Advance(30 * time.Second)
	probeActiveVersion(t, f.controller, f.store, 1)
	f.clock.Advance(30 * time.Second)
	probeActiveVersion(t, f.controller, f.store, 3)
}

func TestClockRegressionPreservesTimestampOrderAndKeyIdentity(t *testing.T) {
	f := newDiagnosticFixture(t, 2, time.Minute)
	base := f.clock.Now()
	f.probeHealthy(t)
	f.transit.profile = profileForLatest(2, base)
	probeActiveVersion(t, f.controller, f.store, 1)
	before, _ := f.store.State()
	pendingID := pendingKeyID(t, before)
	f.clock.JumpWall(-time.Hour)
	probeActiveVersion(t, f.controller, f.store, 1)
	stable, _ := f.store.State()
	if pendingKeyID(t, stable) != pendingID {
		t.Fatal("clock correction changed key identity")
	}
	if f.observations.clockRegressions != 1 {
		t.Fatal("missing bounded clock regression diagnostic")
	}
	f.clock.Advance(time.Minute)
	probeActiveVersion(t, f.controller, f.store, 2)
	promoted, _ := f.store.State()
	if promoted.ActiveKeyID != pendingID {
		t.Fatal("promotion changed pending identity")
	}
	if err := promoted.Validate(); err != nil {
		t.Fatal(err)
	}
	if f.observations.clockRegressions != 2 {
		t.Fatal("missing promotion timestamp diagnostic")
	}
}

func probeActiveVersion(t *testing.T, controller *status.Controller, store *status.Store, version int) {
	t.Helper()
	if err := controller.ProbeOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	state, _ := store.State()
	assertActiveVersion(t, state, version)
}
