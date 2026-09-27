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

	"github.com/dc-tec/openbao-kubernetes-kms/internal/cli"
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
		TransitMetadataStatus:     cli.CheckPass,
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
	observer, err := newRotationObserver(cfg)
	if err != nil {
		t.Fatalf("new observer: %v", err)
	}
	state, err := observer.RebuildState(commandTestProfile(nil), time.Now().UTC())
	if err != nil {
		t.Fatalf("rebuild state: %v", err)
	}
	return state
}

func TestRotationCommandsFailWhenRemoteCheckCannotRun(t *testing.T) {
	state := testCommandState(t)
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := (status.FileStateStore{Path: statePath}).Save(state); err != nil {
		t.Fatal(err)
	}
	configBytes, err := os.ReadFile("../../test/testdata/config/valid.yaml")
	if err != nil {
		t.Fatal(err)
	}
	configText := strings.ReplaceAll(string(configBytes), "/var/lib/openbao-kms/state/key-registry.json", statePath)
	configText = strings.ReplaceAll(configText, "/etc/openbao-kms/tls/ca.crt",
		filepath.Join(t.TempDir(), "missing-ca.pem"))
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	// #nosec G703 -- configPath is a fixed filename inside t.TempDir().
	if err := os.WriteFile(configPath, []byte(configText), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"rotation-plan", "verify-rotation"} {
		for _, format := range []string{outputFormatText, outputFormatJSON} {
			t.Run(name+"/"+format, func(t *testing.T) {
				output, err := executeCommand(t, name, "--config", configPath, "--output", format)
				if cli.ProcessExitCode(err) != int(cli.ExitCheckFailed) {
					t.Fatalf("expected diagnostic failure exit: %v", err)
				}
				if !strings.Contains(output, "OpenBao diagnostics unavailable") {
					t.Fatalf("missing failure report: %s", output)
				}
				assertFailedRotationReport(t, output, format, state.CurrentHash)
			})
		}
	}
	loaded, err := loadRegistryStateWithCheckpoint(statePath)
	if err != nil || loaded.State.CurrentHash != state.CurrentHash {
		t.Fatalf("diagnostics changed local state: %v", err)
	}
}

func assertFailedRotationReport(t *testing.T, output, format, stateHash string) {
	t.Helper()
	if format == outputFormatJSON {
		var report rotationReportJSON
		if err := json.Unmarshal([]byte(output), &report); err != nil {
			t.Fatal(err)
		}
		if report.TransitMetadataStatus != cli.CheckFail || !report.StateLoaded || report.StateHash != stateHash {
			t.Fatalf("partial report lost local evidence or failure: %#v", report)
		}
	} else if !strings.Contains(output, "transitMetadataStatus: fail") || !strings.Contains(output, "stateLoaded: true") {
		t.Fatalf("partial text report looks successful: %s", output)
	}
}
