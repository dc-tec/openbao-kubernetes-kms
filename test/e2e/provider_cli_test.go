//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/test/e2e/framework"
)

const unsupportedTransitKeyType = "chacha20-poly1305"

func TestProviderCLIHappyPathE2E(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), providerFailureDefaultTimeout)
	defer cancel()

	stack := startProviderFailureStack(t, ctx, "obk-e2e-cli", providerFailureStackOptions{})
	stack.runClient(ctx, "write-client", kmsClientModeWriteSample, sampleReadWrite)

	configOutput := stack.runProviderCLI(ctx, "cli-config", "config", "--config", containerConfigPath)
	assertOutputContains(t, configOutput, "identityFingerprint:", "transitAssociatedData: true")

	policyOutput := stack.runProviderCLI(ctx, "cli-policy", "policy", "openbao", "--config", containerConfigPath)
	assertOutputContains(
		t,
		policyOutput,
		fmt.Sprintf(`path "%s/keys/%s"`, stack.environment.TransitMount, stack.environment.TransitKey),
		fmt.Sprintf(`path "%s/encrypt/%s"`, stack.environment.TransitMount, stack.environment.TransitKey),
		fmt.Sprintf(`path "%s/decrypt/%s"`, stack.environment.TransitMount, stack.environment.TransitKey),
		`path "sys/capabilities-self"`,
	)

	doctorOutput := stack.runProviderCLI(ctx, "cli-doctor", "doctor", "--config", containerConfigPath)
	assertOutputContains(
		t,
		doctorOutput,
		"doctor",
		"[pass] openbao.auth",
		"[pass] transit.profile",
		"[pass] transit.probe",
		"[pass] kms.status_encrypt",
	)

	verifyKeyOutput := stack.runProviderCLI(ctx, "cli-verify-key", "verify-key", "--config", containerConfigPath)
	assertOutputContains(
		t,
		verifyKeyOutput,
		"verify-key",
		"[pass] registry.state",
		"[pass] transit.version_restrictions",
	)

	rotationPlanOutput := stack.runProviderCLI(ctx, "cli-rotation-plan", "rotation-plan", "--config", containerConfigPath)
	assertOutputContains(
		t,
		rotationPlanOutput,
		"rotation-plan",
		"stateLoaded: true",
		"rotationState: active",
		"activeKeyIdHash:",
	)

	verifyRotationOutput := stack.runProviderCLI(ctx, "cli-verify-rotation", "verify-rotation", "--config", containerConfigPath)
	assertOutputContains(
		t,
		verifyRotationOutput,
		"verify-rotation",
		"stateLoaded: true",
		"confidence: limited",
	)
}

func TestProviderCLIJWTClaimDriftRedactedE2E(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), providerFailureDefaultTimeout)
	defer cancel()

	stack := startProviderFailureStack(t, ctx, "obk-e2e-cli-jwt-drift", providerFailureStackOptions{
		Config: providerContainerConfigOptions{
			ExpectedSubject: "system:serviceaccount:unexpected:provider",
		},
	})

	output := stack.runProviderCLIExpectFailure(ctx, "cli-doctor-jwt-drift", "doctor", "--config", containerConfigPath)
	assertOutputContains(t, output, "[fail] jwt.local", "[skip] openbao.auth")
	assertOutputNotContains(
		t,
		output,
		stack.environment.JWTIssuer(),
		stack.environment.JWTAudience(),
		stack.environment.JWTSubject(),
	)
}

func TestProviderCLIUnsupportedTransitKeyTypeFailsE2E(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), providerFailureDefaultTimeout)
	defer cancel()

	stack := startProviderFailureStack(t, ctx, "obk-e2e-cli-key-type", providerFailureStackOptions{
		Environment: framework.OpenBaoEnvironmentConfig{
			TransitKeyType: unsupportedTransitKeyType,
		},
	})

	output := stack.runProviderCLIExpectFailure(ctx, "cli-verify-key-type", "verify-key", "--config", containerConfigPath)
	assertOutputContains(t, output, "[fail] transit.profile", "key type is not aes256-gcm96")
}

func TestProviderCLIRotationMissingStateFailsClosedE2E(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	dockerPath := requireDocker(t, ctx)
	prefix := fmt.Sprintf("obk-e2e-cli-missing-state-%d", time.Now().UnixNano())
	primaryVolume := prefix + "-bao-primary"
	createDockerVolumes(t, ctx, dockerPath, primaryVolume)
	t.Cleanup(func() {
		removeVolume(t, context.Background(), dockerPath, primaryVolume)
	})

	stack := startProviderFailureStack(t, ctx, "obk-e2e-cli-missing-state", providerFailureStackOptions{
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

	removeContainer(t, ctx, stack.dockerPath, stack.providerName)
	stack.clearProviderState(ctx)

	rotationPlanOutput := stack.runProviderCLIExpectFailure(ctx, "cli-rotation-plan-missing-state", "rotation-plan", "--config", containerConfigPath)
	assertOutputContains(t, rotationPlanOutput, "local registry state is absent for non-initial Transit metadata")
	assertOutputNotContains(t, rotationPlanOutput, "activeKeyIdHash:")

	verifyRotationOutput := stack.runProviderCLIExpectFailure(ctx, "cli-verify-rotation-missing-state", "verify-rotation", "--config", containerConfigPath)
	assertOutputContains(t, verifyRotationOutput, "local registry state is absent for non-initial Transit metadata")
	assertOutputNotContains(t, verifyRotationOutput, "activeKeyIdHash:")
}

func (s *providerFailureStack) runProviderCLI(ctx context.Context, nameSuffix string, args ...string) string {
	s.t.Helper()

	output, err := s.runProviderCLICommand(ctx, nameSuffix, args...)
	if err != nil {
		logs := dockerLogs(context.Background(), s.dockerPath, s.providerName)
		s.t.Fatalf("run provider CLI %s: %v: %s\nprovider logs:\n%s", nameSuffix, err, output, logs)
	}
	return output
}

func (s *providerFailureStack) runProviderCLIExpectFailure(ctx context.Context, nameSuffix string, args ...string) string {
	s.t.Helper()

	output, err := s.runProviderCLICommand(ctx, nameSuffix, args...)
	if err == nil {
		s.t.Fatalf("provider CLI %s succeeded unexpectedly:\n%s", nameSuffix, output)
	}
	return output
}

func (s *providerFailureStack) runProviderCLICommand(ctx context.Context, nameSuffix string, args ...string) (string, error) {
	s.t.Helper()

	containerName := s.providerName + "-" + nameSuffix
	dockerArgs := []string{
		"run", "--rm",
		"--name", containerName,
		"--network", s.networkName,
		"--volume", s.volumes.config + ":/config:ro",
		"--volume", s.volumes.tls + ":/bao/tls:ro",
		"--volume", s.volumes.run + ":/run/openbao-kms",
		"--volume", s.volumes.state + ":/var/lib/openbao-kms/state:ro",
		s.providerImage,
	}
	dockerArgs = append(dockerArgs, args...)
	output, err := runDockerOutput(ctx, s.dockerPath, dockerArgs...)
	return strings.TrimSpace(output), err
}

func assertOutputContains(t *testing.T, output string, values ...string) {
	t.Helper()

	for _, value := range values {
		if !strings.Contains(output, value) {
			t.Fatalf("output missing %q:\n%s", value, output)
		}
	}
}

func assertOutputNotContains(t *testing.T, output string, values ...string) {
	t.Helper()

	for _, value := range values {
		if value != "" && strings.Contains(output, value) {
			t.Fatalf("output contains %q:\n%s", value, output)
		}
	}
}
