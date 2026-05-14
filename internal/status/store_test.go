package status_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/aad"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/kmsv2"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/status"
)

func TestStoreCurrentBecomesUnhealthyWhenStale(t *testing.T) {
	clock := newFakeClock()
	observer := newTestObserver(t, clock, 3, 2*time.Minute)
	state := rebuildState(t, observer, profileForLatest(1, clock.Now()), clock.Now())
	store := newTestStore(t, clock)

	if err := store.PublishHealthy(state, clock.Now()); err != nil {
		t.Fatalf("publish healthy: %v", err)
	}
	current, err := store.Current(context.Background())
	if err != nil {
		t.Fatalf("current status: %v", err)
	}
	if current.Healthz != kmsv2.HealthOK {
		t.Fatalf("expected healthy status, got %s", current.Healthz)
	}
	if current.KeyID == "" {
		t.Fatal("expected active key ID while healthy")
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
		t.Fatalf("expected stale Status key ID to be hidden, got %s", stale.KeyID)
	}
	if _, err := store.Lookup(current.KeyID); err != nil {
		t.Fatalf("lookup active key ID after staleness: %v", err)
	}
}

func TestStoreLookupExcludesPendingSnapshots(t *testing.T) {
	clock := newFakeClock()
	observer := newTestObserver(t, clock, 3, 2*time.Minute)
	state := rebuildState(t, observer, profileForLatest(1, clock.Now()), clock.Now())
	result, err := observer.Observe(state, profileForLatest(2, clock.Now()), clock.Now())
	if err != nil {
		t.Fatalf("observe pending rotation: %v", err)
	}
	pendingKeyID := pendingKeyID(t, result.State)
	store := newTestStore(t, clock)
	if err := store.PublishHealthy(result.State, clock.Now()); err != nil {
		t.Fatalf("publish pending state: %v", err)
	}

	_, err = store.Lookup(pendingKeyID)
	if !errors.Is(err, keyregistry.ErrUnknownKeyID) {
		t.Fatalf("expected pending key ID to be unavailable for decrypt, got %v", err)
	}
}

func TestStoreDiagnosticsExposeRedactedConsistencyState(t *testing.T) {
	clock := newFakeClock()
	observer := newTestObserver(t, clock, 3, 2*time.Minute)
	state := rebuildState(t, observer, profileForLatest(1, clock.Now()), clock.Now())
	result, err := observer.Observe(state, profileForLatest(2, clock.Now()), clock.Now())
	if err != nil {
		t.Fatalf("observe pending rotation: %v", err)
	}
	active, err := result.State.ActiveSnapshot()
	if err != nil {
		t.Fatalf("active snapshot: %v", err)
	}
	pendingID := pendingKeyID(t, result.State)
	store := newTestStore(t, clock)
	if err := store.PublishHealthy(result.State, clock.Now()); err != nil {
		t.Fatalf("publish pending state: %v", err)
	}
	store.UpdateCircuitBreaker(status.CircuitBreakerSnapshot{
		State:               status.CircuitBreakerOpen,
		ConsecutiveFailures: 2,
	})

	diagnostics, err := store.Diagnostics(context.Background())
	if err != nil {
		t.Fatalf("diagnostics: %v", err)
	}
	if diagnostics.ActiveKeyIDHash != aad.HashValue(active.KubernetesKeyID) {
		t.Fatalf("unexpected active key ID hash: %s", diagnostics.ActiveKeyIDHash)
	}
	if diagnostics.ActiveKeyIDHash == active.KubernetesKeyID {
		t.Fatal("diagnostics exposed raw active key ID")
	}
	if diagnostics.PendingKeyIDHash != aad.HashValue(pendingID) {
		t.Fatalf("unexpected pending key ID hash: %s", diagnostics.PendingKeyIDHash)
	}
	if diagnostics.RotationState != status.RotationStatePending {
		t.Fatalf("expected pending rotation state, got %s", diagnostics.RotationState)
	}
	if diagnostics.PendingStableObservationCount != 1 {
		t.Fatalf("unexpected pending observation count: %d", diagnostics.PendingStableObservationCount)
	}
	if diagnostics.CircuitBreaker.State != status.CircuitBreakerOpen {
		t.Fatalf("expected open circuit breaker, got %s", diagnostics.CircuitBreaker.State)
	}
}

func TestStoreConcurrentPublicationAndReads(t *testing.T) {
	clock := newFakeClock()
	observer := newTestObserver(t, clock, 1, 0)
	v1 := rebuildState(t, observer, profileForLatest(1, clock.Now()), clock.Now())
	v2 := rebuildState(t, observer, profileForLatest(2, clock.Now()), clock.Now())
	store := newTestStore(t, clock)
	if err := store.PublishHealthy(v1, clock.Now()); err != nil {
		t.Fatalf("publish initial state: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				current, err := store.Current(context.Background())
				if err != nil {
					t.Errorf("current status: %v", err)
					return
				}
				if current.Healthz == kmsv2.HealthOK && current.KeyID == "" {
					t.Error("healthy status returned empty key ID")
					return
				}
			}
		}()
	}
	for i := 0; i < 100; i++ {
		if i%2 == 0 {
			if err := store.PublishHealthy(v1, clock.Now()); err != nil {
				t.Fatalf("publish v1: %v", err)
			}
		} else if err := store.PublishHealthy(v2, clock.Now()); err != nil {
			t.Fatalf("publish v2: %v", err)
		}
	}
	wg.Wait()
}

func newTestStore(t *testing.T, clock *fakeClock) *status.Store {
	t.Helper()

	store, err := status.NewStore(status.StoreOptions{
		Clock:        clock,
		MaxStaleness: 2 * time.Minute,
	})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return store
}

func newTestObserver(t *testing.T, clock *fakeClock, stableCount int, delay time.Duration) *status.Observer {
	t.Helper()

	observer, err := status.NewObserver(status.SnapshotScope{
		ProviderName:        "openbao-kms-workload-a",
		ClusterID:           "workload-a",
		OpenBaoInstanceID:   "bao-prod-a",
		TransitMountID:      "transit-prod-primary",
		TransitKeyLineageID: "01HXEXAMPLEKEYLINEAGEID",
		AADMode:             keyregistry.AADModeRequired,
	}, status.RotationPolicy{
		ActivationDelay:               delay,
		RequireStableObservationCount: stableCount,
		RejectVersionRollback:         true,
	})
	if err != nil {
		t.Fatalf("new observer at %s: %v", clock.Now(), err)
	}
	return observer
}

func rebuildState(
	t *testing.T,
	observer *status.Observer,
	profile openbao.KeyProfile,
	now time.Time,
) keyregistry.StateFile {
	t.Helper()

	state, err := observer.RebuildState(profile, now)
	if err != nil {
		t.Fatalf("rebuild state: %v", err)
	}
	return state
}

func pendingKeyID(t *testing.T, state keyregistry.StateFile) string {
	t.Helper()

	for _, record := range state.Snapshots {
		snapshot, err := record.Snapshot()
		if err != nil {
			t.Fatalf("snapshot from record: %v", err)
		}
		if snapshot.State == keyregistry.StatePending {
			return snapshot.KubernetesKeyID
		}
	}
	t.Fatal("pending snapshot missing")
	return ""
}

func profileForLatest(latest int, base time.Time) openbao.KeyProfile {
	versions := make([]openbao.KeyVersion, 0, latest)
	for version := 1; version <= latest; version++ {
		versions = append(versions, openbao.KeyVersion{
			Version:   version,
			CreatedAt: base.Add(time.Duration(version) * time.Minute).UTC(),
		})
	}
	return openbao.KeyProfile{
		Name:                 "k8s-workload-a-etcd",
		Type:                 openbao.TransitKeyTypeAES256GCM96,
		LatestVersion:        latest,
		VersionCreationTimes: versions,
		SupportsEncryption:   true,
		SupportsDecryption:   true,
	}
}

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Unix(1_700_000_000, 0).UTC()}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(duration time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(duration)
}
