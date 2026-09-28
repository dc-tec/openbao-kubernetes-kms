package main

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/config"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/status"
)

func TestRetirementRecoversRestrictionWithNewerRemoteVersion(t *testing.T) {
	for _, pending := range []bool{false, true} {
		cfg, previous, profile := prepareRetirement(t)
		observer, err := newRotationObserver(cfg)
		if err != nil {
			t.Fatal(err)
		}
		profile.LatestVersion = 3
		profile.VersionCreationTimes = append(profile.VersionCreationTimes, openbao.KeyVersion{
			Version: 3, CreatedAt: profile.VersionCreationTimes[1].CreatedAt.Add(time.Hour),
		})
		if pending {
			result, err := observer.Discover(previous, profile, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			previous = result.State
			if err := (status.FileStateStore{Path: cfg.State.Path}).Save(previous); err != nil {
				t.Fatal(err)
			}
		}
		profile.MinDecryptionVersion = 2
		profile.MinEncryptionVersion = 3
		if _, err := observer.Observe(previous, profile, time.Now(), true); err == nil {
			t.Fatal("expected unusable history to block observation")
		}
		read := func(context.Context, config.Config) (openbao.KeyProfile, error) { return profile, nil }
		report, err := runRetirement(t.Context(), cfg, retirementOptions{
			BeforeVersion: 2, Apply: true, ExpectedStateHash: previous.CurrentHash,
		}, read)
		if err != nil {
			t.Fatal(err)
		}
		assertRetirementSavedState(t, cfg.State.Path, report.NextStateHash, previous.ActiveKeyID)
		next, err := (status.FileStateStore{Path: cfg.State.Path}).Load()
		if err != nil {
			t.Fatal(err)
		}
		for i, record := range previous.Snapshots {
			if record.TransitVersion >= 2 && next.Snapshots[i] != record {
				t.Fatal("recovery changed active or pending observations")
			}
		}
		if _, err := observer.Observe(next, profile, time.Now(), false); err != nil {
			t.Fatalf("retirement did not restore rotation progress: %v", err)
		}
	}
}

func TestRestoreRequiresUsableMatchingMetadataAndCurrentCheckpoint(t *testing.T) {
	for _, scenario := range []string{"success", "restricted", "trimmed", "identity-drift", "stale", "locked"} {
		t.Run(scenario, func(t *testing.T) {
			cfg, previous, profile := prepareRetirement(t)
			removed, err := keyregistry.RetireVersions(previous, 2)
			if err != nil {
				t.Fatal(err)
			}
			if err := (status.FileStateStore{Path: cfg.State.Path}).Save(removed); err != nil {
				t.Fatal(err)
			}
			read := func(context.Context, config.Config) (openbao.KeyProfile, error) { return profile, nil }
			opts := retirementOptions{RestoreVersions: []int{1}}
			plan, err := runRetirement(t.Context(), cfg, opts, read)
			if err != nil || len(plan.RestoredVersions) != 1 || plan.Operation != "restore-versions" {
				t.Fatalf("invalid restore plan: %+v %v", plan, err)
			}
			assertRetirementSavedState(t, cfg.State.Path, removed.CurrentHash, removed.ActiveKeyID)
			opts.Apply, opts.ExpectedStateHash = true, plan.StateHash
			mutateRestoreScenario(t, scenario, cfg, previous, &profile, &opts)
			report, err := runRetirement(t.Context(), cfg, opts, read)
			if scenario != "success" {
				if err == nil {
					t.Fatal("unsafe restore accepted")
				}
				assertRetirementSavedState(t, cfg.State.Path, removed.CurrentHash, removed.ActiveKeyID)
				return
			}
			if err != nil || report.NextGeneration != removed.Generation+1 {
				t.Fatalf("restore failed: %v", err)
			}
			assertRetirementSavedState(t, cfg.State.Path, plan.NextStateHash, previous.ActiveKeyID)
		})
	}
}

func TestRetirementNoOpDoesNotRewriteConfirmedFiles(t *testing.T) {
	cfg, previous, profile := prepareRetirement(t)
	removed, err := keyregistry.RetireVersions(previous, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := (status.FileStateStore{Path: cfg.State.Path}).Save(removed); err != nil {
		t.Fatal(err)
	}
	paths := []string{cfg.State.Path, keyregistry.StateCheckpointPath(cfg.State.Path)}
	before := make([]os.FileInfo, len(paths))
	for i, path := range paths {
		before[i], err = os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
	}
	read := func(context.Context, config.Config) (openbao.KeyProfile, error) { return profile, nil }
	if _, err := runRetirement(t.Context(), cfg, retirementOptions{
		BeforeVersion: 2, Apply: true, ExpectedStateHash: removed.CurrentHash,
	}, read); err != nil {
		t.Fatal(err)
	}
	for i, path := range paths {
		after, err := os.Stat(path)
		if err != nil || !os.SameFile(before[i], after) || before[i].ModTime() != after.ModTime() {
			t.Fatalf("no-op rewrote %s: %v", path, err)
		}
	}
}

func mutateRestoreScenario(t *testing.T, scenario string, cfg config.Config, previous keyregistry.StateFile,
	profile *openbao.KeyProfile, opts *retirementOptions,
) {
	t.Helper()
	switch scenario {
	case "restricted":
		profile.MinDecryptionVersion = 2
	case "trimmed":
		profile.MinAvailableVersion = 2
		profile.VersionCreationTimes = profile.VersionCreationTimes[1:]
	case "identity-drift":
		profile.VersionCreationTimes[0].CreatedAt = profile.VersionCreationTimes[0].CreatedAt.Add(time.Hour)
	case "stale":
		opts.ExpectedStateHash = previous.CurrentHash
	case "locked":
		lock, err := keyregistry.LockState(cfg.State.Path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = lock.Close() })
	}
}
