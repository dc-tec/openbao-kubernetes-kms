package status_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/status"
)

func TestFileStateStoreAnchorsLoadedStateWithCheckpoint(t *testing.T) {
	clock := newFakeClock()
	observer := newTestObserver(t, clock, 3, time.Minute)
	state := rebuildState(t, observer, profileForLatest(1, clock.Now()), clock.Now())
	path := filepath.Join(t.TempDir(), "key-registry.json")
	if err := keyregistry.SaveStateFile(path, state); err != nil {
		t.Fatalf("save unanchored state: %v", err)
	}

	store := status.FileStateStore{Path: path}
	if _, err := store.Load(); err != nil {
		t.Fatalf("load state: %v", err)
	}
	if _, err := os.Stat(keyregistry.StateCheckpointPath(path)); err != nil {
		t.Fatalf("expected checkpoint to be created: %v", err)
	}
}

func TestFileStateStoreRejectsRollbackBelowCheckpoint(t *testing.T) {
	clock := newFakeClock()
	observer := newTestObserver(t, clock, 1, 0)
	stateV1 := rebuildState(t, observer, profileForLatest(1, clock.Now()), clock.Now())
	promoted, err := observer.Observe(stateV1, profileForLatest(2, clock.Now()), clock.Now())
	if err != nil {
		t.Fatalf("promote v2: %v", err)
	}
	path := filepath.Join(t.TempDir(), "key-registry.json")
	store := status.FileStateStore{Path: path}
	if err := store.Save(promoted.State); err != nil {
		t.Fatalf("save promoted state: %v", err)
	}
	if err := keyregistry.SaveStateFile(path, stateV1); err != nil {
		t.Fatalf("replay older state: %v", err)
	}

	_, err = store.Load()
	if !errors.Is(err, keyregistry.ErrStateRollback) {
		t.Fatalf("expected rollback detection, got %v", err)
	}
}

func TestFileStateStoreRejectsMissingStateWhenCheckpointExists(t *testing.T) {
	clock := newFakeClock()
	observer := newTestObserver(t, clock, 3, time.Minute)
	state := rebuildState(t, observer, profileForLatest(1, clock.Now()), clock.Now())
	path := filepath.Join(t.TempDir(), "key-registry.json")
	store := status.FileStateStore{Path: path}
	if err := store.Save(state); err != nil {
		t.Fatalf("save state: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove state file: %v", err)
	}

	_, err := store.Load()
	if !errors.Is(err, keyregistry.ErrStateRollback) {
		t.Fatalf("expected missing state with checkpoint to fail as rollback, got %v", err)
	}
}
