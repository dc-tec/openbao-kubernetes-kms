//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
	"github.com/dc-tec/openbao-kubernetes-kms/test/e2e/framework"
)

func TestKindDRRestoreRunbookE2E(t *testing.T) {
	if !kindCIEnabled() {
		t.Skip(envKindCI + "=true is required")
	}
	providerImage := os.Getenv(envProviderImage)
	if providerImage == "" {
		t.Skip(envProviderImage + " is required")
	}
	nodeImage := os.Getenv(envKindNodeImage)
	if nodeImage == "" {
		t.Skip(envKindNodeImage + " is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Minute)
	defer cancel()

	dockerPath := requireToolOrSkip(t, ctx, framework.EnvDefault(framework.EnvDockerBinary, "docker"))
	kindPath := requireToolOrSkip(t, ctx, framework.EnvDefault(envKindBinary, "kind"))
	kubectlPath := requireToolOrSkip(t, ctx, framework.EnvDefault(envKubectlBinary, "kubectl"))

	prefix := fmt.Sprintf("obk-kind-dr-%d", time.Now().UnixNano())
	clusterName := prefix
	nodeName := clusterName + kindControlPlaneNodeSuffix
	contextName := "kind-" + clusterName
	primaryVolume := prefix + "-bao-primary"
	restoreVolume := prefix + "-bao-restore"
	createDockerVolumes(t, ctx, dockerPath, primaryVolume, restoreVolume)
	var environment *framework.OpenBaoEnvironment
	t.Cleanup(func() {
		if environment != nil {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cleanupCancel()
			if err := environment.Close(cleanupCtx); err != nil {
				t.Errorf("close OpenBao environment: %v", err)
			}
		}
		removeVolume(t, context.Background(), dockerPath, primaryVolume)
		removeVolume(t, context.Background(), dockerPath, restoreVolume)
		if !strings.EqualFold(os.Getenv(framework.EnvSkipCleanup), "true") {
			deleteKindCluster(t, context.Background(), kindPath, clusterName)
		}
	})

	createKindCluster(t, ctx, kindPath, clusterName, nodeImage)
	environment = startKindOpenBaoWithConfig(t, ctx, framework.OpenBaoEnvironmentConfig{
		StorageVolume: primaryVolume,
	})
	loadProviderImageIntoKind(t, ctx, kindPath, clusterName, providerImage)
	stageKindProvider(t, ctx, dockerPath, nodeName, providerImage, environment)
	waitForKindProviderSocket(t, ctx, dockerPath, nodeName)
	enableKindAPIServerKMS(t, ctx, dockerPath, kubectlPath, contextName, nodeName)

	// #nosec G101 -- Kubernetes object name, not a credential or secret value.
	secretName := "obk-kind-dr"
	secretValue := "kind-dr-secret-" + strconvTime(time.Now())
	createKindSecretNamed(t, ctx, kubectlPath, contextName, secretName, secretValue)
	assertKindSecretReadableNamed(t, ctx, kubectlPath, contextName, secretName, secretValue)
	assertKindEtcdEncryptedNamed(t, ctx, dockerPath, nodeName, secretName, secretValue)
	if err := environment.RotateTransitKey(ctx); err != nil {
		t.Fatalf("rotate Transit key before backup: %v", err)
	}
	waitKindActiveVersion(t, ctx, dockerPath, nodeName, 2)
	restartKindAPIServer(t, ctx, dockerPath, kubectlPath, contextName, nodeName)
	// #nosec G101 -- Kubernetes object name, not a credential or secret value.
	rotatedSecretName := "obk-kind-dr-rotated"
	rotatedSecretValue := "kind-dr-rotated-" + strconvTime(time.Now())
	createKindSecretNamed(t, ctx, kubectlPath, contextName, rotatedSecretName, rotatedSecretValue)
	assertKindEtcdEncryptedNamed(t, ctx, dockerPath, nodeName, rotatedSecretName, rotatedSecretValue)

	backupDir := t.TempDir()
	waitForKindProviderStateFile(t, ctx, dockerPath, nodeName)
	backupKindProviderRunbookFiles(t, ctx, dockerPath, nodeName, backupDir)
	snapshotPath := filepath.Join(t.TempDir(), "openbao-kind-dr.snap")
	if err := environment.SaveRaftSnapshot(ctx, snapshotPath); err != nil {
		t.Fatalf("save OpenBao raft snapshot: %v", err)
	}

	removeKindProviderForDR(t, ctx, dockerPath, nodeName)
	if err := environment.RestoreRaftSnapshot(ctx, restoreVolume, snapshotPath); err != nil {
		t.Fatalf("restore OpenBao raft snapshot for Kind DR: %v", err)
	}
	rehydrateKindProviderRunbookFiles(t, ctx, dockerPath, nodeName, backupDir)
	waitForKindProviderSocket(t, ctx, dockerPath, nodeName)
	restartKindAPIServer(t, ctx, dockerPath, kubectlPath, contextName, nodeName)
	assertKindSecretReadableNamed(t, ctx, kubectlPath, contextName, secretName, secretValue)
	assertKindSecretReadableNamed(t, ctx, kubectlPath, contextName, rotatedSecretName, rotatedSecretValue)
	waitKindActiveVersion(t, ctx, dockerPath, nodeName, 2)
	assertKindEtcdEncryptedNamed(t, ctx, dockerPath, nodeName, secretName, secretValue)
	assertKindEtcdEncryptedNamed(t, ctx, dockerPath, nodeName, rotatedSecretName, rotatedSecretValue)

	// #nosec G101 -- Kubernetes object name, not a credential or secret value.
	restoredSecretName := "obk-kind-dr-restored"
	restoredSecretValue := "kind-dr-restored-secret-" + strconvTime(time.Now())
	createKindSecretNamed(t, ctx, kubectlPath, contextName, restoredSecretName, restoredSecretValue)
	assertKindSecretReadableNamed(t, ctx, kubectlPath, contextName, restoredSecretName, restoredSecretValue)
	assertKindEtcdEncryptedNamed(t, ctx, dockerPath, nodeName, restoredSecretName, restoredSecretValue)
}

func waitKindActiveVersion(t *testing.T, ctx context.Context, dockerPath, nodeName string, version int) {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) && ctx.Err() == nil {
		output, err := runDockerOutput(ctx, dockerPath, "exec", nodeName, "cat", kindProviderStatePath)
		var state keyregistry.StateFile
		if err == nil && json.Unmarshal([]byte(output), &state) == nil && state.Validate() == nil {
			active, activeErr := state.ActiveSnapshot()
			if activeErr == nil && active.TransitVersion == version {
				return
			}
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("provider on %s did not promote Transit version %d", nodeName, version)
}

func waitForKindProviderStateFile(t *testing.T, ctx context.Context, dockerPath string, nodeName string) {
	t.Helper()

	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		_, err := runDockerOutput(ctx, dockerPath, "exec", nodeName, "test", "-s", kindProviderStatePath)
		if err == nil {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("provider state file did not become available at %s", kindProviderStatePath)
}

func backupKindProviderRunbookFiles(
	t *testing.T,
	ctx context.Context,
	dockerPath string,
	nodeName string,
	backupDir string,
) {
	t.Helper()

	// Stop the only state writer before copying the registry and its checkpoint.
	dockerCopy(t, ctx, dockerPath, nodeName+":"+kindProviderStaticPodPath,
		filepath.Join(backupDir, "bao-kms-provider.yaml"))
	runDocker(t, ctx, dockerPath, "exec", nodeName, "rm", "-f", kindProviderStaticPodPath)
	waitForKindProviderContainerGone(t, ctx, dockerPath, nodeName)
	for _, file := range kindProviderRunbookFiles() {
		if file.nodePath == kindProviderStaticPodPath {
			continue
		}
		dockerCopy(t, ctx, dockerPath, nodeName+":"+file.nodePath, filepath.Join(backupDir, file.backupName))
	}
	statePath := filepath.Join(backupDir, "key-registry.json")
	state, _, err := keyregistry.LoadStateFile(statePath, keyregistry.StateLoadOptions{})
	if err != nil {
		t.Fatalf("validate backed-up registry: %v", err)
	}
	checkpoint, err := keyregistry.LoadStateCheckpoint(filepath.Join(backupDir, "key-registry.json.checkpoint"))
	if err != nil {
		t.Fatalf("validate backed-up checkpoint: %v", err)
	}
	if err := checkpoint.ValidateState(state); err != nil {
		t.Fatalf("backup does not contain a consistent registry/checkpoint pair: %v", err)
	}
}

func removeKindProviderForDR(t *testing.T, ctx context.Context, dockerPath string, nodeName string) {
	t.Helper()

	runDocker(t, ctx, dockerPath, "exec", nodeName, "rm", "-f", kindProviderStaticPodPath)
	waitForKindProviderContainerGone(t, ctx, dockerPath, nodeName)
	runDocker(t, ctx, dockerPath, "exec", nodeName, "rm", "-rf",
		"/etc/openbao-kms",
		"/var/lib/openbao-kms",
		"/run/openbao-kms",
	)
}

func waitForKindProviderContainerGone(t *testing.T, ctx context.Context, dockerPath string, nodeName string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		output, err := runDockerOutput(
			ctx,
			dockerPath,
			"exec",
			nodeName,
			"crictl",
			"ps",
			"--name",
			"^bao-kms-provider$",
			"-q",
		)
		if err == nil && strings.TrimSpace(output) == "" {
			return
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("provider container did not stop during DR replacement\nprovider status:\n%s",
		kindContainerStatus(ctx, dockerPath, nodeName, "^bao-kms-provider$"),
	)
}

func rehydrateKindProviderRunbookFiles(
	t *testing.T,
	ctx context.Context,
	dockerPath string,
	nodeName string,
	backupDir string,
) {
	t.Helper()

	runDocker(t, ctx, dockerPath, "exec", nodeName, "mkdir", "-p",
		"/etc/openbao-kms/tls",
		"/var/lib/openbao-kms/state",
		filepath.Dir(kindProviderJWTPath),
		"/run/openbao-kms",
	)
	for _, file := range kindProviderRunbookFiles() {
		if file.nodePath == kindProviderStaticPodPath {
			continue
		}
		dockerCopy(t, ctx, dockerPath, filepath.Join(backupDir, file.backupName), nodeName+":"+file.nodePath)
	}
	runDocker(t, ctx, dockerPath, "exec", nodeName, "sh", "-c", kindProviderPermissionsScript)
	dockerCopy(
		t,
		ctx,
		dockerPath,
		filepath.Join(backupDir, "bao-kms-provider.yaml"),
		nodeName+":"+kindProviderStaticPodPath,
	)
}

type kindProviderRunbookFile struct {
	nodePath   string
	backupName string
}

func kindProviderRunbookFiles() []kindProviderRunbookFile {
	return []kindProviderRunbookFile{
		{nodePath: kindProviderConfigPath, backupName: "provider.yaml"},
		{nodePath: kindProviderCAPath, backupName: "ca.crt"},
		{nodePath: kindProviderJWTPath, backupName: "identity.jwt"},
		{nodePath: kindProviderStatePath, backupName: "key-registry.json"},
		{nodePath: kindProviderStatePath + ".checkpoint", backupName: "key-registry.json.checkpoint"},
		{nodePath: kindProviderStaticPodPath, backupName: "bao-kms-provider.yaml"},
	}
}
