package keyregistry_test

import (
	"encoding/json"
	"errors"
	"os"
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
	TransitMountID              string `json:"transitMountId"`
	TransitKeyLineageID         string `json:"transitKeyLineageId"`
	TransitVersion              int    `json:"transitVersion"`
	TransitVersionCreatedAtUnix int64  `json:"transitVersionCreatedAtUnix"`
	KeyEpoch                    string `json:"keyEpoch"`
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
		{
			name: "key epoch",
			change: func(snapshot keyregistry.KeySnapshot) keyregistry.KeySnapshot {
				snapshot.KeyEpoch = "emergency-epoch-1"
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
		TransitMountID:          s.TransitMountID,
		TransitKeyLineageID:     s.TransitKeyLineageID,
		TransitVersion:          s.TransitVersion,
		TransitVersionCreatedAt: time.Unix(s.TransitVersionCreatedAtUnix, 0).UTC(),
		KeyEpoch:                s.KeyEpoch,
		State:                   keyregistry.SnapshotState(s.State),
		AADMode:                 keyregistry.AADMode(s.AADMode),
	}
}
