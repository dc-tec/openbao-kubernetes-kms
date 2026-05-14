package keyregistry_test

import (
	"errors"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
)

func TestKeyIDCreationTimeInvariantCatalog(t *testing.T) {
	base := loadGoldenFixture(t).Snapshot.keySnapshot()
	baseKeyID, err := keyregistry.DeriveKeyID(base)
	if err != nil {
		t.Fatalf("derive base key_id: %v", err)
	}

	sameSecond := base
	sameSecond.TransitVersionCreatedAt = base.TransitVersionCreatedAt.Add(999 * time.Millisecond)
	sameSecondKeyID, err := keyregistry.DeriveKeyID(sameSecond)
	if err != nil {
		t.Fatalf("derive same-second key_id: %v", err)
	}
	if sameSecondKeyID != baseKeyID {
		t.Fatalf("same Unix-second creation time changed key_id: base %s same-second %s", baseKeyID, sameSecondKeyID)
	}

	nextSecond := base
	nextSecond.TransitVersionCreatedAt = base.TransitVersionCreatedAt.Add(time.Second)
	nextSecondKeyID, err := keyregistry.DeriveKeyID(nextSecond)
	if err != nil {
		t.Fatalf("derive next-second key_id: %v", err)
	}
	if nextSecondKeyID == baseKeyID {
		t.Fatalf("different Unix-second creation time reused key_id %s", baseKeyID)
	}
}

func TestKeyIDNamespaceInvariantCatalog(t *testing.T) {
	base := loadGoldenFixture(t).Snapshot.keySnapshot()
	base.OpenBaoNamespace = ""
	baseKeyID, err := keyregistry.DeriveKeyID(base)
	if err != nil {
		t.Fatalf("derive base key_id: %v", err)
	}

	namespaced := base
	namespaced.OpenBaoNamespace = "admin/workload-a"
	namespacedKeyID, err := keyregistry.DeriveKeyID(namespaced)
	if err != nil {
		t.Fatalf("derive namespaced key_id: %v", err)
	}
	if namespacedKeyID == baseKeyID {
		t.Fatalf("empty and configured OpenBao namespace reused key_id %s", baseKeyID)
	}
}

func TestStateFileRejectsPersistedMismatchedKeyID(t *testing.T) {
	active, err := loadGoldenFixture(t).Snapshot.keySnapshot().Normalize()
	if err != nil {
		t.Fatalf("normalize active snapshot: %v", err)
	}
	record := keyregistry.SnapshotStateRecordFromSnapshot(active)

	other := active
	other.TransitVersion++
	other.TransitVersionCreatedAt = active.TransitVersionCreatedAt.Add(time.Hour)
	other.KubernetesKeyID = ""
	otherKeyID, err := keyregistry.DeriveKeyID(other)
	if err != nil {
		t.Fatalf("derive different snapshot key_id: %v", err)
	}
	record.KubernetesKeyID = otherKeyID

	_, err = keyregistry.NewStateFileFromRecords(
		active.KubernetesKeyID,
		[]keyregistry.SnapshotStateRecord{record},
		1,
		"",
	)
	if err == nil {
		t.Fatal("expected persisted snapshot with mismatched embedded key_id to fail closed")
	}
}

func TestRegistryDistinguishesMalformedAndUnknownKeyID(t *testing.T) {
	active := loadGoldenFixture(t).Snapshot.keySnapshot()
	registry, err := keyregistry.NewRegistry(active, nil)
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}

	unknown := active
	unknown.TransitVersion++
	unknown.TransitVersionCreatedAt = active.TransitVersionCreatedAt.Add(time.Hour)
	unknown.KubernetesKeyID = ""
	unknownKeyID, err := keyregistry.DeriveKeyID(unknown)
	if err != nil {
		t.Fatalf("derive unknown key_id: %v", err)
	}

	if _, err := registry.Lookup("not-a-key-id"); !errors.Is(err, keyregistry.ErrMalformedKeyID) {
		t.Fatalf("malformed key_id returned %v", err)
	}
	if _, err := registry.Lookup(unknownKeyID); !errors.Is(err, keyregistry.ErrUnknownKeyID) {
		t.Fatalf("unknown well-formed key_id returned %v", err)
	}
}
