package keyregistry_test

import (
	"errors"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
)

func retirementState(t *testing.T) keyregistry.StateFile {
	t.Helper()
	active := loadGoldenFixture(t).Snapshot.keySnapshot()
	active.TransitVersion = 3
	active.KubernetesKeyID = ""
	v2 := historicalSnapshot(active)
	v1 := historicalSnapshot(v2)
	state, err := keyregistry.NewStateFile(active, []keyregistry.KeySnapshot{v1, v2}, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestRetirementPersistsRemovalWithoutChangingActiveOrRetainedKeys(t *testing.T) {
	previous := retirementState(t)
	next, err := keyregistry.RetireVersions(previous, 2)
	if err != nil {
		t.Fatal(err)
	}
	if next.ActiveKeyID != previous.ActiveKeyID || next.Generation != previous.Generation+1 ||
		next.PreviousHash != previous.CurrentHash || len(next.Snapshots) != len(previous.Snapshots) {
		t.Fatalf("invalid retirement transition: %#v", next)
	}
	path := filepath.Join(t.TempDir(), "registry.json")
	if err := keyregistry.SaveStateFile(path, next); err != nil {
		t.Fatal(err)
	}
	loaded, registry, err := keyregistry.LoadStateFile(path, keyregistry.StateLoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	assertRetirementRecords(t, previous, loaded, registry)
	repeated, err := keyregistry.RetireVersions(loaded, 2)
	if err != nil || repeated.CurrentHash != loaded.CurrentHash {
		t.Fatalf("repeat retirement must be idempotent: %v", err)
	}
	if err := keyregistry.ValidateStateProgress(previous, next); !errors.Is(err, keyregistry.ErrStateRollback) {
		t.Fatalf("ordinary rotation must not authorize retirement: %v", err)
	}
}

func assertRetirementRecords(t *testing.T, previous, loaded keyregistry.StateFile, registry keyregistry.Registry) {
	t.Helper()
	for i, record := range previous.Snapshots {
		_, lookupErr := registry.Lookup(record.KubernetesKeyID)
		if record.TransitVersion == 1 {
			if !errors.Is(lookupErr, keyregistry.ErrUnknownKeyID) {
				t.Fatalf("removed key remains decryptable: %v", lookupErr)
			}
			record.State = string(keyregistry.StateRemoved)
		} else if lookupErr != nil {
			t.Fatalf("retained key lost: %v", lookupErr)
		}
		if loaded.Snapshots[i] != record {
			t.Fatalf("snapshot identity or observation metadata changed: %#v", loaded.Snapshots[i])
		}
	}
}

func TestStateProgressRejectsLossOfDecryptableSnapshots(t *testing.T) {
	previous := retirementState(t)
	for _, version := range []int{1, 2, 3} {
		for _, replacement := range []string{"drop", "pending", "rejected"} {
			t.Run(replacement+"-v"+strconv.Itoa(version), func(t *testing.T) {
				records := make([]keyregistry.SnapshotStateRecord, 0, len(previous.Snapshots))
				activeID := previous.ActiveKeyID
				for _, record := range previous.Snapshots {
					if record.TransitVersion == version {
						if replacement == "drop" {
							continue
						}
						record.State = replacement
					}
					records = append(records, record)
				}
				if version == 3 {
					active, err := previous.ActiveSnapshot()
					if err != nil {
						t.Fatal(err)
					}
					active.TransitVersion++
					active.TransitVersionCreatedAt = active.TransitVersionCreatedAt.Add(time.Hour)
					active.KubernetesKeyID = ""
					active, err = active.Normalize()
					if err != nil {
						t.Fatal(err)
					}
					activeID = active.KubernetesKeyID
					records = append(records, keyregistry.SnapshotStateRecordFromSnapshot(active))
				}
				next, err := keyregistry.NewStateFileFromRecords(activeID, records, 2, previous.CurrentHash)
				if err != nil {
					t.Fatal(err)
				}
				if err := keyregistry.ValidateStateProgress(previous, next); !errors.Is(err, keyregistry.ErrStateRollback) {
					t.Fatalf("loss of version %d accepted: %v", version, err)
				}
			})
		}
	}
}

func TestStateProgressPreservesRemovedRecords(t *testing.T) {
	previous, err := keyregistry.RetireVersions(retirementState(t), 2)
	if err != nil {
		t.Fatal(err)
	}
	for _, replacement := range []string{"drop", "retired", "changed"} {
		t.Run(replacement, func(t *testing.T) {
			records := make([]keyregistry.SnapshotStateRecord, 0, len(previous.Snapshots))
			for _, record := range previous.Snapshots {
				if record.State == string(keyregistry.StateRemoved) {
					switch replacement {
					case "drop":
						continue
					case "retired":
						record.State = replacement
					case "changed":
						record.ObservedAtUnix++
					}
				}
				records = append(records, record)
			}
			next, err := keyregistry.NewStateFileFromRecords(previous.ActiveKeyID, records, 3, previous.CurrentHash)
			if err != nil {
				t.Fatal(err)
			}
			if err := keyregistry.ValidateStateProgress(previous, next); !errors.Is(err, keyregistry.ErrStateRollback) {
				t.Fatalf("removed record mutation accepted: %v", err)
			}
		})
	}
}

func TestRetirementRejectsActiveRemovalAndPendingRotation(t *testing.T) {
	previous := retirementState(t)
	for _, boundary := range []int{-1, 0, 1, 4} {
		if _, err := keyregistry.RetireVersions(previous, boundary); err == nil {
			t.Fatalf("unsafe boundary %d accepted", boundary)
		}
	}
	records := append([]keyregistry.SnapshotStateRecord(nil), previous.Snapshots...)
	records[1].State = string(keyregistry.StatePending)
	pending, err := keyregistry.NewStateFileFromRecords(previous.ActiveKeyID, records, 2, previous.CurrentHash)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := keyregistry.RetireVersions(pending, 2); err == nil {
		t.Fatal("retirement accepted with pending rotation")
	}
}
