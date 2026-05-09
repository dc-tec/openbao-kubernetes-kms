//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/test/e2e/framework"
)

func TestProviderOpenBaoBackendReplacementE2E(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), providerFailureDefaultTimeout)
	defer cancel()

	dockerPath := requireDocker(t, ctx)
	prefix := fmt.Sprintf("obk-e2e-backend-replace-%d", time.Now().UnixNano())
	openBaoStorageVolume := prefix + "-bao-data"
	createDockerVolumes(t, ctx, dockerPath, openBaoStorageVolume)
	t.Cleanup(func() {
		removeVolume(t, context.Background(), dockerPath, openBaoStorageVolume)
	})

	stack := startProviderFailureStack(t, ctx, "obk-e2e-backend-replace", providerFailureStackOptions{
		Environment: framework.OpenBaoEnvironmentConfig{
			StorageVolume: openBaoStorageVolume,
		},
	})
	stack.runClient(ctx, "write-client", kmsClientModeWriteSample, sampleReadWrite)

	if err := stack.environment.StopContainerKeepAddress(ctx); err != nil {
		t.Fatalf("stop OpenBao backend: %v", err)
	}
	stack.runClient(ctx, "outage-client", kmsClientModeExpectOutage, sampleReadOnly)

	if err := stack.environment.StartStoppedContainer(ctx); err != nil {
		t.Fatalf("start replacement OpenBao backend: %v", err)
	}
	stack.runClient(ctx, "restored-client", kmsClientModeReadSample, sampleReadOnly)
}

func TestProviderContainerizedDRRestoreE2E(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), providerFailureDefaultTimeout)
	defer cancel()

	dockerPath := requireDocker(t, ctx)
	prefix := fmt.Sprintf("obk-e2e-dr-restore-%d", time.Now().UnixNano())
	primaryVolume := prefix + "-bao-primary"
	backupVolume := prefix + "-bao-backup"
	restoreVolume := prefix + "-bao-restore"
	createDockerVolumes(t, ctx, dockerPath, primaryVolume, backupVolume, restoreVolume)
	t.Cleanup(func() {
		removeVolume(t, context.Background(), dockerPath, primaryVolume)
		removeVolume(t, context.Background(), dockerPath, backupVolume)
		removeVolume(t, context.Background(), dockerPath, restoreVolume)
	})

	stack := startProviderFailureStack(t, ctx, "obk-e2e-dr-restore", providerFailureStackOptions{
		Environment: framework.OpenBaoEnvironmentConfig{
			StorageVolume: primaryVolume,
		},
	})
	stack.runClient(ctx, "write-client", kmsClientModeWriteSample, sampleReadWrite)
	snapshotPath := filepath.Join(t.TempDir(), "openbao-raft.snap")
	if err := stack.environment.SaveRaftSnapshot(ctx, snapshotPath); err != nil {
		t.Fatalf("save OpenBao raft snapshot: %v", err)
	}

	if err := stack.environment.StopContainerKeepAddress(ctx); err != nil {
		t.Fatalf("stop primary OpenBao backend: %v", err)
	}
	if err := stack.environment.RestoreRaftSnapshot(ctx, restoreVolume, snapshotPath); err != nil {
		t.Fatalf("restore OpenBao backend from raft snapshot: %v", err)
	}
	stack.runClient(ctx, "restored-client", kmsClientModeReadSample, sampleReadOnly)
}

func createDockerVolumes(t *testing.T, ctx context.Context, dockerPath string, names ...string) {
	t.Helper()

	for _, name := range names {
		runDocker(t, ctx, dockerPath, "volume", "create", name)
	}
}
