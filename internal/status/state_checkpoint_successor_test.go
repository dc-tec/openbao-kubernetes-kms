package status_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/status"
)

func TestFileStateStoreRejectsDivergentAheadState(t *testing.T) {
	clock := newFakeClock()
	initial, promoted := replayContractStates(t, clock)
	retired, err := keyregistry.RetireVersions(promoted, 2)
	if err != nil {
		t.Fatal(err)
	}
	// A lagging peer can accumulate more observations while retaining v1 as active.
	peerObserver := newTestObserver(t, clock, 5, time.Minute)
	peer := initial
	for range 3 {
		observed, observeErr := peerObserver.Observe(peer, profileForLatest(2, clock.Now()), clock.Now(), false)
		if observeErr != nil {
			t.Fatal(observeErr)
		}
		peer = observed.State
	}
	if peer.Generation != retired.Generation+1 || peer.PreviousHash == retired.CurrentHash {
		t.Fatal("fixture must have a divergent direct-successor generation")
	}
	gap, err := keyregistry.NewStateFileFromRecords(
		retired.ActiveKeyID, retired.Snapshots, retired.Generation+2, retired.CurrentHash,
		testIdentityFingerprint,
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []struct {
		name  string
		state keyregistry.StateFile
	}{
		{name: "divergent peer", state: peer},
		{name: "generation gap with matching previous hash", state: gap},
	} {
		t.Run(candidate.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "registry.json")
			disk := status.FileStateStore{Path: path}
			if err := disk.Save(retired); err != nil {
				t.Fatal(err)
			}
			checkpointPath := keyregistry.StateCheckpointPath(path)
			// #nosec G304 -- checkpoint is a fixture in t.TempDir.
			before, err := os.ReadFile(checkpointPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := keyregistry.SaveStateFile(path, candidate.state); err != nil {
				t.Fatal(err)
			}
			if _, err := disk.Load(); !errors.Is(err, keyregistry.ErrStateRollback) {
				t.Fatalf("expected rollback rejection, got %v", err)
			}
			// #nosec G304 -- checkpoint is a fixture in t.TempDir.
			after, err := os.ReadFile(checkpointPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("rejected restore changed the surviving checkpoint")
			}
		})
	}
}

func TestFileStateStoreCompletesInterruptedSuccessorSave(t *testing.T) {
	clock := newFakeClock()
	initial, promoted := replayContractStates(t, clock)
	path := filepath.Join(t.TempDir(), "registry.json")
	disk := status.FileStateStore{Path: path}
	if err := disk.Save(initial); err != nil {
		t.Fatal(err)
	}
	// Simulate a successful state rename followed by a crash before checkpoint save.
	if err := keyregistry.SaveStateFile(path, promoted); err != nil {
		t.Fatal(err)
	}
	loaded, err := disk.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.CurrentHash != promoted.CurrentHash {
		t.Fatal("did not recover the direct successor")
	}
	if err := disk.Confirm(promoted); err != nil {
		t.Fatalf("successor checkpoint was not persisted: %v", err)
	}
}
