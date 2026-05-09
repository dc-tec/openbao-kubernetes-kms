package status_test

import (
	"errors"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/status"
)

func TestRotationPromotesAfterStableObservationAndActivationDelay(t *testing.T) {
	clock := newFakeClock()
	observer := newTestObserver(t, clock, 3, 2*time.Minute)
	profileV1 := profileForLatest(1, clock.Now())
	profileV2 := profileForLatest(2, clock.Now())
	state := rebuildState(t, observer, profileV1, clock.Now())

	first, err := observer.Observe(state, profileV2, clock.Now())
	if err != nil {
		t.Fatalf("first observe v2: %v", err)
	}
	assertActiveVersion(t, first.State, 1)
	assertPendingCount(t, first.State, 1)

	clock.Advance(30 * time.Second)
	second, err := observer.Observe(first.State, profileV2, clock.Now())
	if err != nil {
		t.Fatalf("second observe v2: %v", err)
	}
	assertActiveVersion(t, second.State, 1)
	assertPendingCount(t, second.State, 2)

	clock.Advance(30 * time.Second)
	third, err := observer.Observe(second.State, profileV2, clock.Now())
	if err != nil {
		t.Fatalf("third observe v2: %v", err)
	}
	assertActiveVersion(t, third.State, 1)
	assertPendingCount(t, third.State, 3)
	if third.Promoted {
		t.Fatal("did not expect promotion before activation delay")
	}

	clock.Advance(2 * time.Minute)
	promoted, err := observer.Observe(third.State, profileV2, clock.Now())
	if err != nil {
		t.Fatalf("observe after activation delay: %v", err)
	}
	if !promoted.Promoted {
		t.Fatal("expected promotion after stable observations and activation delay")
	}
	assertActiveVersion(t, promoted.State, 2)
	assertRetiredVersion(t, promoted.State, 1)

	repeated, err := observer.Observe(promoted.State, profileV2, clock.Now())
	if err != nil {
		t.Fatalf("repeat active observation: %v", err)
	}
	if repeated.Changed || repeated.Promoted {
		t.Fatal("expected repeated active observation to be stable")
	}
}

func TestRotationRollbackClearsPendingAndRejectsActiveRollback(t *testing.T) {
	clock := newFakeClock()
	observer := newTestObserver(t, clock, 3, 0)
	profileV1 := profileForLatest(1, clock.Now())
	profileV2 := profileForLatest(2, clock.Now())
	state := rebuildState(t, observer, profileV1, clock.Now())

	first, err := observer.Observe(state, profileV2, clock.Now())
	if err != nil {
		t.Fatalf("first observe v2: %v", err)
	}
	assertPendingCount(t, first.State, 1)

	rolledBack, err := observer.Observe(first.State, profileV1, clock.Now())
	if err != nil {
		t.Fatalf("observe active version after pending: %v", err)
	}
	if !rolledBack.Changed {
		t.Fatal("expected active-version observation to clear pending rotation")
	}
	assertNoPending(t, rolledBack.State)

	restarted, err := observer.Observe(rolledBack.State, profileV2, clock.Now())
	if err != nil {
		t.Fatalf("observe v2 after pending clear: %v", err)
	}
	assertPendingCount(t, restarted.State, 1)

	fastObserver := newTestObserver(t, clock, 1, 0)
	pending, err := fastObserver.Observe(rolledBack.State, profileV2, clock.Now())
	if err != nil {
		t.Fatalf("fast observe v2: %v", err)
	}
	promoted, err := fastObserver.Observe(pending.State, profileV2, clock.Now())
	if err != nil {
		t.Fatalf("fast promote v2: %v", err)
	}
	assertActiveVersion(t, promoted.State, 2)

	_, err = fastObserver.Observe(promoted.State, profileV1, clock.Now())
	if !errors.Is(err, status.ErrVersionRollback) {
		t.Fatalf("expected active rollback rejection, got %v", err)
	}
}

func TestRotationRestartDuringPendingUsesPersistedObservationCount(t *testing.T) {
	clock := newFakeClock()
	observer := newTestObserver(t, clock, 3, time.Minute)
	profileV1 := profileForLatest(1, clock.Now())
	profileV2 := profileForLatest(2, clock.Now())
	state := rebuildState(t, observer, profileV1, clock.Now())

	first, err := observer.Observe(state, profileV2, clock.Now())
	if err != nil {
		t.Fatalf("first observe v2: %v", err)
	}
	second, err := observer.Observe(first.State, profileV2, clock.Now())
	if err != nil {
		t.Fatalf("second observe v2: %v", err)
	}
	assertPendingCount(t, second.State, 2)

	restartedObserver := newTestObserver(t, clock, 3, time.Minute)
	third, err := restartedObserver.Observe(second.State, profileV2, clock.Now())
	if err != nil {
		t.Fatalf("third observe after restart: %v", err)
	}
	assertPendingCount(t, third.State, 3)
	if third.Promoted {
		t.Fatal("did not expect immediate promotion before restarted activation delay")
	}

	clock.Advance(time.Minute)
	promoted, err := restartedObserver.Observe(third.State, profileV2, clock.Now())
	if err != nil {
		t.Fatalf("observe after delay: %v", err)
	}
	if !promoted.Promoted {
		t.Fatal("expected promotion using persisted observation count")
	}
	assertActiveVersion(t, promoted.State, 2)
}

func TestRotationRejectsMetadataThatCannotServeActiveVersion(t *testing.T) {
	clock := newFakeClock()
	observer := newTestObserver(t, clock, 3, time.Minute)
	profileV1 := profileForLatest(1, clock.Now())
	state := rebuildState(t, observer, profileV1, clock.Now())
	profileV2 := profileForLatest(2, clock.Now())
	profileV2.MinEncryptionVersion = 2

	_, err := observer.Observe(state, profileV2, clock.Now())
	if !errors.Is(err, status.ErrTransitKeyUnusable) {
		t.Fatalf("expected active version unusable error, got %v", err)
	}
}

func assertActiveVersion(t *testing.T, state keyregistry.StateFile, version int) {
	t.Helper()

	active, err := state.ActiveSnapshot()
	if err != nil {
		t.Fatalf("active snapshot: %v", err)
	}
	if active.TransitVersion != version {
		t.Fatalf("unexpected active version: want %d got %d", version, active.TransitVersion)
	}
}

func assertPendingCount(t *testing.T, state keyregistry.StateFile, count int) {
	t.Helper()

	for _, record := range state.Snapshots {
		snapshot, err := record.Snapshot()
		if err != nil {
			t.Fatalf("record snapshot: %v", err)
		}
		if snapshot.State == keyregistry.StatePending {
			if record.StableObservationCount != count {
				t.Fatalf("unexpected pending count: want %d got %d", count, record.StableObservationCount)
			}
			return
		}
	}
	t.Fatal("pending snapshot missing")
}

func assertNoPending(t *testing.T, state keyregistry.StateFile) {
	t.Helper()

	for _, record := range state.Snapshots {
		snapshot, err := record.Snapshot()
		if err != nil {
			t.Fatalf("record snapshot: %v", err)
		}
		if snapshot.State == keyregistry.StatePending {
			t.Fatalf("unexpected pending snapshot for version %d", snapshot.TransitVersion)
		}
	}
}

func assertRetiredVersion(t *testing.T, state keyregistry.StateFile, version int) {
	t.Helper()

	for _, record := range state.Snapshots {
		snapshot, err := record.Snapshot()
		if err != nil {
			t.Fatalf("record snapshot: %v", err)
		}
		if snapshot.TransitVersion == version && snapshot.State == keyregistry.StateRetired {
			return
		}
	}
	t.Fatalf("retired version %d missing", version)
}
