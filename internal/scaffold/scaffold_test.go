package scaffold

import (
	"bytes"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/config"
	"gopkg.in/yaml.v3"
)

const (
	systemdSamplePath   = "../../deploy/config/provider-systemd.yaml"
	staticPodSamplePath = "../../deploy/config/provider-static-pod.yaml"
	encryptionSample    = "../../deploy/kubernetes/encryption-config.yaml"
	staticPodManifest   = "../../deploy/static-pod/bao-kms-provider.yaml"
	sampleImage         = "ghcr.io/dc-tec/bao-kms-provider@sha256:" +
		"0000000000000000000000000000000000000000000000000000000000000000"
)

func loadConfig(t *testing.T, path string) config.Config {
	t.Helper()
	cfg, err := config.Load(config.NewRuntime(), config.LoadOptions{Path: path})
	if err != nil {
		t.Fatalf("load %s: %v", path, err)
	}
	return cfg
}

func loadContent(t *testing.T, content []byte) config.Config {
	t.Helper()
	cfg, err := config.Load(config.NewRuntime(), config.LoadOptions{Content: content})
	if err != nil {
		t.Fatalf("load rendered config: %v\n%s", err, content)
	}
	return cfg
}

func TestRenderProviderConfigRoundTripsJWT(t *testing.T) {
	for _, path := range []string{systemdSamplePath, staticPodSamplePath} {
		want := loadConfig(t, path)
		rendered, err := RenderProviderConfig(want)
		if err != nil {
			t.Fatalf("render %s: %v", path, err)
		}
		got := loadContent(t, rendered)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("round trip of %s changed the configuration:\ngot  %+v\nwant %+v", path, got, want)
		}
		if err := config.Validate(got, config.ValidationOptions{}); err != nil {
			t.Fatalf("rendered %s is invalid: %v", path, err)
		}
	}
}

func TestRenderProviderConfigRoundTripsPKCS11(t *testing.T) {
	want := loadConfig(t, systemdSamplePath)
	want.Auth.Method = authMethodCert
	want.Auth.JWT = config.JWTAuthConfig{
		MinRemainingTTL: want.Auth.JWT.MinRemainingTTL,
		ClockSkewLeeway: want.Auth.JWT.ClockSkewLeeway,
	}
	want.Auth.Cert.MountPath = "auth/k8s-workload-a-cert"
	want.Auth.Cert.Name = "openbao-kms-control-plane"
	want.Auth.Cert.Source = "pkcs11"
	want.Auth.Cert.PKCS11 = config.PKCS11CertAuthConfig{
		CertificateFile: "/etc/openbao-kms/client/client-chain.pem",
		ModulePath:      "/usr/lib/softhsm/libsofthsm2.so",
		TokenLabel:      "openbao-kms",
		KeyLabel:        "openbao-kms-client",
		PINFile:         "/etc/openbao-kms/pkcs11/pin", // #nosec G101 -- a file path, not a credential.
		MaxSessions:     4,
	}

	rendered, err := RenderProviderConfig(want)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if bytes.Contains(rendered, []byte("jwtFile")) {
		t.Fatalf("certificate configuration must not render a jwt section:\n%s", rendered)
	}
	got := loadContent(t, rendered)
	if !reflect.DeepEqual(got.Auth.Cert, want.Auth.Cert) || got.Auth.Method != authMethodCert {
		t.Fatalf("certificate auth changed on round trip:\ngot  %+v\nwant %+v", got.Auth, want.Auth)
	}
}

func TestFormatDuration(t *testing.T) {
	cases := map[time.Duration]string{
		0:                       "0s",
		30 * time.Second:        "30s",
		2 * time.Minute:         "2m",
		time.Hour:               "1h",
		90 * time.Second:        "90s",
		1500 * time.Millisecond: "1500ms",
	}
	for input, want := range cases {
		if got := formatDuration(input); got != want {
			t.Fatalf("formatDuration(%s) = %q, want %q", input, got, want)
		}
	}
}

func TestRenderEncryptionConfigMatchesSample(t *testing.T) {
	cfg := loadConfig(t, systemdSamplePath)
	rendered, err := RenderEncryptionConfig(cfg)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	got, err := config.ParseEncryptionConfiguration(bytes.NewReader(rendered))
	if err != nil {
		t.Fatalf("parse rendered: %v", err)
	}
	opts := config.EncryptionValidationOptions{AllowIdentityFallback: true}
	if _, err := config.ValidateEncryptionConfiguration(cfg, got, opts); err != nil {
		t.Fatalf("rendered EncryptionConfiguration does not match the config: %v", err)
	}
	want, err := config.LoadEncryptionConfiguration(encryptionSample)
	if err != nil {
		t.Fatalf("load sample: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rendered EncryptionConfiguration differs from %s:\n%s", encryptionSample, rendered)
	}
}

func TestRenderStaticPodMatchesSample(t *testing.T) {
	cfg := loadConfig(t, staticPodSamplePath)
	got, err := buildStaticPod(cfg, StaticPodOptions{Image: sampleImage, SocketGID: 1234})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	content, err := os.ReadFile(staticPodManifest)
	if err != nil {
		t.Fatalf("read sample: %v", err)
	}
	var want podManifest
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)
	if err := decoder.Decode(&want); err != nil {
		t.Fatalf("decode sample: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("generated manifest differs from %s:\ngot  %+v\nwant %+v", staticPodManifest, got, want)
	}
	if _, err := RenderStaticPod(cfg, StaticPodOptions{Image: sampleImage, SocketGID: 1234}); err != nil {
		t.Fatalf("render: %v", err)
	}
}

func TestRenderStaticPodRejectsUnsafeInput(t *testing.T) {
	cfg := loadConfig(t, staticPodSamplePath)
	tagged := StaticPodOptions{Image: "ghcr.io/dc-tec/bao-kms-provider:0.1.0", SocketGID: 1234}
	if _, err := RenderStaticPod(cfg, tagged); err == nil {
		t.Fatal("expected a tag-only image to be rejected")
	}
	if _, err := RenderStaticPod(cfg, StaticPodOptions{Image: sampleImage}); err == nil {
		t.Fatal("expected a missing socket GID to be rejected")
	}
	cfg.Auth.Method = authMethodCert
	if _, err := RenderStaticPod(cfg, StaticPodOptions{Image: sampleImage, SocketGID: 1234}); err == nil {
		t.Fatal("expected certificate auth to be rejected")
	}
}

func TestValidateImageDigest(t *testing.T) {
	valid := []string{
		sampleImage,
		"registry.example.internal:5000/kms/bao-kms-provider@sha256:" + strings.Repeat("a", 64),
	}
	for _, image := range valid {
		if err := ValidateImageDigest(image); err != nil {
			t.Fatalf("expected %q to be accepted: %v", image, err)
		}
	}
	invalid := []string{
		"ghcr.io/dc-tec/bao-kms-provider:latest",
		"ghcr.io/dc-tec/bao-kms-provider:0.1.0@sha256:" + strings.Repeat("a", 64),
		"ghcr.io/dc-tec/bao-kms-provider@sha256:short",
		"",
	}
	for _, image := range invalid {
		if err := ValidateImageDigest(image); err == nil {
			t.Fatalf("expected %q to be rejected", image)
		}
	}
}

func TestStaticPodJWTCredentialDirectory(t *testing.T) {
	for _, path := range []string{
		"/identity.jwt",
		"credentials/identity.jwt",
		"/etc/openbao-kms/identity.jwt",
		"/var/lib/openbao-kms/identity.jwt",
		"/var/lib/openbao-kms/state/identity.jwt",
		"/var/lib/openbao-kms/state/credentials/identity.jwt",
		"/run/openbao-kms/identity.jwt",
		"/run/openbao-kms/credentials/identity.jwt",
	} {
		t.Run(path, func(t *testing.T) {
			cfg := loadConfig(t, staticPodSamplePath)
			cfg.Auth.JWT.JWTFile = path
			if _, err := RenderStaticPod(cfg, StaticPodOptions{Image: sampleImage, SocketGID: 1234}); err == nil {
				t.Fatal("unsafe JWT directory accepted")
			}
		})
	}
	cfg := loadConfig(t, staticPodSamplePath)
	// A custom dedicated directory remains supported without relocating its file.
	cfg.Auth.JWT.JWTFile = "/srv/kms-credentials/identity.jwt"
	manifest, err := buildStaticPod(cfg, StaticPodOptions{Image: sampleImage, SocketGID: 1234})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, mount := range manifest.Spec.Containers[0].VolumeMounts {
		if mount.Name == "jwt" {
			found = mount.ReadOnly && mount.MountPath == "/srv/kms-credentials"
		}
	}
	if !found {
		t.Fatal("JWT directory must be mounted read-only")
	}
}

func TestRenderOpenBaoSetupQuotesValues(t *testing.T) {
	cfg := loadConfig(t, systemdSamplePath)
	cfg.OpenBao.Namespace = "admin/workload-a"
	cfg.Auth.JWT.ExpectedSubject = "system:it's-quoted"
	script, err := RenderOpenBaoSetup(cfg, SetupOptions{PolicyName: "openbao-kms-workload-a"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	text := string(script)
	for _, want := range []string{
		"export BAO_NAMESPACE='admin/workload-a'",
		"bao secrets enable -path='transit' transit",
		"bao write 'transit/keys/k8s-workload-a-etcd' type=aes256-gcm96",
		"bao auth enable -path='k8s-workload-a-jwt' jwt",
		`bound_subject='system:it'\''s-quoted'`,
		"token_policies='openbao-kms-workload-a'",
		"token_no_default_policy=true",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("setup script missing %q:\n%s", want, text)
		}
	}
}

func TestRenderOpenBaoSetupRequiresRoleBindings(t *testing.T) {
	cfg := loadConfig(t, systemdSamplePath)
	cfg.Auth.JWT.ExpectedSubject = ""
	if _, err := RenderOpenBaoSetup(cfg, SetupOptions{PolicyName: "p"}); err == nil {
		t.Fatal("expected a missing expected subject to be rejected")
	}
	cfg = loadConfig(t, systemdSamplePath)
	cfg.Auth.JWT.MountPath = "k8s-workload-a-jwt"
	if _, err := RenderOpenBaoSetup(cfg, SetupOptions{PolicyName: "p"}); err == nil {
		t.Fatal("expected a mount path without auth/ to be rejected")
	}
	if _, err := RenderOpenBaoSetup(loadConfig(t, systemdSamplePath), SetupOptions{}); err == nil {
		t.Fatal("expected an empty policy name to be rejected")
	}
}

func TestWriteOpenBaoPolicyRenewal(t *testing.T) {
	cfg := loadConfig(t, systemdSamplePath)
	var withoutRenewal, withRenewal bytes.Buffer
	if err := WriteOpenBaoPolicy(&withoutRenewal, cfg, PolicyOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := WriteOpenBaoPolicy(&withRenewal, cfg, PolicyOptions{IncludeTokenRenewal: true}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(withoutRenewal.String(), "renew-self") {
		t.Fatal("default policy must not include token renewal")
	}
	if !strings.Contains(withRenewal.String(), `path "auth/token/renew-self"`) {
		t.Fatalf("renewal policy missing renew-self:\n%s", withRenewal.String())
	}
}

func TestOAuth2ScaffoldRoundTripAndCredentialMount(t *testing.T) {
	cfg := loadConfig(t, systemdSamplePath)
	cfg.Auth.JWT.Source = config.JWTSourceOAuth2
	cfg.Auth.JWT.JWTFile = ""
	// #nosec G101 -- fixture configuration contains a credential path, not a client secret.
	cfg.Auth.JWT.OAuth2 = config.OAuth2Config{
		TokenURL: "https://issuer.example/token", ClientID: "provider", AuthMethod: "client_secret_post",
		ClientSecretFile: "/etc/openbao-kms/credentials/client-secret", CACertFile: "/etc/openbao-kms/issuer/ca.pem",
		Scopes: []string{"read", "write"}, Audience: "bao", Resources: []string{"urn:bao:production"},
	}
	rendered, err := RenderProviderConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(rendered, []byte("jwtFile:")) {
		t.Fatal("OAuth config rendered a JWT file")
	}
	got := loadContent(t, rendered)
	if !reflect.DeepEqual(got.Auth, cfg.Auth) {
		t.Fatal("OAuth configuration changed during round trip")
	}
	if err := config.Validate(got, config.ValidationOptions{}); err != nil {
		t.Fatal(err)
	}
	manifest, err := buildStaticPod(cfg, StaticPodOptions{Image: sampleImage, SocketGID: 1234})
	if err != nil {
		t.Fatal(err)
	}
	var credentialMounted bool
	for _, volume := range manifest.Spec.Volumes {
		if volume.Name == authMethodJWT {
			t.Fatal("OAuth pod depends on a JWT file")
		}
		if volume.Name == "oauth2-credentials" {
			credentialMounted = volume.HostPath.Type == "Directory" && volume.HostPath.Path == "/etc/openbao-kms/credentials"
		}
	}
	if !credentialMounted {
		t.Fatal("credential directory not mounted for atomic rotation")
	}
	for _, mount := range manifest.Spec.Containers[0].VolumeMounts {
		if strings.HasPrefix(mount.Name, "oauth2-") && !mount.ReadOnly {
			t.Fatal("issuer material mounted writable")
		}
	}
}

func TestOAuth2CredentialMountConflicts(t *testing.T) {
	for _, tc := range []struct {
		name, credential string
		wantError        bool
	}{
		{"existing TLS directory", "/etc/openbao-kms/tls/client-secret", false},
		{"writable runtime directory", "/run/openbao-kms/client-secret", true},
		{"host root", "/client-secret", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := loadConfig(t, staticPodSamplePath)
			cfg.Auth.JWT.Source = config.JWTSourceOAuth2
			cfg.Auth.JWT.JWTFile = ""
			cfg.Auth.JWT.OAuth2.ClientSecretFile = tc.credential
			manifest, err := buildStaticPod(cfg, StaticPodOptions{Image: sampleImage, SocketGID: 1234})
			if tc.wantError {
				if err == nil {
					t.Fatal("unsafe credential directory accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			mounts := map[string]bool{}
			for _, mount := range manifest.Spec.Containers[0].VolumeMounts {
				if mounts[mount.MountPath] {
					t.Fatal("duplicate container mount path")
				}
				mounts[mount.MountPath] = true
			}
		})
	}
}
