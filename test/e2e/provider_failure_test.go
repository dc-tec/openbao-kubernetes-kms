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

const (
	kmsClientModeEnv         = "KMS_CLIENT_MODE"
	kmsSamplePathEnv         = "KMS_SAMPLE_PATH"
	kmsRotationSamplePathEnv = "KMS_ROTATION_SAMPLE_PATH"

	kmsClientModeFullStack               = "full-stack"
	kmsClientModeCreateStaleSocket       = "create-stale-socket"
	kmsClientModeWriteSample             = "write-sample"
	kmsClientModeReadSample              = "read-sample"
	kmsClientModeExpectOutage            = "expect-outage"
	kmsClientModeExpectUnhealthy         = "expect-unhealthy"
	kmsClientModeExpectPolicyDenied      = "expect-policy-denied"
	kmsClientModeExpectSocketUnavailable = "expect-socket-unavailable"
	kmsClientModeExpectStatusStaleness   = "expect-status-staleness"
	kmsClientModeExpectJWTRefresh        = "expect-jwt-refresh"
	kmsClientModeExpectRotationPromotion = "expect-rotation-promotion"
	kmsClientModeExpectRotationRollback  = "expect-rotation-rollback"
	kmsClientModeDecryptStorm            = "decrypt-storm"
	kmsClientModeDecryptSoak             = "decrypt-soak"
	kmsClientModeLoadSoak                = "load-soak"
	kmsClientSampleMount                 = "/kms-sample"
	missingTransitKeyName                = "missing-kms-e2e-key"
	providerFailureDefaultTimeout        = 5 * time.Minute
	providerJWTRotationInitialJWTTTL     = 8 * time.Second
	providerJWTRotationReplacementJWTTTL = 30 * time.Minute
)

func TestProviderOpenBaoOutageFailsClosedE2E(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), providerFailureDefaultTimeout)
	defer cancel()

	stack := startProviderFailureStack(t, ctx, "obk-e2e-outage", providerFailureStackOptions{})
	stack.runClient(ctx, "write-client", kmsClientModeWriteSample, sampleReadWrite)

	if err := stack.environment.StopContainer(ctx); err != nil {
		t.Fatalf("stop OpenBao container: %v", err)
	}

	stack.runClient(ctx, "outage-client", kmsClientModeExpectOutage, sampleReadOnly)
}

func TestProviderOpenBaoSealFailsClosedE2E(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), providerFailureDefaultTimeout)
	defer cancel()

	stack := startProviderFailureStack(t, ctx, "obk-e2e-sealed", providerFailureStackOptions{})
	stack.runClient(ctx, "write-client", kmsClientModeWriteSample, sampleReadWrite)

	if err := stack.environment.Seal(ctx); err != nil {
		t.Fatalf("seal OpenBao: %v", err)
	}

	stack.runClient(ctx, "sealed-client", kmsClientModeExpectOutage, sampleReadOnly)
}

func TestProviderBadPolicyFailsClosedE2E(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), providerFailureDefaultTimeout)
	defer cancel()

	stack := startProviderFailureStack(t, ctx, "obk-e2e-policy", providerFailureStackOptions{
		Config: providerContainerConfigOptions{
			DeepProbeInterval: "10m",
		},
	})
	stack.runClient(ctx, "write-client", kmsClientModeWriteSample, sampleReadWrite)

	if err := stack.environment.InstallProviderPolicy(ctx, stack.environment.MetadataOnlyProviderPolicy()); err != nil {
		t.Fatalf("install reduced OpenBao provider policy: %v", err)
	}

	stack.runClient(ctx, "policy-client", kmsClientModeExpectPolicyDenied, sampleReadOnly)
}

func TestProviderExpiredJWTFailsClosedE2E(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), providerFailureDefaultTimeout)
	defer cancel()

	stack := startProviderFailureStack(t, ctx, "obk-e2e-expired-jwt", providerFailureStackOptions{
		BeforePopulate: func(t *testing.T, environment *framework.OpenBaoEnvironment, _ string) {
			t.Helper()
			if err := environment.WriteJWTFile(time.Now().UTC().Add(-10*time.Minute), time.Minute); err != nil {
				t.Fatalf("write expired JWT: %v", err)
			}
		},
	})

	stack.runClient(ctx, "expired-jwt-client", kmsClientModeExpectSocketUnavailable, sampleNotMounted)
}

func TestProviderJWTExpectedClaimDriftFailsClosedE2E(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), providerFailureDefaultTimeout)
	defer cancel()

	stack := startProviderFailureStack(t, ctx, "obk-e2e-jwt-claim-drift", providerFailureStackOptions{
		Config: providerContainerConfigOptions{
			ExpectedSubject: "system:serviceaccount:unexpected:provider",
		},
	})

	stack.runClient(ctx, "jwt-claim-drift-client", kmsClientModeExpectSocketUnavailable, sampleNotMounted)
	stack.assertProviderLogsDoNotContain(
		ctx,
		stack.environment.JWTIssuer(),
		stack.environment.JWTAudience(),
		stack.environment.JWTSubject(),
	)
}

func TestProviderJWTFileRotationE2E(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), providerFailureDefaultTimeout)
	defer cancel()

	stack := startProviderFailureStack(t, ctx, "obk-e2e-jwt-rotation", providerFailureStackOptions{
		Environment: framework.OpenBaoEnvironmentConfig{
			JWTTokenTTL: "4s",
			JWTMaxTTL:   "20s",
		},
		Config: providerContainerConfigOptions{
			MinJWTRemainingTTL:     "1s",
			LoginBeforeTokenExpiry: "2s",
		},
		BeforeProviderStart: func(t *testing.T, ctx context.Context, stack *providerFailureStack) {
			t.Helper()
			stack.replaceJWT(ctx, time.Now().UTC(), providerJWTRotationInitialJWTTTL)
		},
	})

	stack.runClient(ctx, "initial-client", kmsClientModeWriteSample, sampleReadWrite)
	stack.replaceJWT(ctx, time.Now().UTC(), providerJWTRotationReplacementJWTTTL)
	stack.runClient(ctx, "refresh-client", kmsClientModeExpectJWTRefresh, sampleNotMounted)
}

func TestProviderJWTSigningKeyRolloverE2E(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), providerFailureDefaultTimeout)
	defer cancel()

	stack := startProviderFailureStack(t, ctx, "obk-e2e-jwt-key-rollover", providerFailureStackOptions{
		Environment: framework.OpenBaoEnvironmentConfig{
			JWTTokenTTL: "4s",
			JWTMaxTTL:   "20s",
		},
		Config: providerContainerConfigOptions{
			MinJWTRemainingTTL:     "1s",
			LoginBeforeTokenExpiry: "2s",
		},
		BeforeProviderStart: func(t *testing.T, ctx context.Context, stack *providerFailureStack) {
			t.Helper()
			stack.replaceJWT(ctx, time.Now().UTC(), providerJWTRotationInitialJWTTTL)
		},
	})

	stack.runClient(ctx, "initial-client", kmsClientModeWriteSample, sampleReadWrite)
	if err := stack.environment.RotateJWTSigningKey(ctx, true); err != nil {
		t.Fatalf("rotate JWT signing key: %v", err)
	}
	stack.replaceJWT(ctx, time.Now().UTC(), providerJWTRotationReplacementJWTTTL)
	stack.runClient(ctx, "rollover-client", kmsClientModeExpectJWTRefresh, sampleNotMounted)
}

func TestProviderTransitKeyMissingFailsClosedE2E(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), providerFailureDefaultTimeout)
	defer cancel()

	stack := startProviderFailureStack(t, ctx, "obk-e2e-missing-key", providerFailureStackOptions{
		Config: providerContainerConfigOptions{
			TransitKeyName: missingTransitKeyName,
		},
	})

	stack.runClient(ctx, "missing-key-client", kmsClientModeExpectSocketUnavailable, sampleNotMounted)
}

func TestProviderStatusStalenessFailsClosedE2E(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), providerFailureDefaultTimeout)
	defer cancel()

	stack := startProviderFailureStack(t, ctx, "obk-e2e-stale-status", providerFailureStackOptions{
		Config: providerContainerConfigOptions{
			ProbeInterval:      "10s",
			DeepProbeInterval:  "30s",
			StatusMaxStaleness: "2s",
		},
	})

	stack.runClient(ctx, "staleness-client", kmsClientModeExpectStatusStaleness, sampleNotMounted)
}

func TestProviderStaleSocketReclaimedE2E(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), providerFailureDefaultTimeout)
	defer cancel()

	stack := startProviderFailureStack(t, ctx, "obk-e2e-stale-socket", providerFailureStackOptions{
		BeforeProviderStart: func(t *testing.T, ctx context.Context, stack *providerFailureStack) {
			t.Helper()
			runKMSClientContainer(
				t,
				ctx,
				stack.dockerPath,
				stack.providerName,
				stack.providerName+"-stale-socket-client",
				stack.networkName,
				stack.providerImage,
				stack.clientPath,
				stack.volumes,
				[]string{kmsClientModeEnv + "=" + kmsClientModeCreateStaleSocket},
				nil,
			)
		},
	})

	stack.runClient(ctx, "reclaimed-client", kmsClientModeFullStack, sampleNotMounted)
}

func TestProviderDecryptStormSmokeE2E(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), providerFailureDefaultTimeout)
	defer cancel()

	stack := startProviderFailureStack(t, ctx, "obk-e2e-decrypt-storm", providerFailureStackOptions{})
	stack.runClient(ctx, "storm-client", kmsClientModeDecryptStorm, sampleNotMounted)
}

type providerFailureStackOptions struct {
	ProviderImage       string
	Config              providerContainerConfigOptions
	Environment         framework.OpenBaoEnvironmentConfig
	MountSoftHSM        bool
	ProviderStart       providerContainerStartOptions
	BeforePopulate      func(t *testing.T, environment *framework.OpenBaoEnvironment, stagingDir string)
	BeforeProviderStart func(t *testing.T, ctx context.Context, stack *providerFailureStack)
}

type providerFailureStack struct {
	t             *testing.T
	dockerPath    string
	openBaoImage  string
	providerImage string
	networkName   string
	providerName  string
	clientPath    string
	sampleVolume  string
	volumes       providerVolumes
	environment   *framework.OpenBaoEnvironment
	providerStart providerContainerStartOptions
}

type sampleMountMode string

const (
	sampleNotMounted sampleMountMode = ""
	sampleReadOnly   sampleMountMode = "ro"
	sampleReadWrite  sampleMountMode = "rw"
)

func startProviderFailureStack(
	t *testing.T,
	ctx context.Context,
	prefixBase string,
	opts providerFailureStackOptions,
) *providerFailureStack {
	t.Helper()

	requireOpenBaoCI(t)
	providerImage := opts.ProviderImage
	if providerImage == "" {
		providerImage = requireProviderImageFromEnv(t, envProviderImage)
	}
	dockerPath := requireDocker(t, ctx)
	prefix := fmt.Sprintf("%s-%d", prefixBase, time.Now().UnixNano())
	networkName := prefix + "-net"
	providerName := prefix + "-provider"
	sampleVolume := prefix + "-sample"
	volumes := providerVolumes{
		config: prefix + "-config",
		tls:    prefix + "-tls",
		run:    prefix + "-run",
		state:  prefix + "-state",
	}
	providerStart := opts.ProviderStart
	if opts.MountSoftHSM {
		volumes.hsm = prefix + "-hsm"
		providerStart.Env = append(providerStart.Env, "SOFTHSM2_CONF="+containerSoftHSMConfigPath)
		providerStart.Volumes = append(providerStart.Volumes, volumes.hsm+":/hsm")
	}
	stack := &providerFailureStack{
		t:             t,
		dockerPath:    dockerPath,
		openBaoImage:  framework.EnvDefault(framework.EnvOpenBaoImage, framework.DefaultOpenBaoImage),
		providerImage: providerImage,
		networkName:   networkName,
		providerName:  providerName,
		sampleVolume:  sampleVolume,
		volumes:       volumes,
		providerStart: providerStart,
	}
	var providerStarted bool
	t.Cleanup(func() {
		if providerStarted {
			removeContainer(t, context.Background(), dockerPath, providerName)
		}
		if stack.environment != nil {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cleanupCancel()
			if err := stack.environment.Close(cleanupCtx); err != nil {
				t.Errorf("close OpenBao environment: %v", err)
			}
		}
		for _, volume := range append(volumes.names(), sampleVolume) {
			removeVolume(t, context.Background(), dockerPath, volume)
		}
		removeNetwork(t, context.Background(), dockerPath, networkName)
	})

	runDocker(t, ctx, dockerPath, "network", "create", networkName)
	for _, volume := range append(volumes.names(), sampleVolume) {
		runDocker(t, ctx, dockerPath, "volume", "create", volume)
	}

	envConfig := opts.Environment
	envConfig.NetworkName = networkName
	environment, err := framework.StartOpenBaoEnvironment(ctx, envConfig)
	if errors.Is(err, framework.ErrDockerUnavailable) {
		t.Skip(err.Error())
	}
	if err != nil {
		t.Fatalf("start OpenBao environment: %v", err)
	}
	stack.environment = environment

	stagingDir := t.TempDir()
	if opts.BeforePopulate != nil {
		opts.BeforePopulate(t, environment, stagingDir)
	}
	writeProviderContainerConfigWithOptions(t, filepath.Join(stagingDir, "provider.yaml"), environment, opts.Config)
	copyFile(t, environment.CACertFile, filepath.Join(stagingDir, "openbao-ca.crt"), 0o644)
	copyFile(t, environment.JWTFile, filepath.Join(stagingDir, "identity.jwt"), 0o600)
	populateProviderVolumes(t, ctx, dockerPath, stagingDir, stack.openBaoImage, volumes)
	prepareWritableKMSClientVolume(t, ctx, dockerPath, stack.openBaoImage, sampleVolume)

	clientPath := filepath.Join(stagingDir, "kms-client")
	buildKMSClient(t, ctx, clientPath)
	stack.clientPath = clientPath
	if opts.BeforeProviderStart != nil {
		opts.BeforeProviderStart(t, ctx, stack)
	}

	startProviderContainerWithOptions(t, ctx, dockerPath, providerName, networkName, providerImage, volumes, stack.providerStart)
	providerStarted = true
	return stack
}

func requireProviderFailureImage(t *testing.T) string {
	t.Helper()

	requireOpenBaoCI(t)
	return requireProviderImageFromEnv(t, envProviderImage)
}

func requireOpenBaoCI(t *testing.T) {
	t.Helper()

	if !framework.OpenBaoCIEnabled() {
		t.Skip("E2E_OPENBAO_CI=true is required")
	}
}

func requireProviderImageFromEnv(t *testing.T, envName string) string {
	t.Helper()

	providerImage := os.Getenv(envName)
	if providerImage == "" {
		t.Skip(envName + " is required")
	}
	return providerImage
}

func requireDocker(t *testing.T, ctx context.Context) string {
	t.Helper()

	dockerPath, err := exec.LookPath(framework.EnvDefault(framework.EnvDockerBinary, "docker"))
	if err != nil {
		t.Skipf("%s: %v", framework.ErrDockerUnavailable, err)
	}
	if output, err := exec.CommandContext(ctx, dockerPath, "version", "--format", "{{.Server.Version}}").CombinedOutput(); err != nil {
		t.Skipf("%s: %s", framework.ErrDockerUnavailable, strings.TrimSpace(string(output)))
	}
	return dockerPath
}

func (s *providerFailureStack) replaceJWT(ctx context.Context, now time.Time, ttl time.Duration) {
	s.t.Helper()

	tmpDir := s.t.TempDir()
	jwtPath := filepath.Join(tmpDir, "identity.jwt")
	if err := s.environment.WriteJWTFileAt(jwtPath, now, ttl); err != nil {
		s.t.Fatalf("write replacement JWT: %v", err)
	}

	script := `set -eu
cp /src/identity.jwt /bao/tls/identity.jwt
chown 65532:65532 /bao/tls/identity.jwt
chmod 0600 /bao/tls/identity.jwt
`
	runDocker(s.t, ctx, s.dockerPath,
		"run", "--rm",
		"--user", "0:0",
		"--entrypoint", "/bin/sh",
		"--volume", tmpDir+":/src:ro",
		"--volume", s.volumes.tls+":/bao/tls",
		s.openBaoImage,
		"-c", script,
	)
}

func (s *providerFailureStack) runClient(ctx context.Context, nameSuffix string, mode string, sampleMode sampleMountMode) {
	s.t.Helper()
	s.runClientWithEnv(ctx, nameSuffix, mode, sampleMode, nil)
}

func (s *providerFailureStack) restartProvider(ctx context.Context, image string) {
	s.t.Helper()
	if image == "" {
		s.t.Fatal("provider restart image is empty")
	}
	removeContainer(s.t, ctx, s.dockerPath, s.providerName)
	startProviderContainerWithOptions(s.t, ctx, s.dockerPath, s.providerName, s.networkName, image, s.volumes, s.providerStart)
	s.providerImage = image
}

func (s *providerFailureStack) restartProviderWithEmptyState(ctx context.Context, image string) {
	s.t.Helper()
	if image == "" {
		s.t.Fatal("provider restart image is empty")
	}
	removeContainer(s.t, ctx, s.dockerPath, s.providerName)
	s.clearProviderState(ctx)
	startProviderContainerWithOptions(s.t, ctx, s.dockerPath, s.providerName, s.networkName, image, s.volumes, s.providerStart)
	s.providerImage = image
}

func (s *providerFailureStack) clearProviderState(ctx context.Context) {
	s.t.Helper()

	script := `set -eu
rm -f /var/lib/openbao-kms/state/*
chown -R 65532:65532 /var/lib/openbao-kms/state
chmod 0700 /var/lib/openbao-kms/state
`
	runDocker(s.t, ctx, s.dockerPath,
		"run", "--rm",
		"--user", "0:0",
		"--entrypoint", "/bin/sh",
		"--volume", s.volumes.state+":/var/lib/openbao-kms/state",
		s.openBaoImage,
		"-c", script,
	)
}

func (s *providerFailureStack) removeProviderStateFile(ctx context.Context) {
	s.t.Helper()

	script := `set -eu
rm -f /var/lib/openbao-kms/state/key-registry.json
chown -R 65532:65532 /var/lib/openbao-kms/state
chmod 0700 /var/lib/openbao-kms/state
`
	runDocker(s.t, ctx, s.dockerPath,
		"run", "--rm",
		"--user", "0:0",
		"--entrypoint", "/bin/sh",
		"--volume", s.volumes.state+":/var/lib/openbao-kms/state",
		s.openBaoImage,
		"-c", script,
	)
}

func (s *providerFailureStack) assertProviderLogsDoNotContain(ctx context.Context, values ...string) {
	s.t.Helper()

	logs := dockerLogs(ctx, s.dockerPath, s.providerName)
	for _, value := range values {
		if value != "" && strings.Contains(logs, value) {
			s.t.Fatalf("provider logs contain sensitive JWT claim value %q:\n%s", value, logs)
		}
	}
}

func (s *providerFailureStack) runClientWithEnv(
	ctx context.Context,
	nameSuffix string,
	mode string,
	sampleMode sampleMountMode,
	env []string,
) {
	s.t.Helper()

	clientName := s.providerName + "-" + nameSuffix
	extraVolumes := make([]string, 0, 1)
	if sampleMode != sampleNotMounted {
		mount := s.sampleVolume + ":" + kmsClientSampleMount
		if sampleMode == sampleReadOnly {
			mount += ":ro"
		}
		extraVolumes = append(extraVolumes, mount)
	}
	clientEnv := append([]string{kmsClientModeEnv + "=" + mode}, env...)
	runKMSClientContainer(
		s.t,
		ctx,
		s.dockerPath,
		s.providerName,
		clientName,
		s.networkName,
		s.providerImage,
		s.clientPath,
		s.volumes,
		clientEnv,
		extraVolumes,
	)
	removeContainer(s.t, context.Background(), s.dockerPath, clientName)
}

func prepareWritableKMSClientVolume(
	t *testing.T,
	ctx context.Context,
	dockerPath string,
	helperImage string,
	volumeName string,
) {
	t.Helper()

	script := `set -eu
chown -R 65532:65532 /kms-sample
chmod 0700 /kms-sample
`
	runDocker(t, ctx, dockerPath,
		"run", "--rm",
		"--user", "0:0",
		"--entrypoint", "/bin/sh",
		"--volume", volumeName+":/kms-sample",
		helperImage,
		"-c", script,
	)
}

func runKMSClientContainer(
	t *testing.T,
	ctx context.Context,
	dockerPath string,
	providerName string,
	clientName string,
	networkName string,
	providerImage string,
	clientPath string,
	volumes providerVolumes,
	env []string,
	extraVolumes []string,
) {
	t.Helper()

	args := []string{
		"run", "--rm",
		"--name", clientName,
		"--network", networkName,
		"--env", "KMS_SOCKET_PATH=" + containerSocketPath,
		"--volume", volumes.run + ":/run/openbao-kms",
		"--volume", clientPath + ":/kms-client:ro",
	}
	for _, value := range env {
		args = append(args, "--env", value)
	}
	for _, value := range extraVolumes {
		args = append(args, "--volume", value)
	}
	args = append(args,
		"--entrypoint", "/kms-client",
		providerImage,
	)

	output, err := runDockerOutput(ctx, dockerPath, args...)
	if err != nil {
		logs := dockerLogs(context.Background(), dockerPath, providerName)
		t.Fatalf("run KMS client container %s: %v: %s\nprovider logs:\n%s", clientName, err, strings.TrimSpace(output), logs)
	}
	if trimmed := strings.TrimSpace(output); trimmed != "" {
		t.Logf("KMS client container %s output: %s", clientName, trimmed)
	}
}
