package keyregistry_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
)

type goldenFixture struct {
	Snapshot      snapshotFixture   `json:"snapshot"`
	ExpectedKeyID string            `json:"expectedKeyId"`
	Annotations   map[string]string `json:"expectedAnnotations"`
}

type snapshotFixture struct {
	ProviderName                string `json:"providerName"`
	ClusterID                   string `json:"clusterId"`
	OpenBaoInstanceID           string `json:"openbaoInstanceId"`
	OpenBaoNamespace            string `json:"openbaoNamespace"`
	TransitMountID              string `json:"transitMountId"`
	TransitKeyLineageID         string `json:"transitKeyLineageId"`
	TransitVersion              int    `json:"transitVersion"`
	TransitVersionCreatedAtUnix int64  `json:"transitVersionCreatedAtUnix"`
	State                       string `json:"state"`
	AADMode                     string `json:"aadMode"`
}

func TestDeriveKeyIDGolden(t *testing.T) {
	fixture := loadGoldenFixture(t)
	snapshot := fixture.Snapshot.keySnapshot()

	keyID, err := keyregistry.DeriveKeyID(snapshot)
	if err != nil {
		t.Fatalf("derive key ID: %v", err)
	}
	if keyID != fixture.ExpectedKeyID {
		t.Fatalf("unexpected key ID:\nwant %s\ngot  %s", fixture.ExpectedKeyID, keyID)
	}

	repeated, err := keyregistry.DeriveKeyID(snapshot)
	if err != nil {
		t.Fatalf("derive repeated key ID: %v", err)
	}
	if repeated != keyID {
		t.Fatalf("key ID is not deterministic: first %s repeated %s", keyID, repeated)
	}
}

func TestDeriveKeyIDChangesForIdentityFields(t *testing.T) {
	base := loadGoldenFixture(t).Snapshot.keySnapshot()
	baseKeyID, err := keyregistry.DeriveKeyID(base)
	if err != nil {
		t.Fatalf("derive base key ID: %v", err)
	}

	tests := []struct {
		name   string
		change func(keyregistry.KeySnapshot) keyregistry.KeySnapshot
	}{
		{
			name: "provider name",
			change: func(snapshot keyregistry.KeySnapshot) keyregistry.KeySnapshot {
				snapshot.ProviderName = "openbao-kms-workload-b"
				return snapshot
			},
		},
		{
			name: "cluster ID",
			change: func(snapshot keyregistry.KeySnapshot) keyregistry.KeySnapshot {
				snapshot.ClusterID = "workload-b"
				return snapshot
			},
		},
		{
			name: "OpenBao instance",
			change: func(snapshot keyregistry.KeySnapshot) keyregistry.KeySnapshot {
				snapshot.OpenBaoInstanceID = "bao-prod-b"
				return snapshot
			},
		},
		{
			name: "OpenBao namespace",
			change: func(snapshot keyregistry.KeySnapshot) keyregistry.KeySnapshot {
				snapshot.OpenBaoNamespace = "admin/workload-a"
				return snapshot
			},
		},
		{
			name: "Transit mount ID",
			change: func(snapshot keyregistry.KeySnapshot) keyregistry.KeySnapshot {
				snapshot.TransitMountID = "transit-prod-secondary"
				return snapshot
			},
		},
		{
			name: "key lineage",
			change: func(snapshot keyregistry.KeySnapshot) keyregistry.KeySnapshot {
				snapshot.TransitKeyLineageID = "01HXDIFFERENTKEYLINEAGE"
				return snapshot
			},
		},
		{
			name: "Transit version",
			change: func(snapshot keyregistry.KeySnapshot) keyregistry.KeySnapshot {
				snapshot.TransitVersion = 4
				return snapshot
			},
		},
		{
			name: "Transit version creation time",
			change: func(snapshot keyregistry.KeySnapshot) keyregistry.KeySnapshot {
				snapshot.TransitVersionCreatedAt = snapshot.TransitVersionCreatedAt.Add(time.Hour)
				return snapshot
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changedKeyID, err := keyregistry.DeriveKeyID(tt.change(base))
			if err != nil {
				t.Fatalf("derive changed key ID: %v", err)
			}
			if changedKeyID == baseKeyID {
				t.Fatalf("key ID did not change when %s changed", tt.name)
			}
		})
	}
}

func TestDeriveKeyIDCanonicalizesTransitCreationTimeToUnixSeconds(t *testing.T) {
	base := loadGoldenFixture(t).Snapshot.keySnapshot()
	withSubsecondPrecision := base
	withSubsecondPrecision.TransitVersionCreatedAt = base.TransitVersionCreatedAt.Add(987 * time.Millisecond)

	baseKeyID, err := keyregistry.DeriveKeyID(base)
	if err != nil {
		t.Fatalf("derive base key ID: %v", err)
	}
	subsecondKeyID, err := keyregistry.DeriveKeyID(withSubsecondPrecision)
	if err != nil {
		t.Fatalf("derive subsecond key ID: %v", err)
	}
	if subsecondKeyID != baseKeyID {
		t.Fatalf("subsecond precision changed key ID: base %s subsecond %s", baseKeyID, subsecondKeyID)
	}

	normalized, err := withSubsecondPrecision.Normalize()
	if err != nil {
		t.Fatalf("normalize subsecond snapshot: %v", err)
	}
	if !normalized.TransitVersionCreatedAt.Equal(base.TransitVersionCreatedAt) {
		t.Fatalf(
			"creation time was not canonicalized: want %s got %s",
			base.TransitVersionCreatedAt,
			normalized.TransitVersionCreatedAt,
		)
	}
}

func TestDeriveKeyIDRejectsInvalidTransitCreationTime(t *testing.T) {
	base := loadGoldenFixture(t).Snapshot.keySnapshot()
	for _, createdAt := range []time.Time{{}, time.Unix(0, 0).UTC()} {
		t.Run(createdAt.String(), func(t *testing.T) {
			snapshot := base
			snapshot.TransitVersionCreatedAt = createdAt
			if _, err := keyregistry.DeriveKeyID(snapshot); err == nil {
				t.Fatal("expected invalid Transit creation time to fail")
			}
		})
	}
}

func TestParseKeyID(t *testing.T) {
	valid := loadGoldenFixture(t).ExpectedKeyID

	parsed, err := keyregistry.ParseKeyID(valid)
	if err != nil {
		t.Fatalf("parse valid key ID: %v", err)
	}
	if parsed != valid {
		t.Fatalf("parse changed key ID: %s", parsed)
	}

	malformed := []string{
		"",
		"3",
		"obk2.short",
		"raw-transit-version-3",
		"obk2.!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!",
	}
	for _, keyID := range malformed {
		t.Run(keyID, func(t *testing.T) {
			_, err := keyregistry.ParseKeyID(keyID)
			if !errors.Is(err, keyregistry.ErrMalformedKeyID) {
				t.Fatalf("expected malformed key ID error, got %v", err)
			}
		})
	}
}

func TestRegistryLookupRejectsUnknownKeyID(t *testing.T) {
	fixture := loadGoldenFixture(t)
	active := fixture.Snapshot.keySnapshot()

	registry, err := keyregistry.NewRegistry(active, nil)
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}

	gotActive, ok := registry.Active()
	if !ok {
		t.Fatal("active snapshot missing")
	}
	if gotActive.KubernetesKeyID != fixture.ExpectedKeyID {
		t.Fatalf("unexpected active key ID: %s", gotActive.KubernetesKeyID)
	}

	unknownSnapshot := active
	unknownSnapshot.TransitVersion = active.TransitVersion + 1
	unknownSnapshot.KubernetesKeyID = ""
	unknownKeyID, err := keyregistry.DeriveKeyID(unknownSnapshot)
	if err != nil {
		t.Fatalf("derive unknown key ID: %v", err)
	}

	_, err = registry.Lookup(unknownKeyID)
	if !errors.Is(err, keyregistry.ErrUnknownKeyID) {
		t.Fatalf("expected unknown key ID to be rejected before Transit, got %v", err)
	}

	_, err = registry.Lookup("not-a-key-id")
	if !errors.Is(err, keyregistry.ErrMalformedKeyID) {
		t.Fatalf("expected malformed key ID to be rejected, got %v", err)
	}
}

func TestNormalizeRejectsMismatchedKeyID(t *testing.T) {
	snapshot := loadGoldenFixture(t).Snapshot.keySnapshot()
	snapshot.KubernetesKeyID = "obk2.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

	_, err := snapshot.Normalize()
	if err == nil {
		t.Fatal("expected mismatched key ID to fail")
	}
}

func TestStateFileRoundTrip(t *testing.T) {
	active := loadGoldenFixture(t).Snapshot.keySnapshot()
	historical := historicalSnapshot(active)
	state, err := keyregistry.NewStateFile(active, []keyregistry.KeySnapshot{historical}, 1, "")
	if err != nil {
		t.Fatalf("new state file: %v", err)
	}

	path := filepath.Join(t.TempDir(), "key-registry.json")
	if err := keyregistry.SaveStateFile(path, state); err != nil {
		t.Fatalf("save state file: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat state file: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o640 {
		t.Fatalf("unexpected state file mode: %04o", got)
	}

	loaded, registry, err := keyregistry.LoadStateFile(path, keyregistry.StateLoadOptions{})
	if err != nil {
		t.Fatalf("load state file: %v", err)
	}
	if loaded.CurrentHash != state.CurrentHash {
		t.Fatalf("state hash changed across round trip: %s != %s", loaded.CurrentHash, state.CurrentHash)
	}

	activeSnapshot, ok := registry.Active()
	if !ok {
		t.Fatal("active snapshot missing")
	}
	if activeSnapshot.KubernetesKeyID != state.ActiveKeyID {
		t.Fatalf("unexpected active key ID: %s", activeSnapshot.KubernetesKeyID)
	}
	historicalKeyID, err := keyregistry.DeriveKeyID(historical)
	if err != nil {
		t.Fatalf("derive historical key ID: %v", err)
	}
	if _, err := registry.Lookup(historicalKeyID); err != nil {
		t.Fatalf("lookup historical key ID after reload: %v", err)
	}
}

func TestStateFileRegistryExcludesPendingAndRejectedSnapshots(t *testing.T) {
	active, err := loadGoldenFixture(t).Snapshot.keySnapshot().Normalize()
	if err != nil {
		t.Fatalf("normalize active: %v", err)
	}
	pending := active
	pending.TransitVersion++
	pending.TransitVersionCreatedAt = active.TransitVersionCreatedAt.Add(time.Hour)
	pending.KubernetesKeyID = ""
	pending.State = keyregistry.StatePending
	pending, err = pending.Normalize()
	if err != nil {
		t.Fatalf("normalize pending: %v", err)
	}
	rejected := active
	rejected.TransitVersion += 2
	rejected.TransitVersionCreatedAt = active.TransitVersionCreatedAt.Add(2 * time.Hour)
	rejected.KubernetesKeyID = ""
	rejected.State = keyregistry.StateRejected
	rejected, err = rejected.Normalize()
	if err != nil {
		t.Fatalf("normalize rejected: %v", err)
	}

	state, err := keyregistry.NewStateFileFromRecords(
		active.KubernetesKeyID,
		[]keyregistry.SnapshotStateRecord{
			keyregistry.SnapshotStateRecordFromSnapshot(active),
			keyregistry.SnapshotStateRecordFromSnapshot(pending),
			keyregistry.SnapshotStateRecordFromSnapshot(rejected),
		},
		1,
		"",
	)
	if err != nil {
		t.Fatalf("new state file: %v", err)
	}
	registry, err := state.Registry()
	if err != nil {
		t.Fatalf("registry from state: %v", err)
	}

	if _, err := registry.Lookup(active.KubernetesKeyID); err != nil {
		t.Fatalf("lookup active key ID: %v", err)
	}
	for _, keyID := range []string{pending.KubernetesKeyID, rejected.KubernetesKeyID} {
		_, err := registry.Lookup(keyID)
		if !errors.Is(err, keyregistry.ErrUnknownKeyID) {
			t.Fatalf("expected non-decryptable key ID to be excluded, got %v", err)
		}
	}
}

func TestMissingStateFileCanBeRebuiltFromMetadata(t *testing.T) {
	active := loadGoldenFixture(t).Snapshot.keySnapshot()
	historical := historicalSnapshot(active)

	_, _, err := keyregistry.LoadStateFile(filepath.Join(t.TempDir(), "missing.json"), keyregistry.StateLoadOptions{})
	if !errors.Is(err, keyregistry.ErrStateNotFound) {
		t.Fatalf("expected missing state error, got %v", err)
	}

	state, err := keyregistry.RebuildStateFromMetadata(active, []keyregistry.KeySnapshot{historical})
	if err != nil {
		t.Fatalf("rebuild state from metadata: %v", err)
	}
	registry, err := state.Registry()
	if err != nil {
		t.Fatalf("registry from rebuilt state: %v", err)
	}
	if _, ok := registry.Active(); !ok {
		t.Fatal("rebuilt registry missing active snapshot")
	}
}

func TestStateFileFromRecordsPreservesRotationObservationState(t *testing.T) {
	active := loadGoldenFixture(t).Snapshot.keySnapshot()
	normalized, err := active.Normalize()
	if err != nil {
		t.Fatalf("normalize active: %v", err)
	}
	record := keyregistry.SnapshotStateRecordFromSnapshot(normalized)
	record.ObservedAtUnix = 1_700_000_000
	record.StableObservationCount = 3
	record.StableAtUnix = 1_700_000_060
	record.PromotedAtUnix = 1_700_000_120

	state, err := keyregistry.NewStateFileFromRecords(
		normalized.KubernetesKeyID,
		[]keyregistry.SnapshotStateRecord{record},
		7,
		"",
	)
	if err != nil {
		t.Fatalf("new state from records: %v", err)
	}
	if got := state.Snapshots[0].StableObservationCount; got != 3 {
		t.Fatalf("stable observation count was not preserved: %d", got)
	}
	if got := state.Snapshots[0].StableAtUnix; got != record.StableAtUnix {
		t.Fatalf("stable time was not preserved: %d", got)
	}
	if got := state.Snapshots[0].PromotedAtUnix; got != record.PromotedAtUnix {
		t.Fatalf("promoted time was not preserved: %d", got)
	}
}

func TestStateFileFromRecordsRejectsInvalidRotationObservationState(t *testing.T) {
	active := loadGoldenFixture(t).Snapshot.keySnapshot()
	normalized, err := active.Normalize()
	if err != nil {
		t.Fatalf("normalize active: %v", err)
	}
	record := keyregistry.SnapshotStateRecordFromSnapshot(normalized)
	record.StableObservationCount = 1

	_, err = keyregistry.NewStateFileFromRecords(
		normalized.KubernetesKeyID,
		[]keyregistry.SnapshotStateRecord{record},
		7,
		"",
	)
	if !errors.Is(err, keyregistry.ErrStateCorrupt) {
		t.Fatalf("expected corrupt observation metadata error, got %v", err)
	}
}

func TestStateFileFromRecordsRejectsStrippedStateSurfaces(t *testing.T) {
	active := loadGoldenFixture(t).Snapshot.keySnapshot()
	normalized, err := active.Normalize()
	if err != nil {
		t.Fatalf("normalize active: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*keyregistry.SnapshotStateRecord)
	}{
		{
			name: "disaster recovery state",
			mutate: func(record *keyregistry.SnapshotStateRecord) {
				record.State = "disaster_recovery"
			},
		},
		{
			name: "AAD disabled mode",
			mutate: func(record *keyregistry.SnapshotStateRecord) {
				record.AADMode = "aad.disabled"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := keyregistry.SnapshotStateRecordFromSnapshot(normalized)
			tt.mutate(&record)
			_, err := keyregistry.NewStateFileFromRecords(
				normalized.KubernetesKeyID,
				[]keyregistry.SnapshotStateRecord{record},
				7,
				"",
			)
			if err == nil {
				t.Fatal("expected stripped state surface to be rejected")
			}
		})
	}
}

func TestLoadStateFileRejectsUnsafePermissions(t *testing.T) {
	path := saveValidStateFile(t)
	// #nosec G302 -- this test intentionally creates unsafe state-file permissions.
	if err := os.Chmod(path, 0o666); err != nil {
		t.Fatalf("chmod state file: %v", err)
	}

	_, _, err := keyregistry.LoadStateFile(path, keyregistry.StateLoadOptions{})
	if !errors.Is(err, keyregistry.ErrStatePermission) {
		t.Fatalf("expected state permission error, got %v", err)
	}
}

func TestLoadStateFileRejectsCorruption(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "key-registry.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatalf("write corrupt state: %v", err)
	}

	_, _, err := keyregistry.LoadStateFile(path, keyregistry.StateLoadOptions{})
	if !errors.Is(err, keyregistry.ErrStateCorrupt) {
		t.Fatalf("expected corrupt state error, got %v", err)
	}
}

func TestLoadStateFileRejectsHashMismatch(t *testing.T) {
	path := saveValidStateFile(t)
	// #nosec G304 -- test reads the local temp state file it just created.
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	var state keyregistry.StateFile
	if err := json.Unmarshal(content, &state); err != nil {
		t.Fatalf("decode state file: %v", err)
	}
	state.CurrentHash = "krs1.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	reencoded, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("encode tampered state: %v", err)
	}
	if err := os.WriteFile(path, reencoded, 0o600); err != nil {
		t.Fatalf("write tampered state: %v", err)
	}

	_, _, err = keyregistry.LoadStateFile(path, keyregistry.StateLoadOptions{})
	if !errors.Is(err, keyregistry.ErrStateCorrupt) {
		t.Fatalf("expected corrupt state error, got %v", err)
	}
}

func TestStateReplayAndRollbackDetection(t *testing.T) {
	active := loadGoldenFixture(t).Snapshot.keySnapshot()
	previous, err := keyregistry.NewStateFile(active, nil, 1, "")
	if err != nil {
		t.Fatalf("new previous state: %v", err)
	}

	path := filepath.Join(t.TempDir(), "key-registry.json")
	if err := keyregistry.SaveStateFile(path, previous); err != nil {
		t.Fatalf("save previous state: %v", err)
	}
	_, _, err = keyregistry.LoadStateFile(path, keyregistry.StateLoadOptions{MinimumGeneration: 2})
	if !errors.Is(err, keyregistry.ErrStateRollback) {
		t.Fatalf("expected replay rollback error, got %v", err)
	}

	lower := historicalSnapshot(active)
	lower.State = keyregistry.StateActive
	if _, err := keyregistry.PromoteState(previous, lower, nil); !errors.Is(err, keyregistry.ErrStateRollback) {
		t.Fatalf("expected active version rollback error, got %v", err)
	}
}

func TestStateCheckpointRejectsRollbackAndSameGenerationHashMismatch(t *testing.T) {
	active := loadGoldenFixture(t).Snapshot.keySnapshot()
	previous, err := keyregistry.NewStateFile(active, nil, 1, "")
	if err != nil {
		t.Fatalf("new previous state: %v", err)
	}
	nextActive := active
	nextActive.TransitVersion++
	nextActive.TransitVersionCreatedAt = active.TransitVersionCreatedAt.Add(time.Hour)
	nextActive.KubernetesKeyID = ""
	nextActive.State = keyregistry.StateActive
	next, err := keyregistry.PromoteState(previous, nextActive, []keyregistry.KeySnapshot{historicalSnapshot(nextActive)})
	if err != nil {
		t.Fatalf("promote state: %v", err)
	}
	checkpoint, err := keyregistry.NewStateCheckpoint(next)
	if err != nil {
		t.Fatalf("new checkpoint: %v", err)
	}

	if err := checkpoint.ValidateState(previous); !errors.Is(err, keyregistry.ErrStateRollback) {
		t.Fatalf("expected checkpoint to reject older generation, got %v", err)
	}

	alternateActive := active
	alternateActive.TransitVersion += 2
	alternateActive.TransitVersionCreatedAt = active.TransitVersionCreatedAt.Add(2 * time.Hour)
	alternateActive.KubernetesKeyID = ""
	alternateActive.State = keyregistry.StateActive
	alternate, err := keyregistry.NewStateFile(alternateActive, nil, next.Generation, next.PreviousHash)
	if err != nil {
		t.Fatalf("new alternate state: %v", err)
	}
	if err := checkpoint.ValidateState(alternate); !errors.Is(err, keyregistry.ErrStateRollback) {
		t.Fatalf("expected checkpoint to reject same-generation hash mismatch, got %v", err)
	}
}

func FuzzParseKeyID(f *testing.F) {
	fixture := loadGoldenFixture(f)
	f.Add(fixture.ExpectedKeyID)
	f.Add("")
	f.Add("obk2.short")
	f.Add("raw-transit-version-3")
	f.Add("obk2.!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!")

	f.Fuzz(func(t *testing.T, keyID string) {
		parsed, err := keyregistry.ParseKeyID(keyID)
		if err != nil {
			return
		}
		if parsed != keyID {
			t.Fatalf("parse changed key ID: %s != %s", parsed, keyID)
		}
		if _, err := keyregistry.ParseKeyID(parsed); err != nil {
			t.Fatalf("parsed key ID is not stable: %v", err)
		}
	})
}

func saveValidStateFile(t *testing.T) string {
	t.Helper()

	active := loadGoldenFixture(t).Snapshot.keySnapshot()
	state, err := keyregistry.NewStateFile(active, nil, 1, "")
	if err != nil {
		t.Fatalf("new state file: %v", err)
	}
	path := filepath.Join(t.TempDir(), "key-registry.json")
	if err := keyregistry.SaveStateFile(path, state); err != nil {
		t.Fatalf("save state file: %v", err)
	}
	return path
}

func historicalSnapshot(active keyregistry.KeySnapshot) keyregistry.KeySnapshot {
	historical := active
	historical.TransitVersion = active.TransitVersion - 1
	historical.TransitVersionCreatedAt = active.TransitVersionCreatedAt.Add(-time.Hour)
	historical.KubernetesKeyID = ""
	historical.State = keyregistry.StateRetired
	return historical
}

func loadGoldenFixture(t testing.TB) goldenFixture {
	t.Helper()

	const goldenFixturePath = "../../test/testdata/key-aad/golden-v1.json"
	content, err := os.ReadFile(goldenFixturePath)
	if err != nil {
		t.Fatalf("read golden fixture: %v", err)
	}

	var fixture goldenFixture
	if err := json.Unmarshal(content, &fixture); err != nil {
		t.Fatalf("decode golden fixture: %v", err)
	}
	return fixture
}

func (s snapshotFixture) keySnapshot() keyregistry.KeySnapshot {
	return keyregistry.KeySnapshot{
		ProviderName:            s.ProviderName,
		ClusterID:               s.ClusterID,
		OpenBaoInstanceID:       s.OpenBaoInstanceID,
		OpenBaoNamespace:        s.OpenBaoNamespace,
		TransitMountID:          s.TransitMountID,
		TransitKeyLineageID:     s.TransitKeyLineageID,
		TransitVersion:          s.TransitVersion,
		TransitVersionCreatedAt: time.Unix(s.TransitVersionCreatedAtUnix, 0).UTC(),
		State:                   keyregistry.SnapshotState(s.State),
		AADMode:                 keyregistry.AADMode(s.AADMode),
	}
}
