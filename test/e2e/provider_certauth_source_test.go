//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
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
	envSPIREServerImage           = "E2E_SPIRE_SERVER_IMAGE"
	envSPIREAgentImage            = "E2E_SPIRE_AGENT_IMAGE"
	envOpenBaoCertAuthURISANAlias = "E2E_OPENBAO_CERT_AUTH_URI_SAN_ALIAS"

	spireServerSocketPath                = "/run/spire/server/private/api.sock"
	spireAgentSocketPath                 = "/run/spire/sockets/agent.sock"
	testCertAuthSPIFFEID                 = "spiffe://example.org/openbao-kms/workload-a"
	openBaoCertAuthAliasNameSourceURISAN = "uri_san"
)

func TestProviderCertAuthPKCS11SoftHSME2E(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	stack := startProviderFailureStack(t, ctx, "obk-e2e-certauth-pkcs11", providerFailureStackOptions{
		Environment: framework.OpenBaoEnvironmentConfig{
			CertAuth: true,
		},
		Config: providerContainerConfigOptions{
			AuthMethod: providerAuthMethodCert,
			Cert: providerCertAuthConfigOptions{
				Source: providerCertAuthSourcePKCS11,
			},
		},
		MountSoftHSM: true,
		BeforeProviderStart: func(t *testing.T, ctx context.Context, stack *providerFailureStack) {
			t.Helper()
			configureSoftHSMProviderSource(t, ctx, stack)
		},
	})
	stack.runClient(ctx, "certauth-pkcs11-client", kmsClientModeFullStack, sampleNotMounted)
}

func TestProviderCertAuthSPIREWorkloadAPISourceE2E(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Minute)
	defer cancel()

	providerImage := requireProviderImageFromEnv(t, envProviderImage)
	requireProviderImageFromEnv(t, envSPIREServerImage)
	requireProviderImageFromEnv(t, envSPIREAgentImage)

	dockerPath := requireDocker(t, ctx)
	prefix := fmt.Sprintf("obk-e2e-certauth-spire-%d", time.Now().UnixNano())
	networkName := prefix + "-net"
	runDocker(t, ctx, dockerPath, "network", "create", networkName)
	t.Cleanup(func() {
		removeNetwork(t, context.Background(), dockerPath, networkName)
	})

	stack := &providerFailureStack{
		t:            t,
		dockerPath:   dockerPath,
		providerName: prefix + "-provider",
		networkName:  networkName,
		environment: &framework.OpenBaoEnvironment{
			CertSPIFFEID: testCertAuthSPIFFEID,
		},
	}
	source := startSPIREProviderSource(t, ctx, stack)
	probePath := filepath.Join(t.TempDir(), "certauth-spiffe-probe")
	buildSPIFFECertAuthProbe(t, ctx, probePath)
	runSPIFFEProviderSourceProbe(t, ctx, dockerPath, providerImage, networkName, source.SocketDir, probePath, testCertAuthSPIFFEID)
}

func TestProviderCertAuthSPIREOpenBaoE2E(t *testing.T) {
	if !strings.EqualFold(os.Getenv(envOpenBaoCertAuthURISANAlias), "true") {
		t.Skip(envOpenBaoCertAuthURISANAlias + "=true is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Minute)
	defer cancel()

	stack := startProviderFailureStack(t, ctx, "obk-e2e-certauth-spire-openbao", providerFailureStackOptions{
		Environment: framework.OpenBaoEnvironmentConfig{
			CertAuth:                true,
			CertAuthAliasNameSource: openBaoCertAuthAliasNameSourceURISAN,
		},
		Config: providerContainerConfigOptions{
			AuthMethod: providerAuthMethodCert,
			Cert: providerCertAuthConfigOptions{
				Source: providerCertAuthSourceSPIFFE,
			},
		},
		BeforeProviderStart: func(t *testing.T, ctx context.Context, stack *providerFailureStack) {
			t.Helper()
			source := startSPIREProviderSource(t, ctx, stack)
			configureOpenBaoCertAuthForProviderSource(t, ctx, stack, source.BundlePEM, "SPIRE bundle")
		},
	})
	stack.runClient(ctx, "certauth-spire-client", kmsClientModeFullStack, sampleNotMounted)
}

func configureSoftHSMProviderSource(t *testing.T, ctx context.Context, stack *providerFailureStack) {
	t.Helper()

	if stack.volumes.hsm == "" {
		t.Fatal("SoftHSM provider source requires an HSM volume")
	}
	outputDir := t.TempDir()
	runDocker(t, ctx, stack.dockerPath,
		"run", "--rm",
		"--user", "0:0",
		"--entrypoint", "/certauth-pkcs11-setup",
		"--volume", stack.volumes.hsm+":/hsm",
		"--volume", stack.volumes.tls+":/bao/tls",
		"--volume", outputDir+":/out",
		stack.providerImage,
		"--softhsm-config", containerSoftHSMConfigPath,
		"--token-directory", "/hsm/tokens",
		"--module-path", containerPKCS11ModulePath,
		"--token-label", providerCertAuthPKCS11TokenLabel,
		"--key-label", providerCertAuthPKCS11KeyLabel,
		"--pin-file", containerPKCS11PINPath,
		"--certificate-file", containerCertChainPath,
		"--ca-file", "/out/client-ca.pem",
		"--spiffe-id", stack.environment.CertSPIFFEID,
	)

	caPEM, err := os.ReadFile(filepath.Join(outputDir, "client-ca.pem"))
	if err != nil {
		t.Fatalf("read SoftHSM client CA: %v", err)
	}
	configureOpenBaoCertAuthForProviderSource(t, ctx, stack, caPEM, "SoftHSM CA")
}

func configureOpenBaoCertAuthForProviderSource(
	t *testing.T,
	ctx context.Context,
	stack *providerFailureStack,
	caPEM []byte,
	sourceDescription string,
) {
	t.Helper()

	if err := stack.environment.ConfigureCertAuthRole(ctx, caPEM, stack.environment.CertSPIFFEID); err != nil {
		t.Fatalf("configure OpenBao cert auth for %s: %v", sourceDescription, err)
	}
}

type spireProviderSource struct {
	BundlePEM []byte
	SocketDir string
}

func startSPIREProviderSource(t *testing.T, ctx context.Context, stack *providerFailureStack) spireProviderSource {
	t.Helper()

	serverImage := requireProviderImageFromEnv(t, envSPIREServerImage)
	agentImage := requireProviderImageFromEnv(t, envSPIREAgentImage)
	rootDir := t.TempDir()
	for _, dir := range []string{
		filepath.Join(rootDir, "server", "private"),
		filepath.Join(rootDir, "server", "data"),
		filepath.Join(rootDir, "agent", "data"),
		filepath.Join(rootDir, "sockets"),
	} {
		if err := os.MkdirAll(dir, 0o777); err != nil {
			t.Fatalf("create SPIRE directory %s: %v", dir, err)
		}
	}
	if err := os.Chmod(rootDir, 0o777); err != nil {
		t.Fatalf("chmod SPIRE root directory: %v", err)
	}
	writeSPIREConfigFiles(t, rootDir)

	serverName := stack.providerName + "-spire-server"
	agentName := stack.providerName + "-spire-agent"
	t.Cleanup(func() {
		removeContainer(t, context.Background(), stack.dockerPath, agentName)
		removeContainer(t, context.Background(), stack.dockerPath, serverName)
	})

	runDocker(t, ctx, stack.dockerPath,
		"run", "--detach",
		"--name", serverName,
		"--hostname", "spire-server",
		"--network", stack.networkName,
		"--user", "0:0",
		"--volume", rootDir+":/run/spire",
		"--entrypoint", "/opt/spire/bin/spire-server",
		serverImage,
		"run", "-config", "/run/spire/server.conf",
	)
	waitForDockerCommand(t, ctx, stack.dockerPath, serverName, 30*time.Second,
		"exec", serverName,
		"/opt/spire/bin/spire-server", "healthcheck",
		"-socketPath", spireServerSocketPath,
		"-shallow",
	)

	token := generateSPIREJoinToken(t, ctx, stack.dockerPath, serverName)
	if err := os.WriteFile(filepath.Join(rootDir, "agent.env"), []byte("JOIN_TOKEN="+token+"\n"), 0o600); err != nil {
		t.Fatalf("write SPIRE agent environment file: %v", err)
	}
	runDocker(t, ctx, stack.dockerPath,
		"run", "--detach",
		"--name", agentName,
		"--pid", "host",
		"--network", stack.networkName,
		"--user", "0:0",
		"--volume", rootDir+":/run/spire",
		"--env-file", filepath.Join(rootDir, "agent.env"),
		"--entrypoint", "/opt/spire/bin/spire-agent",
		agentImage,
		"run", "-config", "/run/spire/agent.conf", "-expandEnv",
	)
	waitForDockerCommand(t, ctx, stack.dockerPath, agentName, 30*time.Second,
		"exec", agentName,
		"/opt/spire/bin/spire-agent", "healthcheck",
		"-socketPath", spireAgentSocketPath,
		"-shallow",
	)

	parentID := waitForSPIREAgentID(t, ctx, stack.dockerPath, serverName)
	runDocker(t, ctx, stack.dockerPath,
		"exec", serverName,
		"/opt/spire/bin/spire-server", "entry", "create",
		"-socketPath", spireServerSocketPath,
		"-parentID", parentID,
		"-spiffeID", stack.environment.CertSPIFFEID,
		"-selector", "unix:uid:65532",
		"-output", "json",
	)
	waitForSPIREWorkloadSVID(t, ctx, stack, agentImage, rootDir)

	bundlePEM, err := runDockerOutput(ctx, stack.dockerPath,
		"exec", serverName,
		"/opt/spire/bin/spire-server", "bundle", "show",
		"-socketPath", spireServerSocketPath,
		"-format", "pem",
	)
	if err != nil {
		t.Fatalf("fetch SPIRE bundle: %v: %s", err, strings.TrimSpace(bundlePEM))
	}
	socketDir := filepath.Join(rootDir, "sockets")
	stack.providerStart.Volumes = append(stack.providerStart.Volumes, socketDir+":/run/spire/sockets")
	return spireProviderSource{
		BundlePEM: []byte(bundlePEM),
		SocketDir: socketDir,
	}
}

func buildSPIFFECertAuthProbe(t *testing.T, ctx context.Context, outputPath string) {
	t.Helper()

	goPath, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("find go binary: %v", err)
	}
	cmd := exec.CommandContext(ctx, goPath, "build", "-trimpath", "-tags", "certauth_spiffe", "-o", outputPath, "./test/e2e/certauthspiffeprobe")
	cmd.Dir = findRepoRoot(t)
	cmd.Env = append(os.Environ(),
		"CGO_ENABLED=0",
		"GOOS=linux",
		"GOARCH="+goruntime.GOARCH,
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build SPIFFE cert auth probe: %v: %s", err, strings.TrimSpace(string(output)))
	}
	if err := os.Chmod(outputPath, 0o755); err != nil {
		t.Fatalf("chmod SPIFFE cert auth probe: %v", err)
	}
}

func runSPIFFEProviderSourceProbe(
	t *testing.T,
	ctx context.Context,
	dockerPath string,
	providerImage string,
	networkName string,
	socketDir string,
	probePath string,
	spiffeID string,
) {
	t.Helper()

	probeName := fmt.Sprintf("obk-e2e-certauth-spire-probe-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		removeContainer(t, context.Background(), dockerPath, probeName)
	})
	output, err := runDockerOutput(ctx, dockerPath,
		"run", "--rm",
		"--name", probeName,
		"--network", networkName,
		"--user", "65532:65532",
		"--volume", socketDir+":/run/spire/sockets",
		"--volume", probePath+":/certauth-spiffe-probe:ro",
		"--entrypoint", "/certauth-spiffe-probe",
		providerImage,
		"--workload-api-socket", containerSPIFFEWorkloadAPISocket,
		"--spiffe-id", spiffeID,
		"--trust-domain", "example.org",
	)
	if err != nil {
		t.Fatalf("run SPIFFE cert auth source probe: %v: %s", err, strings.TrimSpace(output))
	}
}

func writeSPIREConfigFiles(t *testing.T, rootDir string) {
	t.Helper()

	serverConfig := `server {
    trust_domain = "example.org"
    bind_address = "0.0.0.0"
    bind_port = "8081"
    socket_path = "/run/spire/server/private/api.sock"
    data_dir = "/run/spire/server/data"
    log_level = "INFO"
}
plugins {
    DataStore "sql" {
        plugin_data {
            database_type = "sqlite3"
            connection_string = "/run/spire/server/data/datastore.sqlite3"
        }
    }
    NodeAttestor "join_token" {
        plugin_data {}
    }
    KeyManager "disk" {
        plugin_data {
            keys_path = "/run/spire/server/data/keys.json"
        }
    }
}
`
	agentConfig := `agent {
    trust_domain = "example.org"
    data_dir = "/run/spire/agent/data"
    log_level = "INFO"
    server_address = "spire-server"
    server_port = "8081"
    socket_path = "/run/spire/sockets/agent.sock"
    insecure_bootstrap = true
    join_token = "$JOIN_TOKEN"
}
plugins {
    NodeAttestor "join_token" {
        plugin_data {}
    }
    KeyManager "disk" {
        plugin_data {
            directory = "/run/spire/agent/data"
        }
    }
    WorkloadAttestor "unix" {
        plugin_data {}
    }
}
`
	if err := os.WriteFile(filepath.Join(rootDir, "server.conf"), []byte(serverConfig), 0o644); err != nil {
		t.Fatalf("write SPIRE server config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "agent.conf"), []byte(agentConfig), 0o644); err != nil {
		t.Fatalf("write SPIRE agent config: %v", err)
	}
}

func generateSPIREJoinToken(t *testing.T, ctx context.Context, dockerPath string, serverName string) string {
	t.Helper()

	output, err := runDockerOutput(ctx, dockerPath,
		"exec", serverName,
		"/opt/spire/bin/spire-server", "token", "generate",
		"-socketPath", spireServerSocketPath,
		"-ttl", "600",
		"-output", "json",
	)
	if err != nil {
		t.Fatalf("generate SPIRE join token: %v: %s", err, strings.TrimSpace(output))
	}
	var body spireTokenGenerateResponse
	if err := json.Unmarshal([]byte(output), &body); err != nil {
		t.Fatalf("decode SPIRE join token response: %v", err)
	}
	if body.Value == "" {
		t.Fatal("SPIRE join token response did not include a token value")
	}
	return body.Value
}

func waitForSPIREAgentID(t *testing.T, ctx context.Context, dockerPath string, serverName string) string {
	t.Helper()

	output := waitForDockerCommand(t, ctx, dockerPath, serverName, 30*time.Second,
		"exec", serverName,
		"/opt/spire/bin/spire-server", "agent", "list",
		"-socketPath", spireServerSocketPath,
		"-output", "json",
	)
	var body spireAgentListResponse
	if err := json.Unmarshal([]byte(output), &body); err != nil {
		t.Fatalf("decode SPIRE agent list response: %v", err)
	}
	if len(body.Agents) == 0 {
		t.Fatal("SPIRE server did not report an attested agent")
	}
	parentID, err := body.Agents[0].ID.String()
	if err != nil {
		t.Fatalf("format SPIRE agent ID: %v", err)
	}
	return parentID
}

func waitForSPIREWorkloadSVID(
	t *testing.T,
	ctx context.Context,
	stack *providerFailureStack,
	agentImage string,
	rootDir string,
) {
	t.Helper()

	waitForDockerCommand(t, ctx, stack.dockerPath, stack.providerName+"-spire-agent", 30*time.Second,
		"run", "--rm",
		"--network", stack.networkName,
		"--user", "65532:65532",
		"--volume", rootDir+":/run/spire",
		"--entrypoint", "/opt/spire/bin/spire-agent",
		agentImage,
		"api", "fetch", "x509",
		"-socketPath", spireAgentSocketPath,
		"-silent",
	)
}

func waitForDockerCommand(
	t *testing.T,
	ctx context.Context,
	dockerPath string,
	logContainerName string,
	timeout time.Duration,
	args ...string,
) string {
	t.Helper()

	deadline := time.Now().Add(timeout)
	var lastOutput string
	var lastErr error
	for time.Now().Before(deadline) {
		output, err := runDockerOutput(ctx, dockerPath, args...)
		if err == nil {
			return output
		}
		lastOutput = output
		lastErr = err
		select {
		case <-ctx.Done():
			t.Fatalf("docker wait command canceled: %v", ctx.Err())
		case <-time.After(250 * time.Millisecond):
		}
	}
	logs := dockerLogs(context.Background(), dockerPath, logContainerName)
	t.Fatalf("docker %s did not succeed within %s: %v: %s\ncontainer logs:\n%s",
		strings.Join(args, " "),
		timeout,
		lastErr,
		strings.TrimSpace(lastOutput),
		logs,
	)
	return ""
}

type spireTokenGenerateResponse struct {
	Value string `json:"value"`
}

type spireAgentListResponse struct {
	Agents []spireAgentListEntry `json:"agents"`
}

type spireAgentListEntry struct {
	ID spireID `json:"id"`
}

type spireID struct {
	Path        string `json:"path"`
	TrustDomain string `json:"trust_domain"`
}

func (id spireID) String() (string, error) {
	if id.Path == "" || id.TrustDomain == "" {
		return "", fmt.Errorf("SPIRE ID is incomplete")
	}
	return "spiffe://" + id.TrustDomain + id.Path, nil
}
