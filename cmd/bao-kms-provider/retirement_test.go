package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/cli"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/config"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/status"
)

func prepareRetirement(t *testing.T) (config.Config, keyregistry.StateFile, openbao.KeyProfile) {
	t.Helper()
	cfg := loadCommandConfig(t)
	cfg.State.Path = filepath.Join(t.TempDir(), "registry.json")
	profile := commandTestProfile(func(profile *openbao.KeyProfile) {
		profile.LatestVersion = 2
		profile.VersionCreationTimes = append(profile.VersionCreationTimes, openbao.KeyVersion{
			Version: 2, CreatedAt: profile.VersionCreationTimes[0].CreatedAt.Add(time.Hour),
		})
	})
	observer, err := newRotationObserver(cfg)
	if err != nil {
		t.Fatal(err)
	}
	state, err := observer.RebuildState(profile, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := (status.FileStateStore{Path: cfg.State.Path}).Save(state); err != nil {
		t.Fatal(err)
	}
	return cfg, state, profile
}

func TestRetirementPlanIsReadOnlyAndApplyUsesReviewedHash(t *testing.T) {
	cfg, previous, profile := prepareRetirement(t)
	readProfile := func(context.Context, config.Config) (openbao.KeyProfile, error) { return profile, nil }
	opts := retirementOptions{BeforeVersion: 2}
	plan, err := runRetirement(t.Context(), cfg, opts, readProfile)
	if err != nil {
		t.Fatal(err)
	}
	assertRetirementSavedState(t, cfg.State.Path, previous.CurrentHash, previous.ActiveKeyID)
	if plan.Applied || len(plan.RemovedVersions) != 1 || plan.RemovedVersions[0].TransitVersion != 1 {
		t.Fatalf("unexpected plan: %#v", plan)
	}
	if _, err := os.Stat(cfg.State.Path + ".lock"); !os.IsNotExist(err) {
		t.Fatalf("plan unexpectedly created lock: %v", err)
	}
	opts.Apply = true
	opts.ExpectedStateHash = plan.StateHash
	applied, err := runRetirement(t.Context(), cfg, opts, readProfile)
	if err != nil {
		t.Fatal(err)
	}
	assertRetirementSavedState(t, cfg.State.Path, plan.NextStateHash, previous.ActiveKeyID)
	if !applied.Applied {
		t.Fatal("applied report missing success")
	}
	if _, err := runRetirement(t.Context(), cfg, opts, readProfile); err == nil {
		t.Fatal("stale reviewed hash accepted")
	}
	var out bytes.Buffer
	if err := printRetirementReport(&out, applied, outputFormatJSON); err != nil {
		t.Fatal(err)
	}
	var decoded retirementReport
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil || !decoded.Applied {
		t.Fatalf("invalid JSON report: %v", err)
	}
}

func assertRetirementSavedState(t *testing.T, path, hash, activeID string) {
	t.Helper()
	loaded, err := loadRegistryStateWithCheckpoint(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.State.CurrentHash != hash || loaded.CheckpointHash != hash || loaded.State.ActiveKeyID != activeID {
		t.Fatalf("unexpected state or checkpoint: %#v", loaded)
	}
}

func TestRetirementFailureDoesNotChangeState(t *testing.T) {
	for _, scenario := range []string{
		"unavailable", "locked", "missing-hash", "stale-hash", "newer-version", "unusable", "identity-drift",
	} {
		t.Run(scenario, func(t *testing.T) {
			cfg, previous, profile := prepareRetirement(t)
			opts := retirementOptions{BeforeVersion: 2, Apply: true, ExpectedStateHash: previous.CurrentHash}
			var profileErr error
			switch scenario {
			case "unavailable":
				profileErr = errors.New("OpenBao unavailable")
			case "locked":
				lock, err := keyregistry.LockState(cfg.State.Path)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = lock.Close() }()
			case "missing-hash":
				opts.ExpectedStateHash = ""
			case "stale-hash":
				opts.ExpectedStateHash = "stale"
			case "newer-version":
				profile.LatestVersion++
			case "unusable":
				profile.SoftDeleted = true
			case "identity-drift":
				profile.VersionCreationTimes[1].CreatedAt = profile.VersionCreationTimes[1].CreatedAt.Add(time.Hour)
			}
			readProfile := func(context.Context, config.Config) (openbao.KeyProfile, error) { return profile, profileErr }
			if _, err := runRetirement(t.Context(), cfg, opts, readProfile); err == nil {
				t.Fatal("unsafe retirement accepted")
			}
			assertRetirementSavedState(t, cfg.State.Path, previous.CurrentHash, previous.ActiveKeyID)
		})
	}
}

func TestRetirementUsageRejectsIncompleteApply(t *testing.T) {
	for _, args := range [][]string{
		{"retire-versions"},
		{"retire-versions", "--before-version", "1"},
		{"retire-versions", "--before-version", "2", "--apply"},
		{"retire-versions", "--before-version", "2", "--output", "xml"},
	} {
		if _, err := executeCommand(t, args...); cli.ProcessExitCode(err) != int(cli.ExitUsage) {
			t.Fatalf("expected usage exit for %v, got %v", args, err)
		}
	}
}

func TestRetirementReportsPartialSaveAndCheckpointRecovers(t *testing.T) {
	cfg, previous, profile := prepareRetirement(t)
	checkpointTemp := filepath.Join(filepath.Dir(cfg.State.Path), ".registry.json.checkpoint.tmp")
	blocker := filepath.Join(checkpointTemp, "blocker")
	// A nonempty directory prevents checkpoint temp-file creation after the
	// state rename succeeds. This models the two-file commit's partial failure.
	if err := os.MkdirAll(blocker, 0o700); err != nil {
		t.Fatal(err)
	}
	readProfile := func(context.Context, config.Config) (openbao.KeyProfile, error) { return profile, nil }
	opts := retirementOptions{BeforeVersion: 2, Apply: true, ExpectedStateHash: previous.CurrentHash}
	if _, err := runRetirement(t.Context(), cfg, opts, readProfile); err == nil {
		t.Fatal("partial save reported success")
	}
	loaded, err := loadRegistryStateWithCheckpoint(cfg.State.Path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.State.CurrentHash == previous.CurrentHash || loaded.CheckpointStatus != stateCheckpointStatusBehind {
		t.Fatalf("expected new state with old checkpoint: %#v", loaded)
	}
	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(checkpointTemp); err != nil {
		t.Fatal(err)
	}
	if _, err := (status.FileStateStore{Path: cfg.State.Path}).Load(); err != nil {
		t.Fatalf("startup could not repair checkpoint: %v", err)
	}
	assertRetirementSavedState(t, cfg.State.Path, loaded.State.CurrentHash, previous.ActiveKeyID)
}

func TestRetirementConfirmsBehindCheckpointBeforeAdvancing(t *testing.T) {
	for _, failCheckpoint := range []bool{false, true} {
		t.Run(fmt.Sprintf("checkpoint_failure_%t", failCheckpoint), func(t *testing.T) {
			cfg, previous, profile := prepareRetirement(t)
			ahead, err := keyregistry.NewStateFileFromRecords(
				previous.ActiveKeyID, previous.Snapshots, previous.Generation+1, previous.CurrentHash,
				previous.IdentityFingerprint,
			)
			if err != nil {
				t.Fatal(err)
			}
			if err := keyregistry.SaveStateFile(cfg.State.Path, ahead); err != nil {
				t.Fatal(err)
			}
			if failCheckpoint {
				blocker := filepath.Join(filepath.Dir(cfg.State.Path), ".registry.json.checkpoint.tmp", "blocker")
				if err := os.MkdirAll(blocker, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			readProfile := func(context.Context, config.Config) (openbao.KeyProfile, error) { return profile, nil }
			opts := retirementOptions{BeforeVersion: 2, Apply: true, ExpectedStateHash: ahead.CurrentHash}
			report, err := runRetirement(t.Context(), cfg, opts, readProfile)
			if failCheckpoint {
				if err == nil {
					t.Fatal("retirement advanced without confirming the prior checkpoint")
				}
				loaded, loadErr := loadRegistryStateWithCheckpoint(cfg.State.Path)
				if loadErr != nil {
					t.Fatalf("failed retirement left an unrecoverable pair: %v", loadErr)
				}
				if loaded.State.CurrentHash != ahead.CurrentHash || loaded.CheckpointHash != previous.CurrentHash {
					t.Fatal("failed checkpoint repair changed the persistence pair")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			assertRetirementSavedState(t, cfg.State.Path, report.NextStateHash, previous.ActiveKeyID)
		})
	}
}
