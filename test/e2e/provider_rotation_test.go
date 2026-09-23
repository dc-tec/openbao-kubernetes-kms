//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
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

	// An observed minimum must not authorize local retirement. The operator must
	// stop the writer and apply the exact reviewed state transition.
	plan := retirementPlan(t, ctx, stack)
	applyArgs := []string{
		"retire-versions", "--config", containerConfigPath, "--before-version", "2",
		"--apply", "--expected-state-hash", plan.StateHash, "--output", "json",
	}
	if output, err := runProviderMaintenance(ctx, stack, applyArgs...); err == nil || !strings.Contains(output, "locked") {
		t.Fatalf("retirement failed to exclude running provider: %v: %s", err, output)
	}
	if output, err := runProviderMaintenance(ctx, stack, "serve", "--config", containerConfigPath); err == nil ||
		!strings.Contains(output, "locked") {
		t.Fatalf("second serve failed to stop before bootstrap: %v: %s", err, output)
	}
	runDocker(t, ctx, dockerPath, "stop", stack.providerName)
	output, err := runProviderMaintenance(ctx, stack, applyArgs...)
	if err != nil {
		t.Fatalf("apply retirement: %v: %s", err, output)
	}
	var applied retirementE2EReport
	if err := json.Unmarshal([]byte(output), &applied); err != nil {
		t.Fatalf("decode applied retirement: %v: %s", err, output)
	}
	if !applied.Applied || applied.NextStateHash != plan.NextStateHash {
		t.Fatalf("applied retirement differs from reviewed plan: %+v", applied)
	}
	stack.restartProvider(ctx, stack.providerImage)
	stack.runClientWithEnv(ctx, "retirement-client", "expect-retirement", sampleReadOnly, rotationEnv)
}

type retirementE2EReport struct {
	Applied         bool   `json:"applied"`
	StateHash       string `json:"stateHash"`
	NextStateHash   string `json:"nextStateHash"`
	RemovedVersions []struct {
		TransitVersion int `json:"transitVersion"`
	} `json:"removedVersions"`
}

func retirementPlan(t *testing.T, ctx context.Context, stack *providerFailureStack) retirementE2EReport {
	t.Helper()
	output, err := runProviderMaintenance(ctx, stack, "retire-versions", "--config", containerConfigPath,
		"--before-version", "2", "--output", "json")
	if err != nil {
		t.Fatalf("plan retirement: %v: %s", err, output)
	}
	var plan retirementE2EReport
	if err := json.Unmarshal([]byte(output), &plan); err != nil {
		t.Fatalf("decode retirement plan: %v: %s", err, output)
	}
	if plan.Applied || plan.StateHash == "" || plan.NextStateHash == "" || len(plan.RemovedVersions) != 1 ||
		plan.RemovedVersions[0].TransitVersion != 1 {
		t.Fatalf("unexpected retirement plan: %+v", plan)
	}
	return plan
}

func runProviderMaintenance(ctx context.Context, stack *providerFailureStack, command ...string) (string, error) {
	args := make([]string, 0, 14+len(command))
	args = append(args,
		"run", "--rm", "--network", stack.networkName, "--read-only",
		"--volume", stack.volumes.config+":/config:ro",
		"--volume", stack.volumes.tls+":/bao/tls:ro",
		"--volume", stack.volumes.run+":/run/openbao-kms",
		"--volume", stack.volumes.state+":/var/lib/openbao-kms/state",
		stack.providerImage,
	)
	return runDockerOutput(ctx, stack.dockerPath, append(args, command...)...)
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
