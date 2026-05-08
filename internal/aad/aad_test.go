package aad_test

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/aad"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
)

type goldenFixture struct {
	Snapshot             snapshotFixture   `json:"snapshot"`
	PluginVersion        string            `json:"pluginVersion"`
	ExpectedKeyID        string            `json:"expectedKeyId"`
	ExpectedAnnotations  map[string]string `json:"expectedAnnotations"`
	ExpectedCanonicalAAD string            `json:"expectedCanonicalAAD"`
	ExpectedTransitAAD   string            `json:"expectedTransitAAD"`
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

type malformedAnnotationFixture struct {
	Name        string            `json:"name"`
	Annotations map[string]string `json:"annotations"`
}

func TestBuildAnnotationsGolden(t *testing.T) {
	fixture := loadGoldenFixture(t)

	annotations, err := aad.BuildAnnotations(fixture.Snapshot.keySnapshot(), fixture.PluginVersion)
	if err != nil {
		t.Fatalf("build annotations: %v", err)
	}
	if !reflect.DeepEqual(annotations, fixture.ExpectedAnnotations) {
		t.Fatalf("annotations mismatch:\nwant %#v\ngot  %#v", fixture.ExpectedAnnotations, annotations)
	}

	parsed, err := aad.ParseAnnotations(annotations)
	if err != nil {
		t.Fatalf("parse annotations: %v", err)
	}
	if parsed.TransitKeyVersion != fixture.Snapshot.TransitVersion {
		t.Fatalf("unexpected parsed Transit version: %d", parsed.TransitKeyVersion)
	}
}

func TestBuildCanonicalAADGolden(t *testing.T) {
	fixture := loadGoldenFixture(t)
	snapshot := fixture.Snapshot.keySnapshot()

	canonical, err := aad.BuildCanonical(snapshot, fixture.ExpectedAnnotations)
	if err != nil {
		t.Fatalf("build canonical AAD: %v", err)
	}
	if string(canonical) != fixture.ExpectedCanonicalAAD {
		t.Fatalf("canonical AAD mismatch:\nwant %s\ngot  %s", fixture.ExpectedCanonicalAAD, string(canonical))
	}
	if got := aad.EncodeForTransit(canonical); got != fixture.ExpectedTransitAAD {
		t.Fatalf("Transit AAD mismatch:\nwant %s\ngot  %s", fixture.ExpectedTransitAAD, got)
	}
}

func TestAnnotationOrderDoesNotAffectAAD(t *testing.T) {
	fixture := loadGoldenFixture(t)
	snapshot := fixture.Snapshot.keySnapshot()

	reordered := map[string]string{
		aad.KeyAADVersion:        fixture.ExpectedAnnotations[aad.KeyAADVersion],
		aad.KeyPluginVersion:     fixture.ExpectedAnnotations[aad.KeyPluginVersion],
		aad.KeyTransitKeyHash:    fixture.ExpectedAnnotations[aad.KeyTransitKeyHash],
		aad.KeyTransitMountHash:  fixture.ExpectedAnnotations[aad.KeyTransitMountHash],
		aad.KeyTransitKeyVersion: fixture.ExpectedAnnotations[aad.KeyTransitKeyVersion],
		aad.KeyKeyIDHash:         fixture.ExpectedAnnotations[aad.KeyKeyIDHash],
		aad.KeyProvider:          fixture.ExpectedAnnotations[aad.KeyProvider],
	}

	canonical, err := aad.BuildCanonical(snapshot, reordered)
	if err != nil {
		t.Fatalf("build canonical AAD from reordered annotations: %v", err)
	}
	if string(canonical) != fixture.ExpectedCanonicalAAD {
		t.Fatalf("annotation order changed canonical AAD:\nwant %s\ngot  %s", fixture.ExpectedCanonicalAAD, string(canonical))
	}
}

func TestMalformedAnnotationsNeverPanic(t *testing.T) {
	fixture := loadGoldenFixture(t)
	cases := loadMalformedAnnotationFixtures(t)

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			if _, err := aad.ParseAnnotations(tc.Annotations); err == nil {
				t.Fatal("expected malformed annotations to fail parsing")
			}
			if _, err := aad.BuildCanonical(fixture.Snapshot.keySnapshot(), tc.Annotations); err == nil {
				t.Fatal("expected malformed annotations to fail canonical AAD build")
			}
		})
	}
}

func TestAnnotationMismatchFails(t *testing.T) {
	fixture := loadGoldenFixture(t)
	annotations := copyAnnotations(fixture.ExpectedAnnotations)
	annotations[aad.KeyKeyIDHash] = aad.HashValue("different-key-id")

	parsed, err := aad.ParseAnnotations(annotations)
	if err != nil {
		t.Fatalf("parse annotations with valid but mismatched hash: %v", err)
	}
	if err := aad.ValidateForSnapshot(fixture.Snapshot.keySnapshot(), parsed); !errors.Is(err, aad.ErrAnnotationMismatch) {
		t.Fatalf("expected annotation mismatch, got %v", err)
	}
}

func TestAnnotationsAndAADDoNotExposeRawTopology(t *testing.T) {
	fixture := loadGoldenFixture(t)
	snapshot := fixture.Snapshot.keySnapshot()

	annotations, err := aad.BuildAnnotations(snapshot, fixture.PluginVersion)
	if err != nil {
		t.Fatalf("build annotations: %v", err)
	}
	canonical, err := aad.BuildCanonical(snapshot, annotations)
	if err != nil {
		t.Fatalf("build canonical AAD: %v", err)
	}

	annotationValues := strings.Join(values(annotations), "\n")
	for _, raw := range []string{
		snapshot.TransitMountID,
		snapshot.TransitKeyLineageID,
		snapshot.OpenBaoInstanceID,
	} {
		if strings.Contains(annotationValues, raw) {
			t.Fatalf("annotations exposed raw topology value %q", raw)
		}
		if strings.Contains(string(canonical), raw) {
			t.Fatalf("canonical AAD exposed raw topology value %q", raw)
		}
	}
}

func FuzzParseAnnotations(f *testing.F) {
	fixture := loadGoldenFixture(f)
	valid, err := json.Marshal(fixture.ExpectedAnnotations)
	if err != nil {
		f.Fatalf("marshal valid annotations seed: %v", err)
	}
	f.Add(string(valid))
	f.Add("{}")
	f.Add(`{"kms.openbao.org/provider":"openbao-transit"}`)
	f.Add(`{"provider":"openbao-transit"}`)

	f.Fuzz(func(t *testing.T, input string) {
		var annotations map[string]string
		if err := json.Unmarshal([]byte(input), &annotations); err != nil {
			return
		}

		parsed, err := aad.ParseAnnotations(annotations)
		if err != nil {
			return
		}
		if parsed.Provider == "" || parsed.AADVersion == "" {
			t.Fatalf("parsed annotations missing required identity: %#v", parsed)
		}
		if _, err := aad.BuildCanonical(fixture.Snapshot.keySnapshot(), annotations); err != nil {
			return
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

func loadMalformedAnnotationFixtures(t testing.TB) []malformedAnnotationFixture {
	t.Helper()

	const malformedFixturesPath = "../../test/testdata/key-aad/malformed-annotations.json"
	content, err := os.ReadFile(malformedFixturesPath)
	if err != nil {
		t.Fatalf("read malformed annotation fixtures: %v", err)
	}

	var fixtures []malformedAnnotationFixture
	if err := json.Unmarshal(content, &fixtures); err != nil {
		t.Fatalf("decode malformed annotation fixtures: %v", err)
	}
	return fixtures
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

func copyAnnotations(source map[string]string) map[string]string {
	copied := make(map[string]string, len(source))
	for key, value := range source {
		copied[key] = value
	}
	return copied
}

func values(source map[string]string) []string {
	result := make([]string, 0, len(source))
	for _, value := range source {
		result = append(result, value)
	}
	return result
}
