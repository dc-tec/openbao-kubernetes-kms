//go:build e2e

package e2e

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/test/e2e/framework"
)

const (
	envProviderImage = "E2E_PROVIDER_IMAGE"

	containerConfigPath = "/config/provider.yaml"
	containerCAPath     = "/bao/tls/openbao-ca.crt"
	containerJWTPath    = "/bao/tls/identity.jwt"
	containerSocketPath = "/run/openbao-kms/kms.sock"
	containerStatePath  = "/var/lib/openbao-kms/state/key-registry.json"
)

func TestProviderContainerFullStackE2E(t *testing.T) {
	if !framework.OpenBaoCIEnabled() {
		t.Skip("E2E_OPENBAO_CI=true is required")
	}
	providerImage := os.Getenv(envProviderImage)
	if providerImage == "" {
		t.Skip(envProviderImage + " is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	dockerPath, err := exec.LookPath(framework.EnvDefault(framework.EnvDockerBinary, "docker"))
	if err != nil {
		t.Skipf("%s: %v", framework.ErrDockerUnavailable, err)
	}
	if output, err := exec.CommandContext(ctx, dockerPath, "version", "--format", "{{.Server.Version}}").CombinedOutput(); err != nil {
		t.Skipf("%s: %s", framework.ErrDockerUnavailable, strings.TrimSpace(string(output)))
	}

	prefix := fmt.Sprintf("obk-e2e-%d", time.Now().UnixNano())
	networkName := prefix + "-net"
	providerName := prefix + "-provider"
	clientName := prefix + "-client"
	volumes := providerVolumes{
		config: prefix + "-config",
		tls:    prefix + "-tls",
		run:    prefix + "-run",
		state:  prefix + "-state",
	}
	var environment *framework.OpenBaoEnvironment
	var providerStarted bool
	t.Cleanup(func() {
		if providerStarted {
			removeContainer(t, context.Background(), dockerPath, providerName)
		}
		removeContainer(t, context.Background(), dockerPath, clientName)
		if environment != nil {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cleanupCancel()
			if err := environment.Close(cleanupCtx); err != nil {
				t.Errorf("close OpenBao environment: %v", err)
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

	environment, err = framework.StartOpenBaoEnvironment(ctx, framework.OpenBaoEnvironmentConfig{
		NetworkName: networkName,
	})
	if errors.Is(err, framework.ErrDockerUnavailable) {
		t.Skip(err.Error())
	}
	if err != nil {
		t.Fatalf("start OpenBao environment: %v", err)
	}

	stagingDir := t.TempDir()
	writeProviderContainerConfig(t, filepath.Join(stagingDir, "provider.yaml"), environment)
	copyFile(t, environment.CACertFile, filepath.Join(stagingDir, "openbao-ca.crt"), 0o644)
	copyFile(t, environment.JWTFile, filepath.Join(stagingDir, "identity.jwt"), 0o600)
	populateProviderVolumes(t, ctx, dockerPath, stagingDir, framework.EnvDefault(framework.EnvOpenBaoImage, framework.DefaultOpenBaoImage), volumes)

	clientPath := filepath.Join(stagingDir, "kms-client")
	buildKMSClient(t, ctx, clientPath)

	startProviderContainer(t, ctx, dockerPath, providerName, networkName, providerImage, volumes)
	providerStarted = true

	output, err := runDockerOutput(ctx, dockerPath,
		"run", "--rm",
		"--name", clientName,
		"--network", networkName,
		"--env", "KMS_SOCKET_PATH="+containerSocketPath,
		"--volume", volumes.run+":/run/openbao-kms",
		"--volume", clientPath+":/kms-client:ro",
		"--entrypoint", "/kms-client",
		providerImage,
	)
	if err != nil {
		logs := dockerLogs(context.Background(), dockerPath, providerName)
		t.Fatalf("run KMS client container: %v: %s\nprovider logs:\n%s", err, strings.TrimSpace(output), logs)
	}
}

type providerVolumes struct {
	config string
	tls    string
	run    string
	state  string
}

func (v providerVolumes) names() []string {
	return []string{v.config, v.tls, v.run, v.state}
}

func writeProviderContainerConfig(t *testing.T, path string, environment *framework.OpenBaoEnvironment) {
	t.Helper()
	writeProviderContainerConfigWithOptions(t, path, environment, providerContainerConfigOptions{})
}

type providerContainerConfigOptions struct {
	OpenBaoTimeout         string
	TransitKeyName         string
	ProbeInterval          string
	DeepProbeInterval      string
	StatusMaxStaleness     string
	MinJWTRemainingTTL     string
	LoginBeforeTokenExpiry string
}

func writeProviderContainerConfigWithOptions(
	t *testing.T,
	path string,
	environment *framework.OpenBaoEnvironment,
	opts providerContainerConfigOptions,
) {
	t.Helper()

	if opts.OpenBaoTimeout == "" {
		opts.OpenBaoTimeout = "5s"
	}
	if opts.TransitKeyName == "" {
		opts.TransitKeyName = environment.TransitKey
	}
	if opts.ProbeInterval == "" {
		opts.ProbeInterval = "1s"
	}
	if opts.DeepProbeInterval == "" {
		opts.DeepProbeInterval = "30s"
	}
	if opts.StatusMaxStaleness == "" {
		opts.StatusMaxStaleness = "1m"
	}
	if opts.MinJWTRemainingTTL == "" {
		opts.MinJWTRemainingTTL = "2m"
	}
	if opts.LoginBeforeTokenExpiry == "" {
		opts.LoginBeforeTokenExpiry = "30s"
	}

	raw := fmt.Sprintf(`configVersion: v1alpha1
server:
  socketPath: %q
  socketMode: "0600"
  socketGroup: "65532"
  metricsAddress: ""
  healthAddress: ""
openbao:
  address: %q
  caCertFile: %q
  tlsServerName: %q
  timeout: %s
  instanceId: openbao-ci-a
auth:
  method: jwt
  mountPath: %q
  role: %q
  jwtFile: %q
  minJwtRemainingTtl: %s
  clockSkewLeeway: 30s
  loginBeforeTokenExpiry: %s
  tokenStorage: memory
transit:
  mountPath: %q
  keyName: %q
  keyIdScope:
    providerName: openbao-kms-workload-a
    clusterId: workload-a
    transitMountId: transit-ci-primary
    keyLineageId: 01HXEXAMPLEKEYLINEAGEID
  useAssociatedData: true
status:
  probeInterval: %s
  deepProbeInterval: %s
  statusMaxStaleness: %s
state:
  path: %q
rotation:
  mode: observed
  activationDelay: 1s
  requireStableObservationCount: 1
  rejectVersionRollback: true
performance:
  decryptMicroBatching:
    enabled: false
    maxBatchSize: 32
    maxWait: 2ms
logging:
  level: info
  format: json
  redactOpenBaoPaths: true
  logOpenBaoRequestIDs: true
  debugCorrelation:
    enabled: false
    ttl: 15m
`, containerSocketPath,
		environment.ContainerAddress(),
		containerCAPath,
		environment.TLSServerName,
		opts.OpenBaoTimeout,
		environment.AuthMount,
		environment.AuthRole,
		containerJWTPath,
		opts.MinJWTRemainingTTL,
		opts.LoginBeforeTokenExpiry,
		environment.TransitMount,
		opts.TransitKeyName,
		opts.ProbeInterval,
		opts.DeepProbeInterval,
		opts.StatusMaxStaleness,
		containerStatePath,
	)
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write provider container config: %v", err)
	}
}

func populateProviderVolumes(
	t *testing.T,
	ctx context.Context,
	dockerPath string,
	stagingDir string,
	helperImage string,
	volumes providerVolumes,
) {
	t.Helper()

	script := `set -eu
cp /src/provider.yaml /config/provider.yaml
cp /src/openbao-ca.crt /bao/tls/openbao-ca.crt
cp /src/identity.jwt /bao/tls/identity.jwt
chown -R 65532:65532 /config /bao/tls /run/openbao-kms /var/lib/openbao-kms/state
chmod 0700 /config /bao/tls /run/openbao-kms /var/lib/openbao-kms/state
chmod 0600 /config/provider.yaml /bao/tls/identity.jwt
chmod 0644 /bao/tls/openbao-ca.crt
`
	runDocker(t, ctx, dockerPath,
		"run", "--rm",
		"--entrypoint", "/bin/sh",
		"--volume", stagingDir+":/src:ro",
		"--volume", volumes.config+":/config",
		"--volume", volumes.tls+":/bao/tls",
		"--volume", volumes.run+":/run/openbao-kms",
		"--volume", volumes.state+":/var/lib/openbao-kms/state",
		helperImage,
		"-c", script,
	)
}

func buildKMSClient(t *testing.T, ctx context.Context, outputPath string) {
	t.Helper()

	repoRoot := findRepoRoot(t)
	goPath, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("find go binary: %v", err)
	}
	cmd := exec.CommandContext(ctx, goPath, "build", "-trimpath", "-o", outputPath, "./test/e2e/kmsclient")
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"CGO_ENABLED=0",
		"GOOS=linux",
		"GOARCH="+goruntime.GOARCH,
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build KMS client: %v: %s", err, strings.TrimSpace(string(output)))
	}
	if err := os.Chmod(outputPath, 0o755); err != nil {
		t.Fatalf("chmod KMS client: %v", err)
	}
}

func startProviderContainer(
	t *testing.T,
	ctx context.Context,
	dockerPath string,
	name string,
	networkName string,
	image string,
	volumes providerVolumes,
) {
	t.Helper()

	runDocker(t, ctx, dockerPath,
		"run", "--detach",
		"--name", name,
		"--network", networkName,
		"--read-only",
		"--volume", volumes.config+":/config:ro",
		"--volume", volumes.tls+":/bao/tls:ro",
		"--volume", volumes.run+":/run/openbao-kms",
		"--volume", volumes.state+":/var/lib/openbao-kms/state",
		image,
		"serve", "--config", containerConfigPath,
	)
}

func copyFile(t *testing.T, source string, target string, mode os.FileMode) {
	t.Helper()

	input, err := os.Open(source)
	if err != nil {
		t.Fatalf("open %s: %v", source, err)
	}
	defer func() {
		if err := input.Close(); err != nil {
			t.Fatalf("close %s: %v", source, err)
		}
	}()

	output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		t.Fatalf("create %s: %v", target, err)
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		t.Fatalf("copy %s to %s: %v", source, target, err)
	}
	if err := output.Close(); err != nil {
		t.Fatalf("close %s: %v", target, err)
	}
	if err := os.Chmod(target, mode); err != nil {
		t.Fatalf("chmod %s: %v", target, err)
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repository root not found")
		}
		dir = parent
	}
}

func runDocker(t *testing.T, ctx context.Context, dockerPath string, args ...string) {
	t.Helper()

	output, err := runDockerOutput(ctx, dockerPath, args...)
	if err != nil {
		t.Fatalf("docker %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(output))
	}
}

func runDockerOutput(ctx context.Context, dockerPath string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, dockerPath, args...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func dockerLogs(ctx context.Context, dockerPath string, name string) string {
	output, err := runDockerOutput(ctx, dockerPath, "logs", name)
	if err != nil {
		return strings.TrimSpace(output)
	}
	return strings.TrimSpace(output)
}

func removeContainer(t *testing.T, ctx context.Context, dockerPath string, name string) {
	t.Helper()
	output, err := runDockerOutput(ctx, dockerPath, "rm", "-f", name)
	if err != nil && !strings.Contains(output, "No such container") {
		t.Errorf("remove container %s: %v: %s", name, err, strings.TrimSpace(output))
	}
}

func removeVolume(t *testing.T, ctx context.Context, dockerPath string, name string) {
	t.Helper()
	output, err := runDockerOutput(ctx, dockerPath, "volume", "rm", "-f", name)
	if err != nil && !strings.Contains(output, "No such volume") {
		t.Errorf("remove volume %s: %v: %s", name, err, strings.TrimSpace(output))
	}
}

func removeNetwork(t *testing.T, ctx context.Context, dockerPath string, name string) {
	t.Helper()
	output, err := runDockerOutput(ctx, dockerPath, "network", "rm", name)
	if err != nil && !strings.Contains(output, "No such network") {
		t.Errorf("remove network %s: %v: %s", name, err, strings.TrimSpace(output))
	}
}
