package status_test

import (
	"errors"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/status"
)

func TestRotationSequencePromotesOnceAndConverges(t *testing.T) {
	clock := newFakeClock()
	observer := newTestObserver(t, clock, 3, 2*time.Minute)
	profileV1 := profileForLatest(1, clock.Now())
	profileV2 := profileForLatest(2, clock.Now())
	state := rebuildState(t, observer, profileV1, clock.Now())
	assertRotationStateInvariantCatalog(t, state)

	promotions := 0
	state, promotions = observeContractStep(t, observer, state, profileV2, clock.Now(), promotions)
	assertActiveVersion(t, state, 1)
	assertPendingCount(t, state, 1)

	clock.Advance(30 * time.Second)
	state, promotions = observeContractStep(t, observer, state, profileV2, clock.Now(), promotions)
	assertActiveVersion(t, state, 1)
	assertPendingCount(t, state, 2)

	clock.Advance(30 * time.Second)
	state, promotions = observeContractStep(t, observer, state, profileV2, clock.Now(), promotions)
	assertActiveVersion(t, state, 1)
	assertPendingCount(t, state, 3)

	clock.Advance(2 * time.Minute)
	state, promotions = observeContractStep(t, observer, state, profileV2, clock.Now(), promotions)
	assertActiveVersion(t, state, 2)
	assertRetiredVersion(t, state, 1)
	if promotions != 1 {
		t.Fatalf("expected exactly one promotion, got %d", promotions)
	}

	assertConvergedObservationIdempotent(t, observer, state, profileV2, clock.Now())
}

func TestRotationSequenceClearsPendingAndRestartsObservation(t *testing.T) {
	clock := newFakeClock()
	observer := newTestObserver(t, clock, 2, 0)
	profileV1 := profileForLatest(1, clock.Now())
	profileV2 := profileForLatest(2, clock.Now())
	state := rebuildState(t, observer, profileV1, clock.Now())
	promotions := 0

	state, promotions = observeContractStep(t, observer, state, profileV2, clock.Now(), promotions)
	assertActiveVersion(t, state, 1)
	assertPendingCount(t, state, 1)

	state, promotions = observeContractStep(t, observer, state, profileV1, clock.Now(), promotions)
	assertActiveVersion(t, state, 1)
	assertNoPending(t, state)

	state, promotions = observeContractStep(t, observer, state, profileV2, clock.Now(), promotions)
	assertActiveVersion(t, state, 1)
	assertPendingCount(t, state, 1)

	state, promotions = observeContractStep(t, observer, state, profileV2, clock.Now(), promotions)
	assertActiveVersion(t, state, 2)
	assertRetiredVersion(t, state, 1)
	if promotions != 1 {
		t.Fatalf("expected restart sequence to promote once, got %d promotions", promotions)
	}
}

func TestRotationSequenceRestartPreservesPendingObservationState(t *testing.T) {
	clock := newFakeClock()
	observer := newTestObserver(t, clock, 2, time.Minute)
	profileV1 := profileForLatest(1, clock.Now())
	profileV2 := profileForLatest(2, clock.Now())
	state := rebuildState(t, observer, profileV1, clock.Now())
	promotions := 0

	state, promotions = observeContractStep(t, observer, state, profileV2, clock.Now(), promotions)
	assertPendingCount(t, state, 1)

	restarted := newTestObserver(t, clock, 2, time.Minute)
	state, promotions = observeContractStep(t, restarted, state, profileV2, clock.Now(), promotions)
	assertPendingCount(t, state, 2)
	if promotions != 0 {
		t.Fatalf("restart promoted before activation delay: %d", promotions)
	}

	clock.Advance(time.Minute)
	state, promotions = observeContractStep(t, restarted, state, profileV2, clock.Now(), promotions)
	assertActiveVersion(t, state, 2)
	if promotions != 1 {
		t.Fatalf("expected promotion after restarted delay, got %d", promotions)
	}
}

func TestRotationSequenceRejectsRollbackAfterPromotion(t *testing.T) {
	clock := newFakeClock()
	observer := newTestObserver(t, clock, 1, 0)
	profileV1 := profileForLatest(1, clock.Now())
	profileV2 := profileForLatest(2, clock.Now())
	state := rebuildState(t, observer, profileV1, clock.Now())

	state, _ = observeContractStep(t, observer, state, profileV2, clock.Now(), 0)
	assertActiveVersion(t, state, 2)

	_, err := observer.Observe(state, profileV1, clock.Now())
	if !errors.Is(err, status.ErrVersionRollback) {
		t.Fatalf("expected active rollback rejection, got %v", err)
	}
	assertRotationStateInvariantCatalog(t, state)
}

func TestRotationSequenceRejectsOlderMetadataAfterNewerPromotion(t *testing.T) {
	clock := newFakeClock()
	observer := newTestObserver(t, clock, 1, 0)
	profileV1 := profileForLatest(1, clock.Now())
	profileV2 := profileForLatest(2, clock.Now())
	profileV3 := profileForLatest(3, clock.Now())
	state := rebuildState(t, observer, profileV1, clock.Now())
	promotions := 0

	state, promotions = observeContractStep(t, observer, state, profileV2, clock.Now(), promotions)
	state, promotions = observeContractStep(t, observer, state, profileV3, clock.Now(), promotions)
	assertActiveVersion(t, state, 3)
	assertRetiredVersion(t, state, 2)
	if promotions != 2 {
		t.Fatalf("expected two promotions before rollback check, got %d", promotions)
	}

	_, err := observer.Observe(state, profileV2, clock.Now())
	if !errors.Is(err, status.ErrVersionRollback) {
		t.Fatalf("expected stale older metadata rejection, got %v", err)
	}
	assertRotationStateInvariantCatalog(t, state)
}

func TestRotationSequenceRejectsMissingIntermediateVersionMetadata(t *testing.T) {
	clock := newFakeClock()
	observer := newTestObserver(t, clock, 3, time.Minute)
	profileV1 := profileForLatest(1, clock.Now())
	profileV3 := profileForLatest(3, clock.Now())
	profileV3.VersionCreationTimes = []openbao.KeyVersion{
		profileV3.VersionCreationTimes[0],
		profileV3.VersionCreationTimes[2],
	}
	state := rebuildState(t, observer, profileV1, clock.Now())

	_, err := observer.Observe(state, profileV3, clock.Now())
	if !errors.Is(err, status.ErrTransitMetadataInvalid) {
		t.Fatalf("expected missing intermediate metadata to fail closed, got %v", err)
	}
	assertRotationStateInvariantCatalog(t, state)
}

func TestRotationSequenceRetainsSkippedIntermediateVersionsWhenMetadataComplete(t *testing.T) {
	clock := newFakeClock()
	observer := newTestObserver(t, clock, 1, 0)
	profileV1 := profileForLatest(1, clock.Now())
	profileV3 := profileForLatest(3, clock.Now())
	state := rebuildState(t, observer, profileV1, clock.Now())

	state, promotions := observeContractStep(t, observer, state, profileV3, clock.Now(), 0)
	assertActiveVersion(t, state, 3)
	assertRetiredVersion(t, state, 2)
	assertRetiredVersion(t, state, 1)
	if promotions != 1 {
		t.Fatalf("expected direct v3 observation to promote once, got %d", promotions)
	}
}

func TestRotationSequenceRejectsBlockedHistoricalVersionAfterPromotion(t *testing.T) {
	clock := newFakeClock()
	observer := newTestObserver(t, clock, 1, 0)
	profileV1 := profileForLatest(1, clock.Now())
	profileV2 := profileForLatest(2, clock.Now())
	state := rebuildState(t, observer, profileV1, clock.Now())

	state, _ = observeContractStep(t, observer, state, profileV2, clock.Now(), 0)
	assertActiveVersion(t, state, 2)
	profileV2.MinDecryptionVersion = 2

	_, err := observer.Observe(state, profileV2, clock.Now())
	if !errors.Is(err, status.ErrTransitKeyUnusable) {
		t.Fatalf("expected blocked historical version to fail closed, got %v", err)
	}
	assertRotationStateInvariantCatalog(t, state)
}

func observeContractStep(
	t testing.TB,
	observer *status.Observer,
	previous keyregistry.StateFile,
	profile openbao.KeyProfile,
	now time.Time,
	promotions int,
) (keyregistry.StateFile, int) {
	t.Helper()

	result, err := observer.Observe(previous, profile, now)
	if err != nil {
		t.Fatalf("observe latest version %d: %v", profile.LatestVersion, err)
	}
	assertRotationTransitionInvariantCatalog(t, previous, result)
	if result.Promoted {
		promotions++
	}
	return result.State, promotions
}

func assertRotationTransitionInvariantCatalog(
	t testing.TB,
	previous keyregistry.StateFile,
	result status.ObservationResult,
) {
	t.Helper()

	if result.Changed {
		if result.State.Generation != previous.Generation+1 {
			t.Fatalf(
				"changed transition did not increment generation by one: previous=%d next=%d",
				previous.Generation,
				result.State.Generation,
			)
		}
	} else {
		if result.State.Generation != previous.Generation {
			t.Fatalf(
				"unchanged transition changed generation: previous=%d next=%d",
				previous.Generation,
				result.State.Generation,
			)
		}
		if result.State.CurrentHash != previous.CurrentHash {
			t.Fatalf(
				"unchanged transition changed state hash: previous=%s next=%s",
				previous.CurrentHash,
				result.State.CurrentHash,
			)
		}
	}

	previousActive, err := previous.ActiveSnapshot()
	if err != nil {
		t.Fatalf("previous active snapshot: %v", err)
	}
	nextActive, err := result.State.ActiveSnapshot()
	if err != nil {
		t.Fatalf("next active snapshot: %v", err)
	}
	if nextActive.TransitVersion < previousActive.TransitVersion {
		t.Fatalf("active version decreased: previous=%d next=%d", previousActive.TransitVersion, nextActive.TransitVersion)
	}
	assertRotationStateInvariantCatalog(t, result.State)
}

func assertRotationStateInvariantCatalog(t testing.TB, state keyregistry.StateFile) {
	t.Helper()

	if err := state.Validate(); err != nil {
		t.Fatalf("state validation failed: %v", err)
	}
	registry, err := state.Registry()
	if err != nil {
		t.Fatalf("registry from state: %v", err)
	}
	activeCount := 0
	for _, record := range state.Snapshots {
		snapshot, snapshotErr := record.Snapshot()
		if snapshotErr != nil {
			t.Fatalf("snapshot from record: %v", snapshotErr)
		}
		switch snapshot.State {
		case keyregistry.StateActive:
			activeCount++
			if snapshot.KubernetesKeyID != state.ActiveKeyID {
				t.Fatalf(
					"active snapshot key_id %s does not match state active key_id %s",
					snapshot.KubernetesKeyID,
					state.ActiveKeyID,
				)
			}
			lookedUp, lookupErr := registry.Lookup(snapshot.KubernetesKeyID)
			if lookupErr != nil {
				t.Fatalf("active snapshot version %d missing from registry: %v", snapshot.TransitVersion, lookupErr)
			}
			if lookedUp.State != keyregistry.StateActive {
				t.Fatalf("active lookup returned state %s", lookedUp.State)
			}
		case keyregistry.StateRetired:
			lookedUp, lookupErr := registry.Lookup(snapshot.KubernetesKeyID)
			if lookupErr != nil {
				t.Fatalf("retired snapshot version %d missing from registry: %v", snapshot.TransitVersion, lookupErr)
			}
			if lookedUp.State != keyregistry.StateRetired {
				t.Fatalf("retired lookup returned state %s", lookedUp.State)
			}
		case keyregistry.StatePending, keyregistry.StateRejected:
			if _, lookupErr := registry.Lookup(snapshot.KubernetesKeyID); !errors.Is(lookupErr, keyregistry.ErrUnknownKeyID) {
				t.Fatalf(
					"non-decryptable snapshot %s version %d lookup returned %v",
					snapshot.State,
					snapshot.TransitVersion,
					lookupErr,
				)
			}
		default:
			t.Fatalf("unexpected snapshot state %s", snapshot.State)
		}
	}
	if activeCount != 1 {
		t.Fatalf("expected exactly one active snapshot, got %d", activeCount)
	}
}

func assertConvergedObservationIdempotent(
	t testing.TB,
	observer *status.Observer,
	state keyregistry.StateFile,
	profile openbao.KeyProfile,
	now time.Time,
) {
	t.Helper()

	result, err := observer.Observe(state, profile, now)
	if err != nil {
		t.Fatalf("observe converged version %d: %v", profile.LatestVersion, err)
	}
	if result.Changed || result.Promoted || result.Pending {
		t.Fatalf(
			"expected converged observation to be idempotent, got changed=%t promoted=%t pending=%t",
			result.Changed,
			result.Promoted,
			result.Pending,
		)
	}
	assertRotationTransitionInvariantCatalog(t, state, result)
}
