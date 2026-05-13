//go:build e2e

package e2e

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/test/e2e/framework"
)

func TestProviderOpenBaoHAFailoverE2E(t *testing.T) {
	if !framework.OpenBaoCIEnabled() {
		t.Skip("E2E_OPENBAO_CI=true is required")
	}
	providerImage := os.Getenv(envProviderImage)
	if providerImage == "" {
		t.Skip(envProviderImage + " is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 9*time.Minute)
	defer cancel()

	dockerPath, err := exec.LookPath(framework.EnvDefault(framework.EnvDockerBinary, "docker"))
	if err != nil {
		t.Skipf("%s: %v", framework.ErrDockerUnavailable, err)
	}
	if output, err := exec.CommandContext(ctx, dockerPath, "version", "--format", "{{.Server.Version}}").CombinedOutput(); err != nil {
		t.Skipf("%s: %s", framework.ErrDockerUnavailable, strings.TrimSpace(string(output)))
	}

	prefix := fmt.Sprintf("obk-e2e-ha-%d", time.Now().UnixNano())
	networkName := prefix + "-net"
	providerName := prefix + "-provider"
	clientName := prefix + "-client"
	volumes := providerVolumes{
		config: prefix + "-config",
		tls:    prefix + "-tls",
		run:    prefix + "-run",
		state:  prefix + "-state",
	}
	var environment *framework.OpenBaoHAEnvironment
	var providerStarted bool
	t.Cleanup(func() {
		if providerStarted {
			removeContainer(t, context.Background(), dockerPath, providerName)
		}
		removeContainer(t, context.Background(), dockerPath, clientName)
		if environment != nil {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cleanupCancel()
			if err := environment.Close(cleanupCtx); err != nil {
				t.Errorf("close OpenBao HA environment: %v", err)
			}
		}
		for _, volume := range volumes.names() {
			removeVolume(t, context.Background(), dockerPath, volume)
		}
		removeNetwork(t, context.Background(), dockerPath, networkName)
	})

	runDocker(t, ctx, dockerPath, "network", "create", networkName)
	for _, volume := range volumes.names() {
		runDocker(t, ctx, dockerPath, "volume", "create", volume)
	}

	environment, err = framework.StartOpenBaoHAEnvironment(ctx, framework.OpenBaoHAEnvironmentConfig{
		NetworkName: networkName,
	})
	if errors.Is(err, framework.ErrDockerUnavailable) {
		t.Skip(err.Error())
	}
	if err != nil {
		t.Fatalf("start OpenBao HA environment: %v", err)
	}

	stagingDir := t.TempDir()
	writeProviderContainerConfigWithOptions(
		t,
		filepath.Join(stagingDir, "provider.yaml"),
		&environment.OpenBaoEnvironment,
		providerContainerConfigOptions{
			OpenBaoAddress: environment.ProviderAddress(),
			OpenBaoTimeout: "2s",
		},
	)
	copyFile(t, environment.CACertFile, filepath.Join(stagingDir, "openbao-ca.crt"), 0o644)
	copyFile(t, environment.JWTFile, filepath.Join(stagingDir, "identity.jwt"), 0o600)
	populateProviderVolumes(t, ctx, dockerPath, stagingDir, framework.EnvDefault(framework.EnvOpenBaoImage, framework.DefaultOpenBaoImage), volumes)

	clientPath := filepath.Join(stagingDir, "kms-client")
	buildKMSClient(t, ctx, clientPath)
	sampleDir := filepath.Join(stagingDir, "sample")
	if err := os.Mkdir(sampleDir, 0o700); err != nil {
		t.Fatalf("create sample directory: %v", err)
	}
	if err := os.Chmod(sampleDir, 0o777); err != nil {
		t.Fatalf("make sample directory container-writable: %v", err)
	}

	startProviderContainer(t, ctx, dockerPath, providerName, networkName, providerImage, volumes)
	providerStarted = true

	runHAKMSClient(t, ctx, dockerPath, clientName, networkName, providerImage, volumes, clientPath, sampleDir, kmsClientModeWriteSample)
	if err := environment.StopActiveNode(ctx); err != nil {
		t.Fatalf("stop OpenBao active node and wait for failover: %v", err)
	}
	runHAKMSClient(t, ctx, dockerPath, clientName, networkName, providerImage, volumes, clientPath, sampleDir, kmsClientModeReadSample)
	runHAKMSClient(t, ctx, dockerPath, clientName, networkName, providerImage, volumes, clientPath, sampleDir, kmsClientModeFullStack)
}

func runHAKMSClient(
	t *testing.T,
	ctx context.Context,
	dockerPath string,
	name string,
	networkName string,
	providerImage string,
	volumes providerVolumes,
	clientPath string,
	sampleDir string,
	mode string,
) {
	t.Helper()

	removeContainer(t, context.Background(), dockerPath, name)
	output, err := runDockerOutput(ctx, dockerPath,
		"run", "--rm",
		"--name", name,
		"--network", networkName,
		"--env", "KMS_SOCKET_PATH="+containerSocketPath,
		"--env", kmsClientModeEnv+"="+mode,
		"--volume", volumes.run+":/run/openbao-kms",
		"--volume", clientPath+":/kms-client:ro",
		"--volume", sampleDir+":/kms-sample",
		"--entrypoint", "/kms-client",
		providerImage,
	)
	if err != nil {
		t.Fatalf("run HA KMS client mode %s: %v: %s", mode, err, strings.TrimSpace(output))
	}
}
