package status_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/kmsv2"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/status"
)

func TestRetirementAllowsVersionRestrictionsAndSubsequentRotation(t *testing.T) {
	clock := newFakeClock()
	base := clock.Now()
	observer := newTestObserver(t, clock, 1, 0)
	profile := profileForLatest(2, base)
	previous := rebuildState(t, observer, profile, base)
	oldID := previous.Snapshots[1].KubernetesKeyID
	profile.MinDecryptionVersion = 2
	if _, err := observer.Observe(previous, profile, base); !errors.Is(err, status.ErrTransitKeyUnusable) {
		t.Fatalf("restriction without operator retirement must fail: %v", err)
	}
	retired, err := keyregistry.RetireVersions(previous, 2)
	if err != nil {
		t.Fatal(err)
	}
	stateStore := status.FileStateStore{Path: filepath.Join(t.TempDir(), "registry.json")}
	if err := stateStore.Save(retired); err != nil {
		t.Fatal(err)
	}
	restarted, err := stateStore.Load()
	if err != nil {
		t.Fatal(err)
	}
	// Simulate Transit trimming the now-retired version after the provider restarts.
	profile.MinAvailableVersion = 2
	profile.VersionCreationTimes = profile.VersionCreationTimes[1:]
	observed, err := observer.Observe(restarted, profile, base)
	if err != nil {
		t.Fatalf("retirement did not permit version restriction: %v", err)
	}
	assertRetirementStatus(t, clock, observed.State, oldID)
	// Jump over v3 to exercise intermediate-version reconstruction with tombstones.
	later := profileForLatest(4, base)
	later.MinDecryptionVersion = 2
	later.MinAvailableVersion = 2
	later.VersionCreationTimes = later.VersionCreationTimes[1:]
	clock.Advance(time.Minute)
	next, err := observer.Observe(observed.State, later, clock.Now())
	if err != nil {
		t.Fatalf("subsequent rotation failed: %v", err)
	}
	assertActiveVersion(t, next.State, 4)
	if err := keyregistry.ValidateStateProgress(restarted, next.State); err != nil {
		t.Fatalf("subsequent rotation lost retained identities: %v", err)
	}
	registry, err := next.State.Registry()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Lookup(previous.ActiveKeyID); err != nil {
		t.Fatalf("v2 must remain decryptable after later promotion: %v", err)
	}
	if _, err := registry.Lookup(oldID); !errors.Is(err, keyregistry.ErrUnknownKeyID) {
		t.Fatalf("later promotion reintroduced a removed version: %v", err)
	}
}

func assertRetirementStatus(t *testing.T, clock *fakeClock, state keyregistry.StateFile, removedID string) {
	t.Helper()
	store := newTestStore(t, clock)
	if err := store.PublishHealthy(state, clock.Now()); err != nil {
		t.Fatal(err)
	}
	current, err := store.Current(context.Background())
	if err != nil || current.Healthz != kmsv2.HealthOK || current.KeyID != state.ActiveKeyID {
		t.Fatalf("retirement lost active health or identity: %#v, %v", current, err)
	}
	if _, err := store.Lookup(removedID); !errors.Is(err, keyregistry.ErrUnknownKeyID) {
		t.Fatalf("removed key remains in runtime lookup: %v", err)
	}
}
