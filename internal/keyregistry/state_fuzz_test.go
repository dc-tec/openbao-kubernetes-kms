package keyregistry

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const (
	fuzzInitialStateFixture = "../../test/testdata/keyregistry/state-initial-v1.json"
	fuzzRotatedStateFixture = "../../test/testdata/keyregistry/state-rotated-v1-v2.json"
)

func FuzzStateFileDecode(f *testing.F) {
	initial := readFuzzFixture(f, fuzzInitialStateFixture)
	rotated := readFuzzFixture(f, fuzzRotatedStateFixture)

	f.Add(string(initial))
	f.Add(string(rotated))
	f.Add("{}")
	f.Add("{")
	f.Add(string(rotated) + "\n{}")
	f.Add(`{"schemaVersion":"keyregistry.openbao-kms/v1alpha1"}`)

	f.Fuzz(func(t *testing.T, input string) {
		state, err := decodeState(bytes.NewReader([]byte(input)))
		if err != nil {
			return
		}
		assertValidDecodedState(t, state)

		encoded, err := json.Marshal(state)
		if err != nil {
			t.Fatalf("marshal decoded state: %v", err)
		}
		reparsed, err := decodeState(bytes.NewReader(encoded))
		if err != nil {
			t.Fatalf("decode marshaled state: %v", err)
		}
		assertEquivalentState(t, state, reparsed)
	})
}

func FuzzStateCheckpointDecode(f *testing.F) {
	initialState, err := decodeState(bytes.NewReader(readFuzzFixture(f, fuzzInitialStateFixture)))
	if err != nil {
		f.Fatalf("decode initial state fixture: %v", err)
	}
	checkpoint, err := NewStateCheckpoint(initialState)
	if err != nil {
		f.Fatalf("build checkpoint seed: %v", err)
	}
	validCheckpoint, err := json.Marshal(checkpoint)
	if err != nil {
		f.Fatalf("marshal checkpoint seed: %v", err)
	}

	f.Add(string(validCheckpoint))
	f.Add("{}")
	f.Add("{")
	f.Add(string(validCheckpoint) + "\n{}")
	f.Add(`{"schemaVersion":"keyregistry.openbao-kms/checkpoint/v1alpha1"}`)

	f.Fuzz(func(t *testing.T, input string) {
		dir := t.TempDir()
		path := filepath.Join(dir, "key-registry.json.checkpoint")
		if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
			t.Fatalf("write checkpoint fuzz input: %v", err)
		}

		checkpoint, err := LoadStateCheckpoint(path)
		if err != nil {
			return
		}
		if err := checkpoint.Validate(); err != nil {
			t.Fatalf("loaded checkpoint failed validation: %v", err)
		}

		encoded, err := json.Marshal(checkpoint)
		if err != nil {
			t.Fatalf("marshal checkpoint: %v", err)
		}
		roundTripPath := filepath.Join(dir, "checkpoint-roundtrip.json")
		if err := os.WriteFile(roundTripPath, encoded, 0o600); err != nil {
			t.Fatalf("write checkpoint round trip: %v", err)
		}
		reparsed, err := LoadStateCheckpoint(roundTripPath)
		if err != nil {
			t.Fatalf("load checkpoint round trip: %v", err)
		}
		if reparsed != checkpoint {
			t.Fatalf("checkpoint round trip changed: %#v != %#v", reparsed, checkpoint)
		}
	})
}

func assertValidDecodedState(t *testing.T, state StateFile) {
	t.Helper()

	if err := state.Validate(); err != nil {
		t.Fatalf("decoded state failed validation: %v", err)
	}
	if _, err := state.ActiveSnapshot(); err != nil {
		t.Fatalf("decoded state active snapshot failed: %v", err)
	}
	if _, err := state.Registry(); err != nil {
		t.Fatalf("decoded state registry failed: %v", err)
	}
}

func assertEquivalentState(t *testing.T, want StateFile, got StateFile) {
	t.Helper()

	if got.SchemaVersion != want.SchemaVersion ||
		got.Generation != want.Generation ||
		got.PreviousHash != want.PreviousHash ||
		got.CurrentHash != want.CurrentHash ||
		got.ActiveKeyID != want.ActiveKeyID ||
		!reflect.DeepEqual(got.Snapshots, want.Snapshots) {
		t.Fatalf("state round trip changed:\nwant %#v\ngot  %#v", want, got)
	}
}

func readFuzzFixture(t testing.TB, path string) []byte {
	t.Helper()

	// #nosec G304 -- fuzz seed fixtures are fixed repository testdata paths.
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fuzz fixture %s: %v", path, err)
	}
	return content
}
