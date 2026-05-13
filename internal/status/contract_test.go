package status_test

import (
	"context"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/kmsv2"
)

func TestStaleStatusHidesKeyIDButRetainsHistoricalDecryptLookup(t *testing.T) {
	clock := newFakeClock()
	store := newTestStore(t, clock)
	active, retired := contractSnapshots(t, clock.Now())
	state, err := keyregistry.NewStateFile(active, []keyregistry.KeySnapshot{retired}, 1, "")
	if err != nil {
		t.Fatalf("new state file: %v", err)
	}
	if err := store.PublishHealthy(state, clock.Now()); err != nil {
		t.Fatalf("publish healthy state: %v", err)
	}

	current, err := store.Current(context.Background())
	if err != nil {
		t.Fatalf("current healthy status: %v", err)
	}
	if current.Healthz != kmsv2.HealthOK || current.KeyID != active.KubernetesKeyID {
		t.Fatalf("unexpected healthy status: %+v", current)
	}

	clock.Advance(3 * time.Minute)
	stale, err := store.Current(context.Background())
	if err != nil {
		t.Fatalf("current stale status: %v", err)
	}
	if stale.Healthz != kmsv2.HealthUnhealthy {
		t.Fatalf("expected stale status to be unhealthy, got %s", stale.Healthz)
	}
	if stale.KeyID != "" {
		t.Fatalf("expected stale Status key_id to be hidden, got %s", stale.KeyID)
	}

	lookedUp, err := store.Lookup(retired.KubernetesKeyID)
	if err != nil {
		t.Fatalf("lookup retired historical key_id after Status became stale: %v", err)
	}
	if lookedUp.State != keyregistry.StateRetired {
		t.Fatalf("expected retired historical snapshot, got %s", lookedUp.State)
	}
}

func contractSnapshots(
	t testing.TB,
	now time.Time,
) (keyregistry.KeySnapshot, keyregistry.KeySnapshot) {
	t.Helper()

	active, err := (keyregistry.KeySnapshot{
		ProviderName:            "openbao-kms-workload-a",
		ClusterID:               "workload-a",
		OpenBaoInstanceID:       "bao-prod-a",
		TransitMountID:          "transit-prod-primary",
		TransitKeyLineageID:     "01HXEXAMPLEKEYLINEAGEID",
		TransitVersion:          2,
		TransitVersionCreatedAt: now.Add(2 * time.Minute),
		State:                   keyregistry.StateActive,
		AADMode:                 keyregistry.AADModeRequired,
	}).Normalize()
	if err != nil {
		t.Fatalf("normalize active snapshot: %v", err)
	}

	retired := active
	retired.TransitVersion = 1
	retired.TransitVersionCreatedAt = now.Add(time.Minute)
	retired.KubernetesKeyID = ""
	retired.State = keyregistry.StateRetired
	retired, err = retired.Normalize()
	if err != nil {
		t.Fatalf("normalize retired snapshot: %v", err)
	}
	return active, retired
}
