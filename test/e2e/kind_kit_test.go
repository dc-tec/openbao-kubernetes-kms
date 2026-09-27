//go:build e2e

package e2e

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/cli"
	"github.com/dc-tec/openbao-kubernetes-kms/test/e2e/framework"
)

const kindKitDir = "/root/kms-install-kit"

func TestKindGeneratedKitAcceptanceE2E(t *testing.T) {
	if !kindCIEnabled() {
		t.Skip(envKindCI + "=true is required")
	}
	providerImage := requireProviderImageFromEnv(t, envProviderImage)
	nodeImage := requireProviderImageFromEnv(t, envKindNodeImage)
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Minute)
	defer cancel()
	docker := requireDocker(t, ctx)
	kind := requireToolOrSkip(t, ctx, framework.EnvDefault(envKindBinary, "kind"))
	kubectl := requireToolOrSkip(t, ctx, framework.EnvDefault(envKubectlBinary, "kubectl"))
	cluster := fmt.Sprintf("obk-kit-%d", time.Now().UnixNano())
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "kubeconfig"))
	t.Cleanup(func() { deleteKindCluster(t, context.Background(), kind, cluster) })
	createKindMultiControlPlaneCluster(t, ctx, kind, cluster, nodeImage, kindConvergenceControlPlaneCount)
	bao := startKindOpenBao(t, ctx)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if err := bao.Close(cleanupCtx); err != nil {
			t.Errorf("close kit OpenBao environment: %v", err)
		}
	})
	loadProviderImageIntoKind(t, ctx, kind, cluster, providerImage)
	nodes := kindControlPlaneNodeNames(cluster, kindConvergenceControlPlaneCount)
	pinned := pinKindProviderImage(t, ctx, docker, nodes[0], providerImage)
	archive := buildKindInstallationKit(t, ctx, docker, providerImage, pinned)
	resolvedValues := filepath.Join(t.TempDir(), "resolved.yaml")
	sharedIdentity, activeKey := "", ""
	for index, node := range nodes {
		if index > 0 && pinKindProviderImage(t, ctx, docker, node, providerImage) != pinned {
			t.Fatal("nodes loaded different provider images")
		}
		identity, key := installKindKitNode(t, ctx, docker, node, archive, pinned, resolvedValues, index, bao, activeKey)
		if index == 0 {
			sharedIdentity, activeKey = identity, key
		} else if identity != sharedIdentity || key != activeKey {
			t.Fatal("generated nodes do not share the installation identity and active key")
		}
	}
	// All nodes receive the generated reader before any node receives a writer.
	for _, node := range nodes {
		enableKindAPIServerKMS(t, ctx, docker, kubectl, "kind-"+cluster, node)
		waitKindDirectAPI(t, ctx, docker, node)
	}
	t.Log("all three API servers are ready with the generated reader configuration")
	verifyKindKitWriterRollout(t, ctx, docker, kubectl, "kind-"+cluster, nodes)
}

func buildKindInstallationKit(t *testing.T, ctx context.Context, docker, image, pinned string) string {
	t.Helper()
	dir := t.TempDir()
	binary := filepath.Join(dir, "bao-kms-provider")
	container, err := runDockerOutput(ctx, docker, "create", image)
	if err != nil {
		t.Fatalf("create binary extraction container: %v", err)
	}
	container = strings.TrimSpace(container)
	defer runDocker(t, context.Background(), docker, "rm", container)
	dockerCopy(t, ctx, docker, container+":/bao-kms-provider", binary)
	archive := filepath.Join(dir, "static-pod-kit.tar.gz")
	// #nosec G204 -- fixed repository build tool with test-owned input and output paths.
	cmd := exec.CommandContext(ctx, "go", "run", "./hack/tools/release_bundle", "-kind", "static-pod",
		"-prefix", "kit", "-binary", binary, "-output", archive, "-image-ref", pinned)
	cmd.Dir = findRepoRoot(t)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build static-pod kit: %v: %s", err, output)
	}
	return archive
}

func installKindKitNode(
	t *testing.T, ctx context.Context, docker, node, archive, image, resolvedValues string,
	index int, bao *framework.OpenBaoEnvironment, expectedKey string,
) (string, string) {
	t.Helper()
	gid := strconv.Itoa(17000 + index)
	runDocker(t, ctx, docker, "exec", node, "groupadd", "--system", "--gid", gid, "openbao-kms-socket")
	runDocker(t, ctx, docker, "exec", node, "mkdir", "-p", kindKitDir, kindEncryptionConfigDir)
	// Joined nodes need not retain kubeadm's bootstrap client files. Reuse the
	// cluster creator's private client configuration, with a direct endpoint below.
	dockerCopy(t, ctx, docker, os.Getenv("KUBECONFIG"), node+":"+kindKitDir+"/api-client.conf")
	dockerCopy(t, ctx, docker, archive, node+":"+kindKitDir+"/kit.tar.gz")
	runDocker(t, ctx, docker, "exec", node, "tar", "-xzf", kindKitDir+"/kit.tar.gz",
		"-C", kindKitDir, "--strip-components=1")
	runDocker(t, ctx, docker, "exec", node, "sh", "-c", kindKitPrepareScript)
	values := kindKitDir + "/values.yaml"
	if index == 0 {
		sample := kindFile(ctx, docker, node, kindKitDir+"/config/init-values-file.yaml")
		replacer := strings.NewReplacer("https://bao.example.internal:8200", bao.ContainerAddress(),
			"tlsServerName: bao.example.internal", "tlsServerName: "+bao.TLSServerName,
			"mountPath: transit", "mountPath: "+bao.TransitMount,
			"keyName: k8s-workload-a-etcd", "keyName: "+bao.TransitKey)
		if err := os.WriteFile(resolvedValues, []byte(replacer.Replace(sample)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	dockerCopy(t, ctx, docker, resolvedValues, node+":"+values)
	args := []string{
		"exec", node, "bao-kms-provider", "init", "--values", values,
		"--out", kindKitDir + "/generated", "--model", "static-pod", "--socket-gid", gid, "--image", image,
	}
	if index == 0 {
		args = append(args, "--new-key")
	}
	runDocker(t, ctx, docker, args...)
	if index == 0 {
		dockerCopy(t, ctx, docker, node+":"+kindKitDir+"/generated/config.yaml", resolvedValues)
		policy := kindFile(ctx, docker, node, kindKitDir+"/generated/openbao-policy.hcl")
		if err := bao.InstallProviderPolicy(ctx, policy); err != nil {
			t.Fatalf("install generated provider policy: %v", err)
		}
	}
	dockerCopy(t, ctx, docker, bao.CACertFile, node+":"+kindProviderCAPath)
	dockerCopy(t, ctx, docker, bao.JWTFile, node+":"+kindProviderJWTPath)
	runDocker(t, ctx, docker, "exec", node, "sh", "-c", kindKitStageScript)
	doctor := runKindKitDiagnostic(t, ctx, docker, node, gid, "doctor", "--config", kindProviderConfigPath)
	for _, id := range []string{"config.validate", "jwt.local", "openbao.auth", "transit.probe", "kms.status_encrypt"} {
		requireKitCheck(t, doctor, id)
	}
	runDocker(t, ctx, docker, "exec", node, "install", "-m", "0644",
		kindKitDir+"/generated/bao-kms-provider.yaml", kindProviderStaticPodPath)
	waitForKindProviderSocket(t, ctx, docker, node)
	waitKindOAuthCondition(t, ctx, "kit provider readiness", func() bool {
		_, err := runDockerOutput(ctx, docker, "exec", node, "curl", "--fail", "--silent", "http://127.0.0.1:8082/ready")
		return err == nil
	})
	probe := runKindKitDiagnostic(t, ctx, docker, node, gid, "probe", "--expected-key-id", expectedKey)
	for _, id := range []string{"client.identity", "socket.path", "kms.status", "kms.encrypt", "kms.decrypt"} {
		requireKitCheck(t, probe, id)
	}
	var record struct {
		Fingerprint string `json:"identityFingerprint"`
		Lineage     string `json:"keyLineageId"`
		SocketGroup string `json:"socketGroup"`
	}
	raw := kindFile(ctx, docker, node, kindKitDir+"/generated/installation.json")
	if err := json.Unmarshal([]byte(raw), &record); err != nil || record.SocketGroup != gid || record.Fingerprint == "" {
		t.Fatal("invalid generated installation record or node socket group")
	}
	t.Logf("kit node %s: uid=65532 gid=65532 socket_gid=%s identity=%s", node, gid, record.Fingerprint)
	return record.Fingerprint + ":" + record.Lineage, requireKitCheck(t, probe, "kms.key_id")
}

const kindKitPrepareScript = `set -eu
cd /root/kms-install-kit
awk '
  $0 == "<!-- static-pod-kit-install -->" { found = 1; next }
  found && $0 == "` + "```sh" + `" { code = 1; next }
  code && $0 == "` + "```" + `" { done = 1; exit }
  code { print }
  END { if (!done) exit 1 }
' README.md > install.sh
sh install.sh
test ! -e /etc/kubernetes/manifests/bao-kms-provider.yaml
`

const kindKitStageScript = `set -eu
install -m 0640 -o root -g 65532 /root/kms-install-kit/generated/config.yaml /etc/openbao-kms/config.yaml
chown root:65532 /var/lib/openbao-kms/credentials/identity.jwt
chmod 0640 /var/lib/openbao-kms/credentials/identity.jwt
chmod 0644 /etc/openbao-kms/tls/ca.crt
install -m 0644 /root/kms-install-kit/generated/encryption-config-readers.yaml \
  /etc/kubernetes/encryption/openbao-kms/encryption-config.yaml
`

func runKindKitDiagnostic(t *testing.T, ctx context.Context, docker, node, gid string, args ...string) cli.Report {
	t.Helper()
	command := make([]string, 0, 8+len(args))
	command = append(command, "exec", node, "setpriv", "--reuid=65532", "--regid=65532", "--groups="+gid,
		"bao-kms-provider")
	command = append(command, args...)
	command = append(command, "--output=json")
	output, err := runDockerOutput(ctx, docker, command...)
	if err != nil {
		t.Fatalf("kit diagnostic on %s: %v: %s", node, err, output)
	}
	var report cli.Report
	if err := json.Unmarshal([]byte(output), &report); err != nil || report.HasFailures() {
		t.Fatalf("invalid or failed kit diagnostic on %s: %v", node, err)
	}
	return report
}

func requireKitCheck(t *testing.T, report cli.Report, id string) string {
	t.Helper()
	for _, check := range report.Checks {
		if check.ID == id && check.Status == cli.CheckPass {
			return check.Message
		}
	}
	t.Fatalf("kit %s did not pass %s", report.Name, id)
	return ""
}

func waitKindDirectAPI(t *testing.T, ctx context.Context, docker, node string) {
	t.Helper()
	deadline, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	for {
		output, err := runDockerOutput(deadline, docker, "exec", node, "kubectl",
			"--kubeconfig="+kindKitDir+"/api-client.conf",
			"--server=https://127.0.0.1:6443", "--request-timeout=5s", "get", "--raw=/readyz")
		if err == nil {
			return
		}
		select {
		case <-deadline.Done():
			t.Fatalf("direct API readiness on %s failed: %v: %s", node, err, strings.TrimSpace(output))
		case <-time.After(time.Second):
		}
	}
}

func verifyKindKitWriterRollout(
	t *testing.T, ctx context.Context, docker, kubectl, contextName string, nodes []string,
) {
	t.Helper()
	values := make([]string, 0, len(nodes))
	for index, writer := range nodes {
		runDocker(t, ctx, docker, "exec", writer, "install", "-m", "0644",
			kindKitDir+"/generated/encryption-config.yaml", kindEncryptionConfigPath)
		restartKindAPIServer(t, ctx, docker, kubectl, contextName, writer)
		waitKindDirectAPI(t, ctx, docker, writer)
		value := "kit-acceptance-" + strconvTime(time.Now())
		values = append(values, value)
		name := fmt.Sprintf("kit-writer-%d", index)
		valueFile := filepath.Join(t.TempDir(), "value")
		if err := os.WriteFile(valueFile, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
		dockerCopy(t, ctx, docker, valueFile, writer+":"+kindKitDir+"/value")
		runDocker(t, ctx, docker, "exec", writer, "kubectl", "--kubeconfig="+kindKitDir+"/api-client.conf",
			"--server=https://127.0.0.1:6443", "create", "secret", "generic", name, "--from-file=value="+kindKitDir+"/value")
		assertKindEtcdEncryptedNamed(t, ctx, docker, writer, name, value)
		for _, reader := range nodes {
			assertKindKitDirectRead(t, ctx, docker, reader, name, value)
		}
		t.Logf("writer %s: stored KMS envelope and direct reads through all three API servers passed", writer)
	}
	for _, node := range nodes {
		restartKindAPIServer(t, ctx, docker, kubectl, contextName, node)
		waitKindDirectAPI(t, ctx, docker, node)
		for index, value := range values {
			assertKindKitDirectRead(t, ctx, docker, node, fmt.Sprintf("kit-writer-%d", index), value)
		}
		t.Logf("API server %s: cold reads of all three Secrets passed", node)
	}
}

func assertKindKitDirectRead(t *testing.T, ctx context.Context, docker, node, name, value string) {
	t.Helper()
	output, err := runDockerOutput(ctx, docker, "exec", node, "kubectl", "--kubeconfig="+kindKitDir+"/api-client.conf",
		"--server=https://127.0.0.1:6443", "get", "secret", name, "-o", "jsonpath={.data.value}")
	if err != nil {
		t.Fatalf("direct Secret read through %s failed: %v", node, err)
	}
	decoded, err := base64.StdEncoding.DecodeString(output)
	if err != nil || string(decoded) != value {
		t.Fatalf("direct Secret read through %s returned unexpected bytes", node)
	}
}
