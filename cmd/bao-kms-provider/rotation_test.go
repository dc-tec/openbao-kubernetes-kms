package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/status"
)

func TestRotationReportRejectsMissingStateAfterTransitRotation(t *testing.T) {
	cfg := loadCommandConfig(t)
	profile := commandTestProfile(func(profile *openbao.KeyProfile) {
		profile.LatestVersion = 2
		profile.VersionCreationTimes = append(profile.VersionCreationTimes, openbao.KeyVersion{
			Version:   2,
			CreatedAt: time.Unix(1_778_277_660, 0).UTC(),
		})
	})

	_, err := applyTransitProfileToRotationReport(
		cfg,
		rotationReport{Name: reportNameRotation, RotationState: status.RotationStateUnknown},
		profile,
		time.Now().UTC(),
	)
	if !errors.Is(err, status.ErrStateUnavailable) {
		t.Fatalf("expected missing rotated state to fail closed, got %v", err)
	}
	if !strings.Contains(err.Error(), "latest_version=2") {
		t.Fatalf("expected bootstrap denial reason in error, got %v", err)
	}
}

func TestRotationReportAllowsMissingStateForInitialBootstrap(t *testing.T) {
	cfg := loadCommandConfig(t)
	profile := commandTestProfile(nil)

	report, err := applyTransitProfileToRotationReport(
		cfg,
		rotationReport{Name: reportNameRotation, RotationState: status.RotationStateUnknown},
		profile,
		time.Now().UTC(),
	)
	if err != nil {
		t.Fatalf("expected initial missing state report to rebuild: %v", err)
	}
	if report.RotationState != status.RotationStateActive {
		t.Fatalf("unexpected rotation state: %s", report.RotationState)
	}
	if report.ActiveTransitVersion != 1 || report.ActiveKeyIDHash == "" {
		t.Fatalf("expected synthesized initial active version in report: %#v", report)
	}
}

func TestRotationReportJSONOutput(t *testing.T) {
	report := rotationReport{
		Name:                      "rotation-plan",
		StateLoaded:               true,
		StateGeneration:           4,
		StateHash:                 "state-hash",
		StateCheckpointLoaded:     true,
		StateCheckpointStatus:     stateCheckpointStatusCurrent,
		StateCheckpointGeneration: 4,
		StateCheckpointHash:       "state-hash",
		ActiveKeyIDHash:           "active-hash",
		ActiveTransitVersion:      3,
		LatestTransitVersion:      4,
		RotationState:             status.RotationStatePending,
		PendingKeyIDHash:          "pending-hash",
		PendingVersion:            4,
		PendingStableCount:        2,
		PendingPromotesAfter:      time.Unix(1_778_277_660, 0).UTC(),
	}

	var out bytes.Buffer
	if err := printRotationReport(&out, report, outputFormatJSON); err != nil {
		t.Fatalf("print JSON: %v", err)
	}

	var decoded rotationReportJSON
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("rotation report JSON is invalid: %v\n%s", err, out.String())
	}
	if decoded.Name != report.Name || decoded.RotationState != report.RotationState {
		t.Fatalf("unexpected decoded report: %#v", decoded)
	}
	if decoded.PendingPromotesAfter == "" {
		t.Fatalf("pending promotion time missing: %#v", decoded)
	}
	if !decoded.StateCheckpointLoaded || decoded.StateCheckpointStatus != stateCheckpointStatusCurrent {
		t.Fatalf("checkpoint status missing: %#v", decoded)
	}
}

func TestRegistryStateLoadRejectsMissingStateWithCheckpoint(t *testing.T) {
	state := testCommandState(t)
	path := filepath.Join(t.TempDir(), "key-registry.json")
	if err := keyregistry.SaveStateFile(path, state); err != nil {
		t.Fatalf("save state: %v", err)
	}
	checkpoint, err := keyregistry.NewStateCheckpoint(state)
	if err != nil {
		t.Fatalf("new checkpoint: %v", err)
	}
	if err := keyregistry.SaveStateCheckpoint(keyregistry.StateCheckpointPath(path), checkpoint); err != nil {
		t.Fatalf("save checkpoint: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove state: %v", err)
	}

	_, err = loadRegistryStateWithCheckpoint(path)
	if !errors.Is(err, keyregistry.ErrStateRollback) {
		t.Fatalf("expected checkpoint-backed missing state to fail as rollback, got %v", err)
	}
}

func TestRegistryStateLoadReportsMissingCheckpoint(t *testing.T) {
	state := testCommandState(t)
	path := filepath.Join(t.TempDir(), "key-registry.json")
	if err := keyregistry.SaveStateFile(path, state); err != nil {
		t.Fatalf("save state: %v", err)
	}

	loaded, err := loadRegistryStateWithCheckpoint(path)
	if err != nil {
		t.Fatalf("load state without checkpoint: %v", err)
	}
	if !loaded.StateLoaded || loaded.CheckpointLoaded {
		t.Fatalf("unexpected checkpoint load state: %#v", loaded)
	}
	if loaded.CheckpointStatus != stateCheckpointStatusMissing {
		t.Fatalf("unexpected checkpoint status: %s", loaded.CheckpointStatus)
	}
}

func testCommandState(t *testing.T) keyregistry.StateFile {
	t.Helper()

	cfg := loadCommandConfig(t)
	observer, err := status.NewObserver(snapshotScope(cfg), rotationPolicy(cfg))
	if err != nil {
		t.Fatalf("new observer: %v", err)
	}
	state, err := observer.RebuildState(commandTestProfile(nil), time.Now().UTC())
	if err != nil {
		t.Fatalf("rebuild state: %v", err)
	}
	return state
}
