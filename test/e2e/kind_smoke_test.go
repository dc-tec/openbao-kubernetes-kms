//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/base64"
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
	envKindCI        = "E2E_KIND_CI"
	envKindBinary    = "KIND"
	envKubectlBinary = "KUBECTL"
	envKindNodeImage = "E2E_KIND_NODE_IMAGE"

	kindProviderConfigPath     = "/etc/openbao-kms/config.yaml"
	kindProviderCAPath         = "/etc/openbao-kms/tls/ca.crt"
	kindProviderJWTPath        = "/var/lib/openbao-kms/identity.jwt"
	kindProviderSocketPath     = "/run/openbao-kms/kms.sock"
	kindProviderStatePath      = "/var/lib/openbao-kms/state/key-registry.json"
	kindEncryptionConfigDir    = "/etc/kubernetes/encryption/openbao-kms"
	kindEncryptionConfigPath   = kindEncryptionConfigDir + "/encryption-config.yaml"
	kindProviderStaticPodPath  = "/etc/kubernetes/manifests/bao-kms-provider.yaml"
	kindAPIServerManifestPath  = "/etc/kubernetes/manifests/kube-apiserver.yaml"
	kindSecretName             = "obk-kind-smoke"
	kindControlPlaneNodeSuffix = "-control-plane"

	kindConvergenceControlPlaneCount = 3
)

func TestKindKMSV2SmokeE2E(t *testing.T) {
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

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	dockerPath := requireToolOrSkip(t, ctx, framework.EnvDefault(framework.EnvDockerBinary, "docker"))
	kindPath := requireToolOrSkip(t, ctx, framework.EnvDefault(envKindBinary, "kind"))
	kubectlPath := requireToolOrSkip(t, ctx, framework.EnvDefault(envKubectlBinary, "kubectl"))

	clusterName := fmt.Sprintf("obk-kind-%d", time.Now().UnixNano())
	nodeName := clusterName + kindControlPlaneNodeSuffix
	contextName := "kind-" + clusterName
	var environment *framework.OpenBaoEnvironment
	t.Cleanup(func() {
		if environment != nil {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cleanupCancel()
			if err := environment.Close(cleanupCtx); err != nil {
				t.Errorf("close OpenBao environment: %v", err)
			}
		}
		if !strings.EqualFold(os.Getenv(framework.EnvSkipCleanup), "true") {
			deleteKindCluster(t, context.Background(), kindPath, clusterName)
		}
	})

	createKindCluster(t, ctx, kindPath, clusterName, nodeImage)

	environment = startKindOpenBao(t, ctx)
	loadProviderImageIntoKind(t, ctx, kindPath, clusterName, providerImage)
	stageKindProvider(t, ctx, dockerPath, nodeName, providerImage, environment)
	waitForKindProviderSocket(t, ctx, dockerPath, nodeName)
	enableKindAPIServerKMS(t, ctx, dockerPath, kubectlPath, contextName, nodeName)

	secretValue := "kind-smoke-secret-" + strconvTime(time.Now())
	createKindSecret(t, ctx, kubectlPath, contextName, secretValue)
	assertKindSecretReadable(t, ctx, kubectlPath, contextName, secretValue)
	assertKindEtcdEncrypted(t, ctx, dockerPath, nodeName, secretValue)
	restartKindAPIServer(t, ctx, dockerPath, kubectlPath, contextName, nodeName)
	assertKindSecretReadable(t, ctx, kubectlPath, contextName, secretValue)
}

func TestKindMultiControlPlaneConvergenceE2E(t *testing.T) {
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

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
	defer cancel()

	dockerPath := requireToolOrSkip(t, ctx, framework.EnvDefault(framework.EnvDockerBinary, "docker"))
	kindPath := requireToolOrSkip(t, ctx, framework.EnvDefault(envKindBinary, "kind"))
	kubectlPath := requireToolOrSkip(t, ctx, framework.EnvDefault(envKubectlBinary, "kubectl"))

	clusterName := fmt.Sprintf("obk-kind-mcp-%d", time.Now().UnixNano())
	nodeNames := kindControlPlaneNodeNames(clusterName, kindConvergenceControlPlaneCount)
	contextName := "kind-" + clusterName
	var environment *framework.OpenBaoEnvironment
	t.Cleanup(func() {
		if environment != nil {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cleanupCancel()
			if err := environment.Close(cleanupCtx); err != nil {
				t.Errorf("close OpenBao environment: %v", err)
			}
		}
		if !strings.EqualFold(os.Getenv(framework.EnvSkipCleanup), "true") {
			deleteKindCluster(t, context.Background(), kindPath, clusterName)
		}
	})

	createKindMultiControlPlaneCluster(t, ctx, kindPath, clusterName, nodeImage, kindConvergenceControlPlaneCount)

	environment = startKindOpenBao(t, ctx)
	loadProviderImageIntoKind(t, ctx, kindPath, clusterName, providerImage)
	for _, nodeName := range nodeNames {
		stageKindProvider(t, ctx, dockerPath, nodeName, providerImage, environment)
	}
	for _, nodeName := range nodeNames {
		waitForKindProviderSocket(t, ctx, dockerPath, nodeName)
		enableKindAPIServerKMS(t, ctx, dockerPath, kubectlPath, contextName, nodeName)
		waitForKindAPIServerContainer(t, ctx, dockerPath, nodeName)
	}

	secretName := "obk-kind-mcp"
	secretValue := "kind-mcp-secret-" + strconvTime(time.Now())
	createKindSecretNamed(t, ctx, kubectlPath, contextName, secretName, secretValue)
	assertKindSecretReadableNamed(t, ctx, kubectlPath, contextName, secretName, secretValue)
	for _, nodeName := range nodeNames {
		assertKindEtcdEncryptedNamed(t, ctx, dockerPath, nodeName, secretName, secretValue)
	}
	for _, nodeName := range nodeNames {
		assertKindSecretReadableThroughOnlyAPIServer(t, ctx, dockerPath, kubectlPath, contextName, nodeNames, nodeName, secretName, secretValue)
	}
	for _, nodeName := range nodeNames {
		restartKindAPIServer(t, ctx, dockerPath, kubectlPath, contextName, nodeName)
		assertKindSecretReadableNamed(t, ctx, kubectlPath, contextName, secretName, secretValue)
	}
}

func TestKindStaticPodUpgradeRollbackE2E(t *testing.T) {
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

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	dockerPath := requireToolOrSkip(t, ctx, framework.EnvDefault(framework.EnvDockerBinary, "docker"))
	kindPath := requireToolOrSkip(t, ctx, framework.EnvDefault(envKindBinary, "kind"))
	kubectlPath := requireToolOrSkip(t, ctx, framework.EnvDefault(envKubectlBinary, "kubectl"))

	clusterName := fmt.Sprintf("obk-kind-upgrade-%d", time.Now().UnixNano())
	nodeName := clusterName + kindControlPlaneNodeSuffix
	contextName := "kind-" + clusterName
	var environment *framework.OpenBaoEnvironment
	t.Cleanup(func() {
		if environment != nil {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cleanupCancel()
			if err := environment.Close(cleanupCtx); err != nil {
				t.Errorf("close OpenBao environment: %v", err)
			}
		}
		if !strings.EqualFold(os.Getenv(framework.EnvSkipCleanup), "true") {
			deleteKindCluster(t, context.Background(), kindPath, clusterName)
		}
	})

	createKindCluster(t, ctx, kindPath, clusterName, nodeImage)

	environment = startKindOpenBao(t, ctx)
	loadProviderImageIntoKind(t, ctx, kindPath, clusterName, providerImage)
	stageKindProvider(t, ctx, dockerPath, nodeName, providerImage, environment)
	waitForKindProviderSocket(t, ctx, dockerPath, nodeName)
	enableKindAPIServerKMS(t, ctx, dockerPath, kubectlPath, contextName, nodeName)

	secretName := "obk-kind-upgrade"
	secretValue := "kind-upgrade-secret-" + strconvTime(time.Now())
	createKindSecretNamed(t, ctx, kubectlPath, contextName, secretName, secretValue)
	assertKindSecretReadableNamed(t, ctx, kubectlPath, contextName, secretName, secretValue)

	originalProviderID := kindProviderContainerID(t, ctx, dockerPath, nodeName)
	backupKindProviderManifest(t, ctx, dockerPath, nodeName)
	applyKindProviderManifestStep(t, ctx, dockerPath, nodeName, "upgrade")
	waitForKindProviderContainerRestart(t, ctx, dockerPath, nodeName, originalProviderID)
	waitForKindProviderSocket(t, ctx, dockerPath, nodeName)
	assertKindSecretReadableNamed(t, ctx, kubectlPath, contextName, secretName, secretValue)

	upgradedSecretName := "obk-kind-upgrade-after"
	upgradedSecretValue := "kind-upgrade-after-secret-" + strconvTime(time.Now())
	createKindSecretNamed(t, ctx, kubectlPath, contextName, upgradedSecretName, upgradedSecretValue)
	assertKindSecretReadableNamed(t, ctx, kubectlPath, contextName, upgradedSecretName, upgradedSecretValue)

	upgradedProviderID := kindProviderContainerID(t, ctx, dockerPath, nodeName)
	restoreKindProviderManifest(t, ctx, dockerPath, nodeName)
	waitForKindProviderContainerRestart(t, ctx, dockerPath, nodeName, upgradedProviderID)
	waitForKindProviderSocket(t, ctx, dockerPath, nodeName)
	assertKindSecretReadableNamed(t, ctx, kubectlPath, contextName, secretName, secretValue)
	assertKindSecretReadableNamed(t, ctx, kubectlPath, contextName, upgradedSecretName, upgradedSecretValue)
	restartKindAPIServer(t, ctx, dockerPath, kubectlPath, contextName, nodeName)
	assertKindSecretReadableNamed(t, ctx, kubectlPath, contextName, secretName, secretValue)
	assertKindSecretReadableNamed(t, ctx, kubectlPath, contextName, upgradedSecretName, upgradedSecretValue)
}

func kindCIEnabled() bool {
	return strings.EqualFold(os.Getenv(envKindCI), "true")
}

func requireToolOrSkip(t *testing.T, ctx context.Context, binary string) string {
	t.Helper()

	path, err := exec.LookPath(binary)
	if err != nil {
		t.Skipf("%s is not available: %v", binary, err)
	}
	if binary == framework.EnvDefault(framework.EnvDockerBinary, "docker") {
		if output, err := exec.CommandContext(ctx, path, "version", "--format", "{{.Server.Version}}").CombinedOutput(); err != nil {
			t.Skipf("%s: %s", framework.ErrDockerUnavailable, strings.TrimSpace(string(output)))
		}
	}
	return path
}

func createKindCluster(t *testing.T, ctx context.Context, kindPath string, clusterName string, nodeImage string) {
	t.Helper()

	output, err := runOutput(ctx, kindPath,
		"create", "cluster",
		"--name", clusterName,
		"--image", nodeImage,
		"--wait", "2m",
	)
	if err != nil {
		t.Fatalf("create Kind cluster: %v: %s", err, strings.TrimSpace(output))
	}
}

func createKindMultiControlPlaneCluster(
	t *testing.T,
	ctx context.Context,
	kindPath string,
	clusterName string,
	nodeImage string,
	controlPlaneCount int,
) {
	t.Helper()

	var config strings.Builder
	config.WriteString("kind: Cluster\n")
	config.WriteString("apiVersion: kind.x-k8s.io/v1alpha4\n")
	config.WriteString("nodes:\n")
	for range controlPlaneCount {
		config.WriteString("- role: control-plane\n")
	}

	configPath := filepath.Join(t.TempDir(), "kind-cluster.yaml")
	if err := os.WriteFile(configPath, []byte(config.String()), 0o600); err != nil {
		t.Fatalf("write Kind multi-control-plane config: %v", err)
	}
	output, err := runOutput(ctx, kindPath,
		"create", "cluster",
		"--name", clusterName,
		"--image", nodeImage,
		"--config", configPath,
		"--wait", "4m",
	)
	if err != nil {
		t.Fatalf("create multi-control-plane Kind cluster: %v: %s", err, strings.TrimSpace(output))
	}
}

func kindControlPlaneNodeNames(clusterName string, count int) []string {
	names := make([]string, 0, count)
	for index := range count {
		if index == 0 {
			names = append(names, clusterName+kindControlPlaneNodeSuffix)
			continue
		}
		names = append(names, fmt.Sprintf("%s%s%d", clusterName, kindControlPlaneNodeSuffix, index+1))
	}
	return names
}

func startKindOpenBao(t *testing.T, ctx context.Context) *framework.OpenBaoEnvironment {
	t.Helper()

	return startKindOpenBaoWithConfig(t, ctx, framework.OpenBaoEnvironmentConfig{})
}

func startKindOpenBaoWithConfig(
	t *testing.T,
	ctx context.Context,
	config framework.OpenBaoEnvironmentConfig,
) *framework.OpenBaoEnvironment {
	t.Helper()

	config.NetworkName = "kind"
	environment, err := framework.StartOpenBaoEnvironment(ctx, config)
	if errors.Is(err, framework.ErrDockerUnavailable) {
		t.Skip(err.Error())
	}
	if err != nil {
		t.Fatalf("start OpenBao environment: %v", err)
	}
	return environment
}

func loadProviderImageIntoKind(
	t *testing.T,
	ctx context.Context,
	kindPath string,
	clusterName string,
	providerImage string,
) {
	t.Helper()

	output, err := runOutput(ctx, kindPath, "load", "docker-image", "--name", clusterName, providerImage)
	if err != nil {
		t.Fatalf("load provider image into Kind: %v: %s", err, strings.TrimSpace(output))
	}
}

func stageKindProvider(
	t *testing.T,
	ctx context.Context,
	dockerPath string,
	nodeName string,
	providerImage string,
	environment *framework.OpenBaoEnvironment,
) {
	t.Helper()

	stagingDir := t.TempDir()
	writeKindProviderConfig(t, filepath.Join(stagingDir, "provider.yaml"), environment)
	writeKindProviderStaticPod(t, filepath.Join(stagingDir, "bao-kms-provider.yaml"), providerImage)
	writeKindEncryptionConfig(t, filepath.Join(stagingDir, "encryption-config.yaml"))
	copyFile(t, environment.CACertFile, filepath.Join(stagingDir, "ca.crt"), 0o644)
	copyFile(t, environment.JWTFile, filepath.Join(stagingDir, "identity.jwt"), 0o600)

	runDocker(t, ctx, dockerPath, "exec", nodeName, "mkdir", "-p",
		"/etc/openbao-kms/tls",
		"/var/lib/openbao-kms/state",
		"/run/openbao-kms",
		kindEncryptionConfigDir,
	)
	dockerCopy(t, ctx, dockerPath, filepath.Join(stagingDir, "provider.yaml"), nodeName+":"+kindProviderConfigPath)
	dockerCopy(t, ctx, dockerPath, filepath.Join(stagingDir, "ca.crt"), nodeName+":"+kindProviderCAPath)
	dockerCopy(t, ctx, dockerPath, filepath.Join(stagingDir, "identity.jwt"), nodeName+":"+kindProviderJWTPath)
	dockerCopy(t, ctx, dockerPath, filepath.Join(stagingDir, "encryption-config.yaml"), nodeName+":"+kindEncryptionConfigPath)
	runDocker(t, ctx, dockerPath, "exec", nodeName, "sh", "-c", kindProviderPermissionsScript)
	dockerCopy(t, ctx, dockerPath, filepath.Join(stagingDir, "bao-kms-provider.yaml"), nodeName+":"+kindProviderStaticPodPath)
}

const kindProviderPermissionsScript = `set -eu
chown -R 65532:65532 /etc/openbao-kms /var/lib/openbao-kms
chown -R 65532:1234 /run/openbao-kms
chmod 0700 /etc/openbao-kms /etc/openbao-kms/tls /var/lib/openbao-kms /var/lib/openbao-kms/state
chmod 2750 /run/openbao-kms
chmod 0600 /etc/openbao-kms/config.yaml /var/lib/openbao-kms/identity.jwt
if [ -f /var/lib/openbao-kms/state/key-registry.json ]; then chmod 0600 /var/lib/openbao-kms/state/key-registry.json; fi
chmod 0644 /etc/openbao-kms/tls/ca.crt /etc/kubernetes/encryption/openbao-kms/encryption-config.yaml
`

func waitForKindProviderSocket(t *testing.T, ctx context.Context, dockerPath string, nodeName string) {
	t.Helper()

	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := runDockerOutput(ctx, dockerPath, "exec", nodeName, "test", "-S", kindProviderSocketPath); err == nil {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	logs := dockerLogs(ctx, dockerPath, nodeName)
	t.Fatalf("provider socket did not become available in Kind node\nnode logs:\n%s", logs)
}

func enableKindAPIServerKMS(
	t *testing.T,
	ctx context.Context,
	dockerPath string,
	kubectlPath string,
	contextName string,
	nodeName string,
) {
	t.Helper()

	manifest, err := runDockerOutput(ctx, dockerPath, "exec", nodeName, "cat", kindAPIServerManifestPath)
	if err != nil {
		t.Fatalf("read kube-apiserver manifest: %v", err)
	}
	patched, err := patchKindAPIServerManifest(manifest)
	if err != nil {
		t.Fatalf("patch kube-apiserver manifest: %v", err)
	}
	staged := filepath.Join(t.TempDir(), "kube-apiserver.yaml")
	if err := os.WriteFile(staged, []byte(patched), 0o600); err != nil {
		t.Fatalf("write patched kube-apiserver manifest: %v", err)
	}
	dockerCopy(t, ctx, dockerPath, staged, nodeName+":"+kindAPIServerManifestPath)
	if err := waitForKindAPIServerReady(ctx, kubectlPath, contextName); err != nil {
		t.Fatalf("kube-apiserver did not become ready: %v\nkube-apiserver status:\n%s\nkube-apiserver logs:\n%s\nprovider status:\n%s\nprovider logs:\n%s\nnode logs:\n%s\npatched manifest:\n%s",
			err,
			kindContainerStatus(ctx, dockerPath, nodeName, "kube-apiserver"),
			kindContainerLogs(ctx, dockerPath, nodeName, "kube-apiserver"),
			kindContainerStatus(ctx, dockerPath, nodeName, "^bao-kms-provider$"),
			kindContainerLogs(ctx, dockerPath, nodeName, "^bao-kms-provider$"),
			dockerLogs(ctx, dockerPath, nodeName),
			kindFile(ctx, dockerPath, nodeName, kindAPIServerManifestPath),
		)
	}
}

func patchKindAPIServerManifest(manifest string) (string, error) {
	if strings.Contains(manifest, "--encryption-provider-config=") {
		return manifest, nil
	}

	commandAnchor := "    - --tls-private-key-file=/etc/kubernetes/pki/apiserver.key\n"
	manifest = strings.Replace(manifest, commandAnchor, commandAnchor+
		"    - --encryption-provider-config="+kindEncryptionConfigPath+"\n", 1)
	if !strings.Contains(manifest, "--encryption-provider-config="+kindEncryptionConfigPath) {
		return "", fmt.Errorf("kube-apiserver command anchor not found")
	}

	mountAnchor := "    - mountPath: /usr/share/ca-certificates\n      name: usr-share-ca-certificates\n      readOnly: true\n"
	manifest = strings.Replace(manifest, mountAnchor, mountAnchor+
		"    - mountPath: "+kindEncryptionConfigDir+"\n      name: openbao-kms-encryption\n      readOnly: true\n"+
		"    - mountPath: /run/openbao-kms\n      name: openbao-kms-run\n", 1)
	if !strings.Contains(manifest, "name: openbao-kms-encryption") ||
		!strings.Contains(manifest, "name: openbao-kms-run") {
		return "", fmt.Errorf("kube-apiserver volumeMount anchor not found")
	}

	volumeAnchor := "  - hostPath:\n      path: /usr/share/ca-certificates\n      type: DirectoryOrCreate\n    name: usr-share-ca-certificates\n"
	manifest = strings.Replace(manifest, volumeAnchor, volumeAnchor+
		"  - hostPath:\n      path: "+kindEncryptionConfigDir+"\n      type: DirectoryOrCreate\n    name: openbao-kms-encryption\n"+
		"  - hostPath:\n      path: /run/openbao-kms\n      type: Directory\n    name: openbao-kms-run\n", 1)
	if !strings.Contains(manifest, "path: "+kindEncryptionConfigDir) ||
		!strings.Contains(manifest, "path: /run/openbao-kms") {
		return "", fmt.Errorf("kube-apiserver volume anchor not found")
	}
	return manifest, nil
}

func createKindSecret(
	t *testing.T,
	ctx context.Context,
	kubectlPath string,
	contextName string,
	secretValue string,
) {
	t.Helper()

	createKindSecretNamed(t, ctx, kubectlPath, contextName, kindSecretName, secretValue)
}

func createKindSecretNamed(
	t *testing.T,
	ctx context.Context,
	kubectlPath string,
	contextName string,
	secretName string,
	secretValue string,
) {
	t.Helper()

	secretFile := filepath.Join(t.TempDir(), "secret-value")
	if err := os.WriteFile(secretFile, []byte(secretValue), 0o600); err != nil {
		t.Fatalf("write Secret payload file: %v", err)
	}
	output, err := runOutput(ctx, kubectlPath,
		"--context", contextName,
		"create", "secret", "generic", secretName,
		"--from-file=value="+secretFile,
	)
	if err != nil {
		t.Fatalf("create Kubernetes Secret: %v: %s", err, strings.TrimSpace(output))
	}
}

func assertKindSecretReadable(
	t *testing.T,
	ctx context.Context,
	kubectlPath string,
	contextName string,
	secretValue string,
) {
	t.Helper()

	assertKindSecretReadableNamed(t, ctx, kubectlPath, contextName, kindSecretName, secretValue)
}

func assertKindSecretReadableNamed(
	t *testing.T,
	ctx context.Context,
	kubectlPath string,
	contextName string,
	secretName string,
	secretValue string,
) {
	t.Helper()

	output, err := runOutput(ctx, kubectlPath,
		"--context", contextName,
		"get", "secret", secretName,
		"-o", "jsonpath={.data.value}",
	)
	if err != nil {
		t.Fatalf("read Kubernetes Secret: %v", err)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(output))
	if err != nil {
		t.Fatalf("decode Kubernetes Secret value: %v", err)
	}
	if string(decoded) != secretValue {
		t.Fatal("Kubernetes Secret value did not round-trip")
	}
}

func assertKindEtcdEncrypted(
	t *testing.T,
	ctx context.Context,
	dockerPath string,
	nodeName string,
	secretValue string,
) {
	t.Helper()

	assertKindEtcdEncryptedNamed(t, ctx, dockerPath, nodeName, kindSecretName, secretValue)
}

func assertKindEtcdEncryptedNamed(
	t *testing.T,
	ctx context.Context,
	dockerPath string,
	nodeName string,
	secretName string,
	secretValue string,
) {
	t.Helper()

	output, err := runDockerOutput(ctx, dockerPath, "exec", nodeName, "sh", "-c", kindEtcdGetSecretScript(secretName))
	if err != nil {
		t.Fatalf("read Secret from etcd: %v: %s", err, redactKindEtcdOutput(output, secretValue))
	}
	raw := []byte(output)
	if bytes.Contains(raw, []byte(secretValue)) {
		t.Fatal("etcd stored the Kubernetes Secret plaintext")
	}
	if !bytes.Contains(raw, []byte("k8s:enc:kms:v2:")) {
		t.Fatal("etcd Secret value did not use Kubernetes KMS v2 envelope format")
	}
}

func kindEtcdGetSecretScript(secretName string) string {
	return fmt.Sprintf(`set -eu
cid="$(crictl ps --name etcd -q | head -n1)"
if [ -z "$cid" ]; then
  printf '%%s\n' 'no etcd container found'
  crictl ps -a
  exit 1
fi
crictl exec "$cid" etcdctl \
  --endpoints=https://127.0.0.1:2379 \
  --cacert=/etc/kubernetes/pki/etcd/ca.crt \
  --cert=/etc/kubernetes/pki/etcd/server.crt \
  --key=/etc/kubernetes/pki/etcd/server.key \
  get /registry/secrets/default/%s
`, secretName)
}

func redactKindEtcdOutput(output string, secretValue string) string {
	redacted := strings.ReplaceAll(output, secretValue, "[redacted-secret]")
	redacted = strings.TrimSpace(redacted)
	if redacted == "" {
		return "<empty>"
	}
	return redacted
}

func restartKindAPIServer(
	t *testing.T,
	ctx context.Context,
	dockerPath string,
	kubectlPath string,
	contextName string,
	nodeName string,
) {
	t.Helper()

	_, err := runDockerOutput(ctx, dockerPath, "exec", nodeName, "sh", "-c", kindRestartAPIServerScript)
	if err != nil {
		t.Fatalf("restart kube-apiserver container: %v\nkube-apiserver status:\n%s",
			err,
			kindContainerStatus(ctx, dockerPath, nodeName, "kube-apiserver"),
		)
	}
	waitForKindAPIServer(t, ctx, kubectlPath, contextName)
}

const kindRestartAPIServerScript = `set -eu
cid="$(crictl ps --name kube-apiserver -q | head -n1)"
if [ -z "$cid" ]; then
  printf '%s\n' 'no kube-apiserver container found'
  crictl ps -a
  exit 1
fi
crictl stop "$cid" >/dev/null
`

func waitForKindAPIServer(t *testing.T, ctx context.Context, kubectlPath string, contextName string) {
	t.Helper()

	if err := waitForKindAPIServerReady(ctx, kubectlPath, contextName); err != nil {
		t.Fatalf("kube-apiserver did not become ready: %v", err)
	}
}

func waitForKindAPIServerContainer(t *testing.T, ctx context.Context, dockerPath string, nodeName string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		output, err := runDockerOutput(ctx, dockerPath, "exec", nodeName, "crictl", "ps", "--name", "kube-apiserver", "-q")
		if err == nil && strings.TrimSpace(output) != "" {
			return
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("kube-apiserver container did not become ready on %s\nkube-apiserver status:\n%s",
		nodeName,
		kindContainerStatus(ctx, dockerPath, nodeName, "kube-apiserver"),
	)
}

func assertKindSecretReadableThroughOnlyAPIServer(
	t *testing.T,
	ctx context.Context,
	dockerPath string,
	kubectlPath string,
	contextName string,
	nodeNames []string,
	targetNode string,
	secretName string,
	secretValue string,
) {
	t.Helper()

	heldNodes := make([]string, 0, len(nodeNames)-1)
	for _, nodeName := range nodeNames {
		if nodeName == targetNode {
			continue
		}
		holdKindAPIServer(t, ctx, dockerPath, nodeName)
		heldNodes = append(heldNodes, nodeName)
	}
	defer restoreHeldKindAPIServers(t, ctx, dockerPath, kubectlPath, contextName, heldNodes)

	waitForKindAPIServer(t, ctx, kubectlPath, contextName)
	assertKindSecretReadableNamed(t, ctx, kubectlPath, contextName, secretName, secretValue)
}

func holdKindAPIServer(t *testing.T, ctx context.Context, dockerPath string, nodeName string) {
	t.Helper()

	_, err := runDockerOutput(ctx, dockerPath, "exec", nodeName, "sh", "-c", kindHoldAPIServerScript)
	if err != nil {
		t.Fatalf("hold kube-apiserver on %s: %v\nkube-apiserver status:\n%s",
			nodeName,
			err,
			kindContainerStatus(ctx, dockerPath, nodeName, "kube-apiserver"),
		)
	}
}

const kindHoldAPIServerScript = `set -eu
hold=/etc/kubernetes/manifests/kube-apiserver.yaml.hold
if [ ! -f "$hold" ]; then
  mv /etc/kubernetes/manifests/kube-apiserver.yaml "$hold"
fi
cid="$(crictl ps --name kube-apiserver -q | head -n1)"
if [ -n "$cid" ]; then crictl stop "$cid" >/dev/null; fi
attempt=0
while [ "$attempt" -lt 60 ]; do
  if [ -z "$(crictl ps --name kube-apiserver -q | head -n1)" ]; then exit 0; fi
  attempt=$((attempt + 1))
  sleep 1
done
printf '%s\n' 'kube-apiserver still running after manifest hold'
crictl ps -a --name kube-apiserver
exit 1
`

func restoreHeldKindAPIServers(
	t *testing.T,
	ctx context.Context,
	dockerPath string,
	kubectlPath string,
	contextName string,
	nodeNames []string,
) {
	t.Helper()

	for _, nodeName := range nodeNames {
		_, err := runDockerOutput(ctx, dockerPath, "exec", nodeName, "sh", "-c", kindRestoreAPIServerScript)
		if err != nil {
			t.Fatalf("restore kube-apiserver on %s: %v\nkube-apiserver status:\n%s",
				nodeName,
				err,
				kindContainerStatus(ctx, dockerPath, nodeName, "kube-apiserver"),
			)
		}
	}
	waitForKindAPIServer(t, ctx, kubectlPath, contextName)
	for _, nodeName := range nodeNames {
		waitForKindAPIServerContainer(t, ctx, dockerPath, nodeName)
	}
}

const kindRestoreAPIServerScript = `set -eu
hold=/etc/kubernetes/manifests/kube-apiserver.yaml.hold
if [ -f "$hold" ]; then
  mv "$hold" /etc/kubernetes/manifests/kube-apiserver.yaml
fi
`

func backupKindProviderManifest(t *testing.T, ctx context.Context, dockerPath string, nodeName string) {
	t.Helper()

	_, err := runDockerOutput(ctx, dockerPath, "exec", nodeName, "cp", kindProviderStaticPodPath, kindProviderStaticPodPath+".rollback")
	if err != nil {
		t.Fatalf("backup provider static pod manifest: %v", err)
	}
}

func restoreKindProviderManifest(t *testing.T, ctx context.Context, dockerPath string, nodeName string) {
	t.Helper()

	_, err := runDockerOutput(ctx, dockerPath, "exec", nodeName, "cp", kindProviderStaticPodPath+".rollback", kindProviderStaticPodPath)
	if err != nil {
		t.Fatalf("restore provider static pod manifest: %v", err)
	}
}

func applyKindProviderManifestStep(
	t *testing.T,
	ctx context.Context,
	dockerPath string,
	nodeName string,
	step string,
) {
	t.Helper()

	manifest := kindFile(ctx, dockerPath, nodeName, kindProviderStaticPodPath)
	anchor := "      args:\n        - serve\n"
	mutated := strings.Replace(manifest, anchor,
		fmt.Sprintf("      env:\n        - name: OPENBAO_KMS_E2E_STATIC_POD_STEP\n          value: %q\n", step)+anchor,
		1,
	)
	if mutated == manifest {
		t.Fatal("provider static pod args anchor not found")
	}
	staged := filepath.Join(t.TempDir(), "bao-kms-provider.yaml")
	if err := os.WriteFile(staged, []byte(mutated), 0o600); err != nil {
		t.Fatalf("write upgraded provider static pod manifest: %v", err)
	}
	dockerCopy(t, ctx, dockerPath, staged, nodeName+":"+kindProviderStaticPodPath)
}

func kindProviderContainerID(t *testing.T, ctx context.Context, dockerPath string, nodeName string) string {
	t.Helper()

	output, err := runDockerOutput(ctx, dockerPath, "exec", nodeName, "crictl", "ps", "--name", "^bao-kms-provider$", "-q")
	if err != nil {
		t.Fatalf("read provider container ID: %v", err)
	}
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		t.Fatal("provider container ID is empty")
	}
	return trimmed
}

func waitForKindProviderContainerRestart(
	t *testing.T,
	ctx context.Context,
	dockerPath string,
	nodeName string,
	previousID string,
) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		output, err := runDockerOutput(ctx, dockerPath, "exec", nodeName, "crictl", "ps", "--name", "^bao-kms-provider$", "-q")
		if err == nil {
			currentID := strings.TrimSpace(output)
			if currentID != "" && currentID != previousID {
				return
			}
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("provider static pod did not restart on %s\nprovider status:\n%s\nprovider logs:\n%s",
		nodeName,
		kindContainerStatus(ctx, dockerPath, nodeName, "^bao-kms-provider$"),
		kindContainerLogs(ctx, dockerPath, nodeName, "^bao-kms-provider$"),
	)
}

func waitForKindAPIServerReady(ctx context.Context, kubectlPath string, contextName string) error {
	deadline := time.Now().Add(2 * time.Minute)
	var lastErr error
	for time.Now().Before(deadline) {
		if output, err := runOutput(ctx, kubectlPath, "--context", contextName, "get", "--raw=/readyz"); err == nil &&
			strings.Contains(output, "ok") {
			return nil
		} else {
			lastErr = err
		}
		time.Sleep(time.Second)
	}
	return lastErr
}

func writeKindProviderConfig(t *testing.T, path string, environment *framework.OpenBaoEnvironment) {
	t.Helper()

	raw := fmt.Sprintf(`configVersion: v1alpha1
server:
  socketPath: %q
  socketMode: "0660"
  socketGroup: "1234"
  metricsAddress: ""
  healthAddress: ""
openbao:
  address: %q
  caCertFile: %q
  tlsServerName: %q
  timeout: 5s
  instanceId: openbao-ci-a
auth:
  method: jwt
  mountPath: %q
  role: %q
  jwtFile: %q
  minJwtRemainingTtl: 2m
  clockSkewLeeway: 30s
  loginBeforeTokenExpiry: 30s
  tokenRenewalIncrement: 1h
  loginTimeout: 0s
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
bootstrap:
  graceTimeout: 60s
  retryInterval: 5s
status:
  probeInterval: 1s
  deepProbeInterval: 30s
  statusMaxStaleness: 1m
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
`, kindProviderSocketPath,
		environment.ContainerAddress(),
		kindProviderCAPath,
		environment.TLSServerName,
		environment.AuthMount,
		environment.AuthRole,
		kindProviderJWTPath,
		environment.TransitMount,
		environment.TransitKey,
		kindProviderStatePath,
	)
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write Kind provider config: %v", err)
	}
}

func writeKindEncryptionConfig(t *testing.T, path string) {
	t.Helper()

	raw := fmt.Sprintf(`apiVersion: apiserver.config.k8s.io/v1
kind: EncryptionConfiguration
resources:
  - resources:
      - secrets
    providers:
      - kms:
          apiVersion: v2
          name: openbao-kms-workload-a
          endpoint: unix://%s
          timeout: 3s
      - identity: {}
`, kindProviderSocketPath)
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write Kind encryption config: %v", err)
	}
}

func writeKindProviderStaticPod(t *testing.T, path string, providerImage string) {
	t.Helper()

	raw := fmt.Sprintf(`apiVersion: v1
kind: Pod
metadata:
  name: bao-kms-provider
  namespace: kube-system
  labels:
    app.kubernetes.io/name: bao-kms-provider
    app.kubernetes.io/component: kms-provider
spec:
  hostNetwork: true
  priorityClassName: system-node-critical
  automountServiceAccountToken: false
  securityContext:
    runAsNonRoot: true
    runAsUser: 65532
    runAsGroup: 65532
    supplementalGroups:
      - 1234
    seccompProfile:
      type: RuntimeDefault
  containers:
    - name: bao-kms-provider
      image: %q
      imagePullPolicy: IfNotPresent
      args:
        - serve
        - --config=%s
      securityContext:
        allowPrivilegeEscalation: false
        readOnlyRootFilesystem: true
        capabilities:
          drop:
            - ALL
      volumeMounts:
        - name: config
          mountPath: %s
          readOnly: true
        - name: tls
          mountPath: /etc/openbao-kms/tls
          readOnly: true
        - name: jwt
          mountPath: %s
          readOnly: true
        - name: run
          mountPath: /run/openbao-kms
        - name: state
          mountPath: /var/lib/openbao-kms/state
  volumes:
    - name: config
      hostPath:
        path: %s
        type: File
    - name: tls
      hostPath:
        path: /etc/openbao-kms/tls
        type: Directory
    - name: jwt
      hostPath:
        path: %s
        type: File
    - name: run
      hostPath:
        path: /run/openbao-kms
        type: Directory
    - name: state
      hostPath:
        path: /var/lib/openbao-kms/state
        type: Directory
`, providerImage,
		kindProviderConfigPath,
		kindProviderConfigPath,
		kindProviderJWTPath,
		kindProviderConfigPath,
		kindProviderJWTPath,
	)
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write Kind provider static pod: %v", err)
	}
}

func dockerCopy(t *testing.T, ctx context.Context, dockerPath string, source string, target string) {
	t.Helper()

	output, err := runDockerOutput(ctx, dockerPath, "cp", source, target)
	if err != nil {
		t.Fatalf("docker cp %s: %v: %s", target, err, strings.TrimSpace(output))
	}
}

func runOutput(ctx context.Context, binary string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, binary, args...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func deleteKindCluster(t *testing.T, ctx context.Context, kindPath string, clusterName string) {
	t.Helper()

	output, err := runOutput(ctx, kindPath, "delete", "cluster", "--name", clusterName)
	if err != nil && !strings.Contains(output, "no nodes found") {
		t.Errorf("delete Kind cluster %s: %v: %s", clusterName, err, strings.TrimSpace(output))
	}
}

func strconvTime(value time.Time) string {
	return fmt.Sprintf("%d", value.UnixNano())
}

func kindContainerStatus(ctx context.Context, dockerPath string, nodeName string, namePattern string) string {
	output, err := runDockerOutput(ctx, dockerPath, "exec", nodeName, "crictl", "ps", "-a", "--name", namePattern)
	if err != nil {
		return strings.TrimSpace(output)
	}
	return strings.TrimSpace(output)
}

func kindContainerLogs(ctx context.Context, dockerPath string, nodeName string, namePattern string) string {
	script := fmt.Sprintf(`set -eu
cid="$(crictl ps -a --name %q -q | head -n1)"
if [ -n "$cid" ]; then crictl logs "$cid" 2>&1 | tail -200; fi
`, namePattern)
	output, err := runDockerOutput(ctx, dockerPath, "exec", nodeName, "sh", "-c", script)
	if err != nil {
		return strings.TrimSpace(output)
	}
	return strings.TrimSpace(output)
}

func kindFile(ctx context.Context, dockerPath string, nodeName string, path string) string {
	output, err := runDockerOutput(ctx, dockerPath, "exec", nodeName, "cat", path)
	if err != nil {
		return strings.TrimSpace(output)
	}
	return strings.TrimSpace(output)
}
