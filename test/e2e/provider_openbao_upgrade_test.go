//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/test/e2e/framework"
)

func TestProviderOpenBaoServerUpgradeE2E(t *testing.T) {
	requireOpenBaoCI(t)
	policy := readVersionsPolicy(t).Validation.OpenBao
	from := ""
	for _, entry := range policy.PreviewMatrix {
		if entry.Version == policy.UpgradeFrom {
			from = entry.Image
		}
	}
	if from == "" || from == policy.Image {
		t.Fatal("OpenBao upgrade needs distinct pinned source and target images")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 9*time.Minute)
	defer cancel()
	dockerPath := requireDocker(t, ctx)
	volume := fmt.Sprintf("obk-upgrade-raft-%d", time.Now().UnixNano())
	createDockerVolumes(t, ctx, dockerPath, volume)
	t.Cleanup(func() { removeVolume(t, context.Background(), dockerPath, volume) })
	t.Logf("OpenBao server upgrade: %s -> %s", from, policy.Image)
	stack := startProviderFailureStack(t, ctx, "obk-e2e-server-upgrade", providerFailureStackOptions{
		Environment: framework.OpenBaoEnvironmentConfig{Image: from, StorageVolume: volume},
	})
	stack.runClient(ctx, "before-upgrade", kmsClientModeWriteSample, sampleReadWrite)
	if err := stack.environment.UpgradeImage(ctx, policy.Image); err != nil {
		t.Fatalf("upgrade OpenBao: %v", err)
	}
	stack.runClient(ctx, "after-upgrade-read", kmsClientModeReadSample, sampleReadOnly)
	stack.runClientWithEnv(ctx, "after-upgrade-write", kmsClientModeWriteSample, sampleReadWrite,
		[]string{kmsSamplePathEnv + "=" + newBinarySamplePath})
	stack.restartProvider(ctx, stack.providerImage)
	stack.runClient(ctx, "after-restart-old", kmsClientModeReadSample, sampleReadOnly)
	stack.runClientWithEnv(ctx, "after-restart-new", kmsClientModeReadSample, sampleReadOnly,
		[]string{kmsSamplePathEnv + "=" + newBinarySamplePath})
}
