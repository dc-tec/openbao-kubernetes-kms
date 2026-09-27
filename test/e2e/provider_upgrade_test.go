//go:build e2e

package e2e

import (
	"context"
	"strings"
	"testing"
	"time"
)

const (
	envProviderOldImage = "E2E_PROVIDER_OLD_IMAGE"
	envProviderNewImage = "E2E_PROVIDER_NEW_IMAGE"

	oldBinarySamplePath = "/kms-sample/old-binary-sample.json"
	newBinarySamplePath = "/kms-sample/new-binary-sample.json"
)

func TestProviderBinaryUpgradeRollbackE2E(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	requireOpenBaoCI(t)
	oldImage := requireProviderImageFromEnv(t, envProviderOldImage)
	newImage := requireProviderImageFromEnv(t, envProviderNewImage)
	if oldImage == newImage {
		t.Fatalf("%s and %s must reference distinct image tags", envProviderOldImage, envProviderNewImage)
	}
	dockerPath := requireDocker(t, ctx)
	requireProviderImageVersionsDiffer(t, ctx, dockerPath, oldImage, newImage)

	stack := startProviderFailureStack(t, ctx, "obk-e2e-provider-upgrade", providerFailureStackOptions{
		ProviderImage: oldImage,
	})
	oldSampleEnv := []string{kmsSamplePathEnv + "=" + oldBinarySamplePath}
	stack.runClientWithEnv(ctx, "old-write-client", kmsClientModeWriteSample, sampleReadWrite, oldSampleEnv)
	runDocker(t, ctx, dockerPath, "stop", stack.providerName)
	before := providerPersistenceHashes(t, ctx, stack)

	stack.restartProvider(ctx, newImage)
	exitCtx, exitCancel := context.WithTimeout(ctx, 30*time.Second)
	defer exitCancel()
	exitCode, err := runDockerOutput(exitCtx, dockerPath, "wait", stack.providerName)
	if err != nil || strings.TrimSpace(exitCode) == "0" {
		t.Fatalf("candidate did not reject legacy state: exit=%q err=%v", exitCode, err)
	}
	if !strings.Contains(dockerLogs(ctx, dockerPath, stack.providerName), legacyStateRejection) {
		t.Fatal("candidate failed without the expected legacy-state rejection")
	}
	doctorOutput := stack.runProviderCLIExpectFailure(ctx, "legacy-doctor", "doctor", "--config", containerConfigPath)
	assertOutputContains(t, doctorOutput, "[fail] registry.state", legacyStateRejection)
	if after := providerPersistenceHashes(t, ctx, stack); after != before {
		t.Fatal("rejected upgrade changed legacy registry or checkpoint")
	}
	stack.restartProvider(ctx, oldImage)
	stack.runClientWithEnv(ctx, "rollback-read-old-client", kmsClientModeReadSample, sampleReadOnly, oldSampleEnv)
}

func requireProviderImageVersionsDiffer(
	t *testing.T,
	ctx context.Context,
	dockerPath string,
	oldImage string,
	newImage string,
) {
	t.Helper()

	oldVersion := providerImageVersion(t, ctx, dockerPath, oldImage)
	newVersion := providerImageVersion(t, ctx, dockerPath, newImage)
	t.Logf("provider upgrade: %s -> %s", oldVersion, newVersion)
	if oldVersion == newVersion {
		t.Fatalf("provider image version outputs are identical; expected distinct binaries")
	}
}

func providerImageVersion(t *testing.T, ctx context.Context, dockerPath string, image string) string {
	t.Helper()

	output, err := runDockerOutput(ctx, dockerPath, "run", "--rm", image, "version")
	if err != nil {
		t.Fatalf("run provider image %s version: %v: %s", image, err, strings.TrimSpace(output))
	}
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		t.Fatalf("provider image %s returned empty version output", image)
	}
	return trimmed
}

const legacyStateRejection = "preview.3 requires fresh bound state"

func providerPersistenceHashes(t *testing.T, ctx context.Context, stack *providerFailureStack) string {
	t.Helper()
	output, err := runDockerOutput(ctx, stack.dockerPath, "run", "--rm", "--user", "0:0",
		"--entrypoint", "sha256sum", "--volume", stack.volumes.state+":/state:ro", stack.openBaoImage,
		"/state/key-registry.json", "/state/key-registry.json.checkpoint")
	if err != nil || strings.TrimSpace(output) == "" {
		t.Fatalf("hash provider persistence: %v", err)
	}
	return output
}
