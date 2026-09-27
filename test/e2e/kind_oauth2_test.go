//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/config"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/oauth2"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/scaffold"
	"github.com/dc-tec/openbao-kubernetes-kms/test/e2e/framework"
)

const (
	kindOAuthSecretPath = "/etc/openbao-kms/credentials/client-secret"
	kindOAuthCAPath     = "/etc/openbao-kms/tls/issuer-ca.pem"
	kindKMSClientPath   = "/usr/local/bin/kms-e2e-client"
	kindKMSSamplePath   = "/kms-sample/encrypted.json"
	kindOAuthHoldDir    = "/etc/kubernetes/oauth-e2e-hold"
)

func TestKindOAuth2KeycloakE2E(t *testing.T) {
	if !kindCIEnabled() {
		t.Skip(envKindCI + "=true is required")
	}
	image := os.Getenv(envProviderImage)
	nodeImage := os.Getenv(envKindNodeImage)
	if image == "" || nodeImage == "" {
		t.Fatal("provider and Kind node images are required")
	}
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "kubeconfig"))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	docker := requireToolOrSkip(t, ctx, framework.EnvDefault(framework.EnvDockerBinary, "docker"))
	kind := requireToolOrSkip(t, ctx, framework.EnvDefault(envKindBinary, "kind"))
	kubectl := requireToolOrSkip(t, ctx, framework.EnvDefault(envKubectlBinary, "kubectl"))
	cluster := fmt.Sprintf("obk-oauth-%d", time.Now().UnixNano())
	node := cluster + kindControlPlaneNodeSuffix
	kubeContext := "kind-" + cluster
	t.Cleanup(func() { deleteKindCluster(t, context.Background(), kind, cluster) })
	t.Log("create isolated control plane and external HTTPS Keycloak issuer")
	createKindCluster(t, ctx, kind, cluster, nodeImage)
	issuer, err := framework.StartKeycloakEnvironment(ctx, "kind")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 30*time.Second)
		defer done()
		if err := issuer.Close(cleanup); err != nil {
			t.Error(err)
		}
	})
	bao := startKindOpenBaoWithConfig(t, ctx, framework.OpenBaoEnvironmentConfig{
		JWTIssuer: issuer.Issuer, JWTAudience: issuer.Audience, JWTSubject: issuer.Subject,
		JWTTokenTTL: "30s", JWTMaxTTL: "30s",
	})
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 20*time.Second)
		defer done()
		if err := bao.Close(cleanup); err != nil {
			t.Error(err)
		}
	})
	ca, err := os.ReadFile(issuer.CACertFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := bao.UseOIDCDiscovery(ctx, string(ca)); err != nil {
		t.Fatal(err)
	}
	loadProviderImageIntoKind(t, ctx, kind, cluster, image)
	pinnedImage := pinKindProviderImage(t, ctx, docker, node, image)
	cfg := stageKindOAuthProvider(t, ctx, docker, node, pinnedImage, bao, issuer)
	waitKindOAuthReady(t, ctx, docker, node)
	providerID := kindProviderContainerID(t, ctx, docker, node)

	t.Log("client_secret_basic: KMS v2 round trip and Kubernetes Secret encrypted in etcd")
	runKindKMSClient(t, ctx, docker, node, kmsClientModeWriteSample)
	enableKindAPIServerKMS(t, ctx, docker, kubectl, kubeContext, node)
	secretValue := "oauth-kind-secret-" + strconvTime(time.Now())
	createKindSecret(t, ctx, kubectl, kubeContext, secretValue)
	assertKindSecretReadable(t, ctx, kubectl, kubeContext, secretValue)
	assertKindEtcdEncrypted(t, ctx, docker, node, secretValue)

	t.Log("reject retired client credential after OpenBao token expiry; atomically rotate without restarting provider")
	oldSecret := issuer.ClientSecret
	newSecret, err := issuer.RotateClientSecret(ctx)
	if err != nil {
		t.Fatal(err)
	}
	runKindKMSClient(t, ctx, docker, node, kmsClientModeExpectAuthFailure)
	waitKindOAuthLog(t, ctx, docker, node, "oauth2_rejected")
	replaceKindOAuthSecret(t, ctx, docker, node, newSecret)
	runKindKMSClient(t, ctx, docker, node, kmsClientModeReadSample)
	if kindProviderContainerID(t, ctx, docker, node) != providerID {
		t.Fatal("credential rotation restarted provider")
	}

	t.Log("issuer outage fails closed after current OpenBao token expires")
	if err := issuer.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	runKindKMSClient(t, ctx, docker, node, kmsClientModeExpectAuthFailure)
	assertKindOAuthLogsRedacted(t, ctx, docker, node, oldSecret, newSecret, secretValue)
	t.Log("cold provider startup with protected API stopped; no JWT file or Kubernetes credential source")
	holdKindOAuthComponent(t, ctx, docker, node, "kube-apiserver")
	holdKindOAuthComponent(t, ctx, docker, node, "bao-kms-provider")
	cfg.Auth.JWT.OAuth2.AuthMethod = oauth2.ClientSecretPost
	writeKindOAuthConfig(t, ctx, docker, node, cfg)
	restoreKindOAuthComponent(t, ctx, docker, node, "bao-kms-provider")
	waitForKindProviderContainerRestart(t, ctx, docker, node, providerID)
	waitKindOAuthLog(t, ctx, docker, node, "oauth2_request")
	runKindKMSClient(t, ctx, docker, node, kmsClientModeExpectSocketUnavailable)

	t.Log("client_secret_post: recover provider before restoring API, then decrypt existing etcd data")
	if err := issuer.Start(ctx); err != nil {
		t.Fatal(err)
	}
	waitKindOAuthReady(t, ctx, docker, node)
	runKindKMSClient(t, ctx, docker, node, kmsClientModeReadSample)
	output, err := runDockerOutput(ctx, docker, "exec", node, "crictl", "ps", "--name", "^kube-apiserver$", "-q")
	if err != nil || strings.TrimSpace(output) != "" {
		t.Fatal("protected API was running during cold provider recovery")
	}
	runDocker(t, ctx, docker, "exec", node, "test", "!", "-e", kindProviderJWTPath)
	restoreKindOAuthComponent(t, ctx, docker, node, "kube-apiserver")
	waitForKindAPIServer(t, ctx, kubectl, kubeContext)
	assertKindSecretReadable(t, ctx, kubectl, kubeContext, secretValue)
	createKindSecretNamed(t, ctx, kubectl, kubeContext, "oauth-after-recovery", secretValue)
	assertKindEtcdEncryptedNamed(t, ctx, docker, node, "oauth-after-recovery", secretValue)
	assertKindOAuthLogsRedacted(t, ctx, docker, node, oldSecret, newSecret, secretValue)
}

func assertKindOAuthLogsRedacted(t *testing.T, ctx context.Context, docker, node string, secrets ...string) {
	t.Helper()
	logs := kindContainerLogs(ctx, docker, node, "^bao-kms-provider$")
	for _, secret := range append(secrets, "eyJ", "hvs.", "hvb.") {
		if strings.Contains(logs, secret) {
			t.Fatal("provider logs exposed a credential or plaintext")
		}
	}
}

func pinKindProviderImage(t *testing.T, ctx context.Context, docker, node, image string) string {
	t.Helper()
	output, err := runDockerOutput(ctx, docker, "exec", node, "ctr", "--namespace=k8s.io", "images", "list")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == image && strings.HasPrefix(fields[2], "sha256:") {
			repository := image[:strings.LastIndex(image, ":")]
			pinned := repository + "@" + fields[2]
			runDocker(t, ctx, docker, "exec", node, "ctr", "--namespace=k8s.io", "images", "tag", image, pinned)
			return pinned
		}
	}
	t.Fatal("loaded provider image manifest digest not found")
	return ""
}

func stageKindOAuthProvider(t *testing.T, ctx context.Context, docker, node, image string,
	bao *framework.OpenBaoEnvironment, issuer *framework.KeycloakEnvironment,
) config.Config {
	t.Helper()
	dir := t.TempDir()
	base := filepath.Join(dir, "base.yaml")
	writeKindProviderConfig(t, base, bao)
	cfg, err := config.Load(config.NewRuntime(), config.LoadOptions{Path: base})
	if err != nil {
		t.Fatal(err)
	}
	cfg.Server.HealthAddress = "127.0.0.1:8081"
	cfg.Server.MetricsAddress = "127.0.0.1:8080"
	cfg.Auth.LoginTimeout = 5 * time.Second
	cfg.Auth.LoginBeforeTokenExpiry = 10 * time.Second
	cfg.Auth.JWT.Source = config.JWTSourceOAuth2
	cfg.Auth.JWT.JWTFile = ""
	cfg.Auth.JWT.ExpectedIssuer = issuer.Issuer
	cfg.Auth.JWT.ExpectedAudience = []string{issuer.Audience}
	cfg.Auth.JWT.ExpectedSubject = issuer.Subject
	cfg.Auth.JWT.OAuth2 = config.OAuth2Config{
		TokenURL: issuer.TokenURL, ClientID: issuer.ClientID, ClientSecretFile: kindOAuthSecretPath,
		AuthMethod: oauth2.ClientSecretBasic, CACertFile: kindOAuthCAPath,
	}
	manifest, err := scaffold.RenderStaticPod(cfg, scaffold.StaticPodOptions{Image: image, SocketGID: 1234})
	if err != nil {
		t.Fatal(err)
	}
	runDocker(t, ctx, docker, "exec", node, "mkdir", "-p", "/etc/openbao-kms/tls", "/etc/openbao-kms/credentials",
		"/run/openbao-kms", "/var/lib/openbao-kms/state", "/kms-sample", kindEncryptionConfigDir, kindOAuthHoldDir)
	writeKindOAuthConfig(t, ctx, docker, node, cfg)
	dockerCopy(t, ctx, docker, bao.CACertFile, node+":"+kindProviderCAPath)
	dockerCopy(t, ctx, docker, issuer.CACertFile, node+":"+kindOAuthCAPath)
	replaceKindOAuthSecret(t, ctx, docker, node, issuer.ClientSecret)
	writeKindEncryptionConfig(t, filepath.Join(dir, "encryption.yaml"))
	dockerCopy(t, ctx, docker, filepath.Join(dir, "encryption.yaml"), node+":"+kindEncryptionConfigPath)
	client := filepath.Join(dir, "kms-client")
	buildKMSClient(t, ctx, client)
	dockerCopy(t, ctx, docker, client, node+":"+kindKMSClientPath)
	runDocker(t, ctx, docker, "exec", node, "sh", "-c", `set -eu
chown -R 65532:65532 /etc/openbao-kms /var/lib/openbao-kms
chmod 0700 /etc/openbao-kms /etc/openbao-kms/credentials /var/lib/openbao-kms /var/lib/openbao-kms/state
chown 65532:1234 /run/openbao-kms
chmod 2750 /run/openbao-kms
chmod 0644 /etc/kubernetes/encryption/openbao-kms/encryption-config.yaml
chmod 0755 /usr/local/bin/kms-e2e-client`)
	manifestPath := filepath.Join(dir, "pod.yaml")
	if err := os.WriteFile(manifestPath, manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	dockerCopy(t, ctx, docker, manifestPath, node+":"+kindProviderStaticPodPath)
	return cfg
}

func writeKindOAuthConfig(t *testing.T, ctx context.Context, docker, node string, cfg config.Config) {
	t.Helper()
	if err := config.Validate(cfg, config.ValidationOptions{}); err != nil {
		t.Fatal(err)
	}
	raw, err := scaffold.RenderProviderConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	dockerCopy(t, ctx, docker, path, node+":"+kindProviderConfigPath)
	runDocker(t, ctx, docker, "exec", node, "chown", "65532:65532", kindProviderConfigPath)
}

func replaceKindOAuthSecret(t *testing.T, ctx context.Context, docker, node, secret string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "credential")
	if err := os.WriteFile(path, []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}
	dockerCopy(t, ctx, docker, path, node+":"+kindOAuthSecretPath+".new")
	runDocker(t, ctx, docker, "exec", node, "sh", "-c", `set -eu
chown 65532:65532 /etc/openbao-kms/credentials/client-secret.new
chmod 0600 /etc/openbao-kms/credentials/client-secret.new
mv /etc/openbao-kms/credentials/client-secret.new /etc/openbao-kms/credentials/client-secret`)
}

func runKindKMSClient(t *testing.T, ctx context.Context, docker, node, mode string) {
	t.Helper()
	runDocker(t, ctx, docker, "exec", node, "env", "KMS_SOCKET_PATH="+kindProviderSocketPath,
		kmsClientModeEnv+"="+mode, kmsSamplePathEnv+"="+kindKMSSamplePath, kindKMSClientPath)
}

func waitKindOAuthReady(t *testing.T, ctx context.Context, docker, node string) {
	t.Helper()
	waitKindOAuthCondition(t, ctx, "provider readiness", func() bool {
		_, err := runDockerOutput(ctx, docker, "exec", node, "curl", "--fail", "--silent", "--max-time", "2", "http://127.0.0.1:8081/ready")
		return err == nil
	})
}

func waitKindOAuthLog(t *testing.T, ctx context.Context, docker, node, marker string) {
	t.Helper()
	waitKindOAuthCondition(t, ctx, marker, func() bool {
		return strings.Contains(kindContainerLogs(ctx, docker, node, "^bao-kms-provider$"), marker)
	})
}

func waitKindOAuthCondition(t *testing.T, ctx context.Context, description string, check func() bool) {
	t.Helper()
	deadline, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	for {
		if check() {
			return
		}
		select {
		case <-deadline.Done():
			t.Fatalf("timed out waiting for %s", description)
		case <-time.After(time.Second):
		}
	}
}

func holdKindOAuthComponent(t *testing.T, ctx context.Context, docker, node, component string) {
	t.Helper()
	// Hold outside the watched directory: kubelet also reads files with .hold suffixes.
	runDocker(t, ctx, docker, "exec", node, "mv", "/etc/kubernetes/manifests/"+component+".yaml", kindOAuthHoldDir+"/"+component+".yaml")
	waitKindOAuthCondition(t, ctx, component+" stopped", func() bool {
		output, err := runDockerOutput(ctx, docker, "exec", node, "crictl", "ps", "--name", "^"+component+"$", "-q")
		return err == nil && strings.TrimSpace(output) == ""
	})
}

func restoreKindOAuthComponent(t *testing.T, ctx context.Context, docker, node, component string) {
	t.Helper()
	runDocker(t, ctx, docker, "exec", node, "mv", kindOAuthHoldDir+"/"+component+".yaml", "/etc/kubernetes/manifests/"+component+".yaml")
}
