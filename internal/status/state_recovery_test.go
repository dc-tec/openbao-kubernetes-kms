package status_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/kmsv2"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/status"
)

func TestControllerReconcilesFailedSaveBeforeMetadataCanRestoreHealth(t *testing.T) {
	for _, target := range []string{"state", "checkpoint"} {
		t.Run(target, func(t *testing.T) {
			f := newPersistenceFixture(t)
			before, _ := f.store.State()
			path := f.disk.Path
			if target == "checkpoint" {
				path = keyregistry.StateCheckpointPath(path)
			}
			unblock := blockPersistenceWrite(t, path)
			f.transit.profile = profileForLatest(2, f.clock.Now())
			if err := f.controller.ProbeOnce(t.Context()); err == nil {
				t.Fatal("blocked save succeeded")
			}
			assertStoreHealth(t, f.store, kmsv2.HealthUnhealthy)
			assertPublishedHash(t, f.store, before.CurrentHash)
			if _, err := f.store.Lookup(before.ActiveKeyID); err != nil {
				t.Fatalf("confirmed key lost: %v", err)
			}
			// An unchanged view of the old backend must not bypass recovery.
			f.transit.profile = profileForLatest(1, f.clock.Now())
			if err := f.controller.ProbeOnce(t.Context()); err == nil {
				t.Fatal("older metadata cleared a blocked save")
			}
			assertReadinessReasons(t, f.store, status.ReasonStateSaveFailed)
			unblock()
			if err := f.controller.ProbeOnce(t.Context()); !errors.Is(err, status.ErrTransitMetadataInvalid) {
				t.Fatalf("recovered pending identity was not validated: %v", err)
			}
			assertPublishedHash(t, f.store, before.CurrentHash)
			assertStoreHealth(t, f.store, kmsv2.HealthUnhealthy)
			f.transit.profile = profileForLatest(2, f.clock.Now())
			if err := f.controller.ProbeOnce(t.Context()); err != nil {
				t.Fatal(err)
			}
			assertStoreHealth(t, f.store, kmsv2.HealthOK)
			published, _ := f.store.State()
			loaded, err := f.disk.Load()
			if err != nil || loaded.CurrentHash != published.CurrentHash {
				t.Fatalf("recovered publication differs from restart state: %v", err)
			}
		})
	}
}

func TestRecoveredPromotionRequiresDeepProbe(t *testing.T) {
	f := newPersistenceFixture(t)
	f.transit.profile = profileForLatest(2, f.clock.Now())
	for range 3 {
		if err := f.controller.ProbeOnce(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	f.clock.Advance(time.Minute)
	unblock := blockPersistenceWrite(t, keyregistry.StateCheckpointPath(f.disk.Path))
	if err := f.controller.ProbeOnce(t.Context()); err == nil {
		t.Fatal("partial promotion save succeeded")
	}
	before, _ := f.store.State()
	assertActiveVersion(t, before, 1)
	unblock()
	if err := f.controller.ProbeOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	after, _ := f.store.State()
	assertActiveVersion(t, after, 2)
	assertReadinessReasons(t, f.store, status.ReasonDeepProbePending)
	assertStoreHealth(t, f.store, kmsv2.HealthUnhealthy)
	if err := f.controller.DeepProbeOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertStoreHealth(t, f.store, kmsv2.HealthOK)
}

func TestFileStateRecoveryRejectsConflictingFiles(t *testing.T) {
	clock := newFakeClock()
	previous, attempted := replayContractStates(t, clock)
	alternate := sameGenerationAlternateState(t, previous, attempted)
	for _, target := range []string{
		"state missing", "checkpoint missing", "other state", "other checkpoint", "checkpoint ahead",
	} {
		t.Run(target, func(t *testing.T) {
			disk := status.FileStateStore{Path: filepath.Join(t.TempDir(), "registry.json")}
			if err := disk.Save(previous); err != nil {
				t.Fatal(err)
			}
			checkpointPath := keyregistry.StateCheckpointPath(disk.Path)
			var err error
			switch target {
			case "state missing":
				err = os.Remove(disk.Path)
			case "checkpoint missing":
				err = os.Remove(checkpointPath)
			case "other state":
				err = keyregistry.SaveStateFile(disk.Path, alternate)
			case "other checkpoint":
				err = writeRecoveryCheckpoint(checkpointPath, alternate)
			case "checkpoint ahead":
				err = writeRecoveryCheckpoint(checkpointPath, attempted)
			}
			if err != nil {
				t.Fatal(err)
			}
			stateBytes := readRecoveryEvidence(t, disk.Path)
			checkpointBytes := readRecoveryEvidence(t, checkpointPath)
			if err := disk.Recover(&previous, attempted); !errors.Is(err, keyregistry.ErrStateRollback) {
				t.Fatalf("unexpected recovery result: %v", err)
			}
			if readRecoveryEvidence(t, disk.Path) != stateBytes || readRecoveryEvidence(t, checkpointPath) != checkpointBytes {
				t.Fatal("rejected recovery modified disk evidence")
			}
		})
	}
}

func readRecoveryEvidence(t *testing.T, path string) string {
	t.Helper()
	// #nosec G304 -- path refers to a state fixture under t.TempDir.
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return string(data)
}

func TestFileStateRecoveryCompletesExactCandidate(t *testing.T) {
	previous, attempted := replayContractStates(t, newFakeClock())
	for _, stage := range []string{"before rename", "after state rename", "after checkpoint rename", "bootstrap absent"} {
		t.Run(stage, func(t *testing.T) {
			disk := status.FileStateStore{Path: filepath.Join(t.TempDir(), "registry.json")}
			base := &previous
			next := attempted
			if stage == "bootstrap absent" {
				base, next = nil, previous
			} else if err := disk.Save(previous); err != nil {
				t.Fatal(err)
			}
			if stage == "after state rename" || stage == "after checkpoint rename" {
				if err := keyregistry.SaveStateFile(disk.Path, attempted); err != nil {
					t.Fatal(err)
				}
			}
			if stage == "after checkpoint rename" {
				if err := writeRecoveryCheckpoint(keyregistry.StateCheckpointPath(disk.Path), attempted); err != nil {
					t.Fatal(err)
				}
			}
			for range 2 {
				if err := disk.Recover(base, next); err != nil {
					t.Fatal(err)
				}
			}
			loaded, err := disk.Load()
			if err != nil || loaded.CurrentHash != next.CurrentHash {
				t.Fatalf("exact candidate not recovered: %v", err)
			}
		})
	}
}

func TestPendingDelayDoesNotPersistRedundantObservations(t *testing.T) {
	f := newDiagnosticFixture(t, 3, time.Minute)
	f.probeHealthy(t)
	f.transit.profile = profileForLatest(2, f.clock.Now())
	for range 3 {
		if err := f.controller.ProbeOnce(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	before, _ := f.store.State()
	saves := f.state.saveCalls
	f.clock.Advance(30 * time.Second)
	for range 3 {
		if err := f.controller.ProbeOnce(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	assertPublishedHash(t, f.store, before.CurrentHash)
	if f.state.saveCalls != saves {
		t.Fatal("activation delay rewrote unchanged progress")
	}
	f.clock.Advance(30 * time.Second)
	if err := f.controller.ProbeOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	current, _ := f.store.State()
	assertActiveVersion(t, current, 2)
	assertStoreHealth(t, f.store, kmsv2.HealthUnhealthy)
}

type persistenceFixture struct {
	clock      *fakeClock
	store      *status.Store
	transit    *fakeTransit
	disk       status.FileStateStore
	controller *status.Controller
}

func newPersistenceFixture(t *testing.T) persistenceFixture {
	t.Helper()
	clock := newFakeClock()
	store := newTestStore(t, clock)
	transit := &fakeTransit{profile: profileForLatest(1, clock.Now())}
	disk := status.FileStateStore{Path: filepath.Join(t.TempDir(), "registry.json")}
	controller := newTestControllerWithOptions(t, status.ControllerOptions{
		Clock: clock, Store: store, Observer: newTestObserver(t, clock, 3, time.Minute),
		Transit: transit, StateStore: disk, MountPath: "transit", KeyName: "k8s-workload-a-etcd",
	})
	if err := controller.ProbeOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := controller.DeepProbeOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	return persistenceFixture{clock, store, transit, disk, controller}
}

func blockPersistenceWrite(t *testing.T, path string) func() {
	t.Helper()
	temp := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp")
	blocker := filepath.Join(temp, "blocker")
	if err := os.MkdirAll(blocker, 0o700); err != nil {
		t.Fatal(err)
	}
	return func() {
		t.Helper()
		if err := os.Remove(blocker); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(temp); err != nil {
			t.Fatal(err)
		}
	}
}

func writeRecoveryCheckpoint(path string, state keyregistry.StateFile) error {
	checkpoint, err := keyregistry.NewStateCheckpoint(state)
	if err != nil {
		return err
	}
	return keyregistry.SaveStateCheckpoint(path, checkpoint)
}

func assertPublishedHash(t *testing.T, store *status.Store, hash string) {
	t.Helper()
	current, _ := store.State()
	if current.CurrentHash != hash {
		t.Fatal("unexpected published state")
	}
}
