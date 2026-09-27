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
)

func TestProviderCLIManagementCapabilitiesFailE2E(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), providerFailureDefaultTimeout)
	defer cancel()
	stack := startProviderFailureStack(t, ctx, "obk-e2e-cli-capabilities", providerFailureStackOptions{})
	stack.runClient(ctx, "write-client", kmsClientModeWriteSample, sampleReadWrite)
	policy := stack.runProviderCLI(ctx, "policy", "policy", "openbao", "--config", containerConfigPath)
	key := stack.environment.TransitKey
	for i, endpoint := range []string{
		"keys/" + key + "/config", "keys/" + key + "/trim", "config/keys",
		"restore/" + key, "restore", "rewrap/" + key,
	} {
		grant := fmt.Sprintf("\npath %q { capabilities = [\"read\", \"update\"] }\n",
			stack.environment.TransitMount+"/"+endpoint)
		if err := stack.environment.InstallProviderPolicy(ctx, policy+grant); err != nil {
			t.Fatal(err)
		}
		output := stack.runProviderCLIExpectCheckFailure(ctx, fmt.Sprintf("capability-%d", i),
			"doctor", "--config", containerConfigPath)
		assertOutputContains(t, output, "[fail] transit.capabilities", "token can perform non-hot-path key management")
	}
	if err := stack.environment.InstallProviderPolicy(ctx, policy); err != nil {
		t.Fatal(err)
	}
	output := stack.runProviderCLI(ctx, "recovered", "doctor", "--config", containerConfigPath)
	assertOutputContains(t, output, "[pass] transit.capabilities")
	stack.runClient(ctx, "read-client", kmsClientModeReadSample, sampleReadOnly)
}

func TestProviderCLIMigrationEncryptionConfigE2E(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), providerFailureDefaultTimeout)
	defer cancel()
	stack := startProviderFailureStack(t, ctx, "obk-e2e-cli-migration", providerFailureStackOptions{})
	// Public synthetic key; no production encryption material is used.
	secret := strings.Repeat("A", 43) + "="
	const targetName = "openbao-kms-workload-a"
	configText := fmt.Sprintf(`apiVersion: apiserver.config.k8s.io/v1
kind: EncryptionConfiguration
resources:
- resources: [secrets]
  providers:
  - aescbc:
      keys:
      - name: old-local-key
        secret: %s
  - kms:
      apiVersion: v2
      name: old-provider
      endpoint: unix:///run/old-provider.sock
  - kms:
      apiVersion: v2
      name: %s
      endpoint: unix://%s
`, secret, targetName, containerSocketPath)
	stagingDir := t.TempDir()
	for name, content := range map[string]string{
		"migration": configText,
		"missing":   strings.ReplaceAll(configText, targetName, "unrelated-provider"),
		"malformed": strings.ReplaceAll(configText, "secret: "+secret, "secret: ["+secret+"]"),
	} {
		if err := os.WriteFile(filepath.Join(stagingDir, name+".yaml"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	runDocker(t, ctx, stack.dockerPath, "run", "--rm", "--user", "0:0", "--entrypoint", "/bin/sh",
		"--volume", stagingDir+":/src:ro", "--volume", stack.volumes.config+":/config", stack.openBaoImage,
		"-c", "cp /src/*.yaml /config/ && chown 65532:65532 /config/*.yaml && chmod 0600 /config/*.yaml")
	output := stack.runProviderCLI(ctx, "migration", "doctor", "--config", containerConfigPath,
		"--encryption-config", "/config/migration.yaml", "--output", "json")
	assertCLIJSONReport(t, output, "doctor", "kubernetes.encryption_config")
	assertOutputNotContains(t, output, secret)
	for _, name := range []string{"missing", "malformed"} {
		output = stack.runProviderCLIExpectCheckFailure(ctx, name, "doctor", "--config", containerConfigPath,
			"--encryption-config", "/config/"+name+".yaml")
		assertOutputContains(t, output, "[fail] kubernetes.encryption_config")
		assertOutputNotContains(t, output, secret)
	}
}

func TestProviderCLIRotationRemoteFailureE2E(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), providerFailureDefaultTimeout)
	defer cancel()
	stack := startProviderFailureStack(t, ctx, "obk-e2e-cli-remote-failure", providerFailureStackOptions{})
	stack.runClient(ctx, "write-client", kmsClientModeWriteSample, sampleReadWrite)
	policy := stack.runProviderCLI(ctx, "policy", "policy", "openbao", "--config", containerConfigPath)
	denyMetadata := fmt.Sprintf("\npath %q { capabilities = [\"deny\"] }\n",
		stack.environment.TransitMount+"/keys/"+stack.environment.TransitKey)
	if err := stack.environment.InstallProviderPolicy(ctx, policy+denyMetadata); err != nil {
		t.Fatal(err)
	}
	assertRotationRemoteFailure(t, ctx, stack, "denied")
	if err := stack.environment.InstallProviderPolicy(ctx, policy); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"rotation-plan", "verify-rotation"} {
		output := stack.runProviderCLI(ctx, command+"-recovered", command,
			"--config", containerConfigPath, "--output", "json")
		assertRotationJSONReport(t, output, command, "active")
	}
	stack.runClient(ctx, "read-client", kmsClientModeReadSample, sampleReadOnly)
	if err := stack.environment.StopContainer(ctx); err != nil {
		t.Fatal(err)
	}
	assertRotationRemoteFailure(t, ctx, stack, "offline")
}

func assertRotationRemoteFailure(t *testing.T, ctx context.Context, stack *providerFailureStack, stage string) {
	t.Helper()
	for _, command := range []string{"rotation-plan", "verify-rotation"} {
		for _, format := range []string{"text", "json"} {
			output := stack.runProviderCLIExpectCheckFailure(ctx, stage+"-"+command+"-"+format, command,
				"--config", containerConfigPath, "--output", format)
			if format == "json" {
				// Docker combines stdout (the report) and stderr (the command error).
				assertOutputContains(t, output, `"transitMetadataStatus": "fail"`, `"stateLoaded": true`,
					`"stateCheckpointStatus": "current"`, `"activeTransitVersion": 1`)
			} else {
				assertOutputContains(t, output, "transitMetadataStatus: fail", "stateLoaded: true",
					"stateCheckpointStatus: current", "activeTransitVersion: 1")
			}
		}
	}
}

func (s *providerFailureStack) runProviderCLIExpectCheckFailure(
	ctx context.Context, suffix string, args ...string,
) string {
	s.t.Helper()
	output, err := s.runProviderCLICommand(ctx, suffix, args...)
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 4 {
		s.t.Fatalf("expected CLI diagnostic exit 4: %v\n%s", err, output)
	}
	return output
}
