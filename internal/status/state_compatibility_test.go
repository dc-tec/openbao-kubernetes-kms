package status_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
)

const (
	initialStateFixturePath = "../../test/testdata/keyregistry/state-initial-v1.json"
	rotatedStateFixturePath = "../../test/testdata/keyregistry/state-rotated-v1-v2.json"
)

func TestStateCompatibilityGoldenFixtures(t *testing.T) {
	clock := newFakeClock()
	base := clock.Now()
	observer := newTestObserver(t, clock, 1, 0)
	initial := rebuildState(t, observer, profileForLatest(1, base), clock.Now())
	assertStateMatchesFixture(t, initial, initialStateFixturePath)

	clock.Advance(time.Minute)
	promoted, err := observer.Observe(initial, profileForLatest(2, base), clock.Now())
	if err != nil {
		t.Fatalf("promote fixture state: %v", err)
	}
	assertStateMatchesFixture(t, promoted.State, rotatedStateFixturePath)
}

func TestStateCompatibilityFixturesLoadAndRetainHistoricalKeyID(t *testing.T) {
	initial := loadStateFixture(t, initialStateFixturePath)
	initialActive, err := initial.ActiveSnapshot()
	if err != nil {
		t.Fatalf("initial active snapshot: %v", err)
	}
	if initialActive.TransitVersion != 1 {
		t.Fatalf("unexpected initial active version: %d", initialActive.TransitVersion)
	}

	rotated := loadStateFixture(t, rotatedStateFixturePath)
	registry, err := rotated.Registry()
	if err != nil {
		t.Fatalf("rotated registry: %v", err)
	}
	rotatedActive, ok := registry.Active()
	if !ok {
		t.Fatal("rotated registry missing active snapshot")
	}
	if rotatedActive.TransitVersion != 2 {
		t.Fatalf("unexpected rotated active version: %d", rotatedActive.TransitVersion)
	}

	historical, err := registry.Lookup(initialActive.KubernetesKeyID)
	if err != nil {
		t.Fatalf("historical key_id from initial fixture is not lookupable after rotation: %v", err)
	}
	if historical.State != keyregistry.StateRetired || historical.TransitVersion != 1 {
		t.Fatalf("unexpected historical snapshot: %+v", historical)
	}
}

func TestStateCompatibilityMutatedFixturesFailClosed(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*keyregistry.StateFile)
		wantError error
	}{
		{
			name: "current hash mismatch",
			mutate: func(state *keyregistry.StateFile) {
				state.CurrentHash = "krs1.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
			},
			wantError: keyregistry.ErrStateCorrupt,
		},
		{
			name: "embedded key_id mismatch",
			mutate: func(state *keyregistry.StateFile) {
				state.Snapshots[0].KubernetesKeyID = "obk2.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
			},
			wantError: keyregistry.ErrStateCorrupt,
		},
		{
			name: "unsupported AAD mode",
			mutate: func(state *keyregistry.StateFile) {
				state.Snapshots[0].AADMode = "aad.disabled"
			},
			wantError: keyregistry.ErrStateCorrupt,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := loadStateFixture(t, rotatedStateFixturePath)
			tt.mutate(&state)
			path := filepath.Join(t.TempDir(), "key-registry.json")
			writeStateFixtureForLoad(t, path, state)

			_, _, err := keyregistry.LoadStateFile(path, keyregistry.StateLoadOptions{})
			if !errors.Is(err, tt.wantError) {
				t.Fatalf("expected %v, got %v", tt.wantError, err)
			}
		})
	}
}

func assertStateMatchesFixture(t testing.TB, state keyregistry.StateFile, path string) {
	t.Helper()

	generated, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatalf("marshal generated state: %v", err)
	}
	// #nosec G304 -- compatibility fixture paths are fixed test constants.
	expected, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read state fixture %s: %v", path, err)
	}
	got := string(append(generated, '\n'))
	if got != string(expected) {
		t.Fatalf("state fixture drift for %s:\nwant %s\ngot  %s", path, expected, got)
	}
}

func loadStateFixture(t testing.TB, path string) keyregistry.StateFile {
	t.Helper()

	// #nosec G304 -- compatibility fixture paths are fixed test constants.
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read state fixture %s: %v", path, err)
	}
	var state keyregistry.StateFile
	if err := json.Unmarshal(content, &state); err != nil {
		t.Fatalf("decode state fixture %s: %v", path, err)
	}
	if err := state.Validate(); err != nil {
		t.Fatalf("validate state fixture %s: %v", path, err)
	}
	return state
}

func writeStateFixtureForLoad(t testing.TB, path string, state keyregistry.StateFile) {
	t.Helper()

	content, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatalf("marshal mutated fixture: %v", err)
	}
	if err := os.WriteFile(path, append(content, '\n'), 0o600); err != nil {
		t.Fatalf("write mutated fixture: %v", err)
	}
}
