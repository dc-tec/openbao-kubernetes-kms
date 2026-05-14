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

const (
	preRotationSamplePath  = "/kms-sample/pre-rotation-sample.json"
	postRotationSamplePath = "/kms-sample/post-rotation-sample.json"
)

func TestProviderTransitRotationE2E(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	dockerPath := requireDocker(t, ctx)
	prefix := fmt.Sprintf("obk-e2e-rotation-%d", time.Now().UnixNano())
	primaryVolume := prefix + "-bao-primary"
	rollbackVolume := prefix + "-bao-rollback"
	createDockerVolumes(t, ctx, dockerPath, primaryVolume, rollbackVolume)
	t.Cleanup(func() {
		removeVolume(t, context.Background(), dockerPath, primaryVolume)
		removeVolume(t, context.Background(), dockerPath, rollbackVolume)
	})

	stack := startProviderFailureStack(t, ctx, "obk-e2e-rotation", providerFailureStackOptions{
		Environment: framework.OpenBaoEnvironmentConfig{
			StorageVolume: primaryVolume,
		},
	})
	rotationEnv := []string{
		kmsSamplePathEnv + "=" + preRotationSamplePath,
		kmsRotationSamplePathEnv + "=" + postRotationSamplePath,
	}
	stack.runClientWithEnv(ctx, "pre-rotation-client", kmsClientModeWriteSample, sampleReadWrite, rotationEnv)

	snapshotPath := filepath.Join(t.TempDir(), "openbao-pre-rotation.snap")
	if err := stack.environment.SaveRaftSnapshot(ctx, snapshotPath); err != nil {
		t.Fatalf("save pre-rotation OpenBao raft snapshot: %v", err)
	}
	if err := stack.environment.RotateTransitKey(ctx); err != nil {
		t.Fatalf("rotate OpenBao Transit key: %v", err)
	}

	stack.runClientWithEnv(ctx, "rotation-client", kmsClientModeExpectRotationPromotion, sampleReadWrite, rotationEnv)

	if err := stack.environment.RestoreRaftSnapshot(ctx, rollbackVolume, snapshotPath); err != nil {
		t.Fatalf("restore pre-rotation OpenBao raft snapshot: %v", err)
	}
	requireOpenBaoTransitVersion(t, ctx, stack.environment, 1)
	stack.runClientWithEnv(ctx, "rollback-client", kmsClientModeExpectRotationRollback, sampleReadOnly, rotationEnv)
}

func TestProviderTransitMinDecryptionVersionBlocksHistoricalE2E(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	dockerPath := requireDocker(t, ctx)
	prefix := fmt.Sprintf("obk-e2e-min-decrypt-%d", time.Now().UnixNano())
	primaryVolume := prefix + "-bao-primary"
	createDockerVolumes(t, ctx, dockerPath, primaryVolume)
	t.Cleanup(func() {
		removeVolume(t, context.Background(), dockerPath, primaryVolume)
	})

	stack := startProviderFailureStack(t, ctx, "obk-e2e-min-decrypt", providerFailureStackOptions{
		Environment: framework.OpenBaoEnvironmentConfig{
			StorageVolume: primaryVolume,
		},
	})
	rotationEnv := []string{
		kmsSamplePathEnv + "=" + preRotationSamplePath,
		kmsRotationSamplePathEnv + "=" + postRotationSamplePath,
	}
	stack.runClientWithEnv(ctx, "pre-rotation-client", kmsClientModeWriteSample, sampleReadWrite, rotationEnv)

	if err := stack.environment.RotateTransitKey(ctx); err != nil {
		t.Fatalf("rotate OpenBao Transit key: %v", err)
	}
	stack.runClientWithEnv(ctx, "rotation-client", kmsClientModeExpectRotationPromotion, sampleReadWrite, rotationEnv)

	if err := stack.environment.SetTransitMinDecryptionVersion(ctx, 2); err != nil {
		t.Fatalf("set OpenBao Transit min_decryption_version: %v", err)
	}
	stack.runClientWithEnv(ctx, "min-decryption-client", kmsClientModeExpectUnhealthy, sampleNotMounted, nil)
}

func TestProviderMissingStateAfterRotationFailsClosedE2E(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	dockerPath := requireDocker(t, ctx)
	prefix := fmt.Sprintf("obk-e2e-missing-state-%d", time.Now().UnixNano())
	primaryVolume := prefix + "-bao-primary"
	createDockerVolumes(t, ctx, dockerPath, primaryVolume)
	t.Cleanup(func() {
		removeVolume(t, context.Background(), dockerPath, primaryVolume)
	})

	stack := startProviderFailureStack(t, ctx, "obk-e2e-missing-state", providerFailureStackOptions{
		Environment: framework.OpenBaoEnvironmentConfig{
			StorageVolume: primaryVolume,
		},
	})
	rotationEnv := []string{
		kmsSamplePathEnv + "=" + preRotationSamplePath,
		kmsRotationSamplePathEnv + "=" + postRotationSamplePath,
	}
	stack.runClientWithEnv(ctx, "pre-rotation-client", kmsClientModeWriteSample, sampleReadWrite, rotationEnv)

	if err := stack.environment.RotateTransitKey(ctx); err != nil {
		t.Fatalf("rotate OpenBao Transit key: %v", err)
	}
	stack.runClientWithEnv(ctx, "rotation-client", kmsClientModeExpectRotationPromotion, sampleReadWrite, rotationEnv)

	stack.restartProviderWithEmptyState(ctx, stack.providerImage)
	stack.runClient(ctx, "missing-state-client", kmsClientModeExpectSocketUnavailable, sampleNotMounted)
}

func requireOpenBaoTransitVersion(
	t *testing.T,
	ctx context.Context,
	environment *framework.OpenBaoEnvironment,
	want int,
) {
	t.Helper()

	client, err := environment.NewClient()
	if err != nil {
		t.Fatalf("create OpenBao client: %v", err)
	}
	profile, err := client.ReadKeyProfile(ctx, environment.TransitMount, environment.TransitKey)
	if err != nil {
		t.Fatalf("read OpenBao Transit key profile: %v", err)
	}
	if profile.LatestVersion != want {
		t.Fatalf("OpenBao Transit latest version = %d, want %d", profile.LatestVersion, want)
	}
}
