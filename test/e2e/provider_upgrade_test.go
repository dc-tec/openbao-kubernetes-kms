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
	newSampleEnv := []string{kmsSamplePathEnv + "=" + newBinarySamplePath}

	stack.runClientWithEnv(ctx, "old-write-client", kmsClientModeWriteSample, sampleReadWrite, oldSampleEnv)

	stack.restartProvider(ctx, newImage)
	stack.runClientWithEnv(ctx, "new-read-old-client", kmsClientModeReadSample, sampleReadOnly, oldSampleEnv)
	stack.runClientWithEnv(ctx, "new-write-client", kmsClientModeWriteSample, sampleReadWrite, newSampleEnv)
	stack.runClientWithEnv(ctx, "new-read-new-client", kmsClientModeReadSample, sampleReadOnly, newSampleEnv)

	stack.restartProvider(ctx, oldImage)
	stack.runClientWithEnv(ctx, "rollback-read-old-client", kmsClientModeReadSample, sampleReadOnly, oldSampleEnv)
	stack.runClientWithEnv(ctx, "rollback-read-new-client", kmsClientModeReadSample, sampleReadOnly, newSampleEnv)
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
