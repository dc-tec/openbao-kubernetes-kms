package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/cli"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/config"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/status"
)

func TestPersistedStateRejectsBackendPathDrift(t *testing.T) {
	for _, field := range []string{"key name", "mount path"} {
		t.Run(field, func(t *testing.T) {
			cfg, original, profile := prepareRetirement(t)
			switch field {
			case "key name":
				cfg.Transit.KeyName = "different-key"
				profile.Name = cfg.Transit.KeyName
			case "mount path":
				cfg.Transit.MountPath = "different-mount"
			}
			// The replacement backend has the same versions and creation timestamps.
			client := fakeDiagnosticTransitClient{profile: profile, disableUpsert: true}
			if _, _, _, err := buildStatusRuntime(cfg, client, nil); !errors.Is(err, status.ErrConfigInvalid) {
				t.Fatalf("runtime accepted backend drift: %v", err)
			}
			if _, err := loadConfiguredRegistryState(cfg); !errors.Is(err, status.ErrConfigInvalid) {
				t.Fatalf("rotation diagnostics accepted backend drift: %v", err)
			}
			report := cli.Report{Name: "doctor"}
			checkRegistryVersionRestrictions(&report, cfg, profile)
			if !report.HasFailures() {
				t.Fatal("doctor accepted backend drift")
			}
			readProfile := func(context.Context, config.Config) (openbao.KeyProfile, error) { return profile, nil }
			opts := retirementOptions{BeforeVersion: 2, Apply: true, ExpectedStateHash: original.CurrentHash}
			if _, err := runRetirement(t.Context(), cfg, opts, readProfile); !errors.Is(err, status.ErrConfigInvalid) {
				t.Fatalf("retirement accepted backend drift: %v", err)
			}
			assertRetirementSavedState(t, cfg.State.Path, original.CurrentHash, original.ActiveKeyID)
		})
	}
}

func TestLegacyUnboundStateRejectedWithoutRewriting(t *testing.T) {
	cfg, _, profile := prepareRetirement(t)
	legacy, err := os.ReadFile("../../test/testdata/keyregistry/state-unbound-v1alpha1.json")
	if err != nil {
		t.Fatal(err)
	}
	// #nosec G703 -- state path belongs to the disposable fixture.
	if err := os.WriteFile(cfg.State.Path, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	var oldState keyregistry.StateFile
	if err := json.Unmarshal(legacy, &oldState); err != nil {
		t.Fatal(err)
	}
	checkpoint := keyregistry.StateCheckpoint{
		SchemaVersion: "keyregistry.openbao-kms/checkpoint/v1alpha1",
		Generation:    oldState.Generation, CurrentHash: oldState.CurrentHash,
	}
	if err := keyregistry.SaveStateCheckpoint(keyregistry.StateCheckpointPath(cfg.State.Path), checkpoint); err != nil {
		t.Fatal(err)
	}
	client := fakeDiagnosticTransitClient{profile: profile, disableUpsert: true}
	if _, _, _, err := buildStatusRuntime(cfg, client, nil); !errors.Is(err, keyregistry.ErrStateCorrupt) ||
		!strings.Contains(err.Error(), "requires fresh bound state") {
		t.Fatalf("legacy state was not rejected clearly: %v", err)
	}
	// #nosec G304 -- state path belongs to the disposable fixture.
	after, err := os.ReadFile(cfg.State.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(legacy, after) {
		t.Fatal("legacy rejection rewrote state")
	}
	anchored, err := keyregistry.LoadStateCheckpoint(keyregistry.StateCheckpointPath(cfg.State.Path))
	if err != nil || anchored != checkpoint {
		t.Fatalf("legacy rejection changed checkpoint: %v", err)
	}
}
