package scaffold

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/config"
)

const nodeTestFingerprint = "cfg1.node-setup-test"

func systemdNodeOptions() NodeSetupOptions {
	return NodeSetupOptions{Model: NodeModelSystemd, Fingerprint: nodeTestFingerprint, ConfigFile: "config.yaml"}
}

func staticPodNodeOptions() NodeSetupOptions {
	return NodeSetupOptions{
		Model: NodeModelStaticPod, Fingerprint: nodeTestFingerprint, ConfigFile: "config.yaml",
		ManifestFile: "bao-kms-provider.yaml", Image: sampleImage, SocketGID: 1234,
	}
}

func renderNodeScript(t *testing.T, cfg config.Config, opts NodeSetupOptions) (string, string) {
	t.Helper()
	script, err := RenderNodeSetup(cfg, opts)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "node-setup.sh")
	if err := os.WriteFile(path, script, 0o600); err != nil {
		t.Fatal(err)
	}
	// #nosec G204 -- syntax check of the generated test script only.
	if out, err := exec.Command("sh", "-n", path).CombinedOutput(); err != nil {
		t.Fatalf("generated script is not valid sh: %v: %s\n%s", err, out, script)
	}
	return string(script), path
}

func requireScriptLines(t *testing.T, script string, lines ...string) {
	t.Helper()
	for _, line := range lines {
		if !strings.Contains(script, line) {
			t.Fatalf("script is missing %q:\n%s", line, script)
		}
	}
}

func TestRenderNodeSetupSystemdUsesPackageIdentity(t *testing.T) {
	cfg := loadConfig(t, systemdSamplePath)
	script, _ := renderNodeScript(t, cfg, systemdNodeOptions())
	requireScriptLines(t, script,
		`as_runtime() { setpriv --reuid=openbao-kms --regid=openbao-kms --init-groups -- "$@"; }`,
		`ensure_dir '`+filepath.Dir(cfg.OpenBao.CACertFile)+`' 'root' 'root' 755`,
		`ensure_dir '`+filepath.Dir(cfg.Auth.JWT.JWTFile)+`' 'root' 'openbao-kms' 750`,
		`install_new "$here"/'config.yaml' "$config_target" root 'openbao-kms' 640`,
		`install_new "$file0" '`+cfg.OpenBao.CACertFile+`' 'root' 'root' 644`,
		`install_new "$file1" '`+cfg.Auth.JWT.JWTFile+`' 'root' 'openbao-kms' 640`,
		`systemctl enable --now bao-kms-provider.service`,
		`fingerprint='`+nodeTestFingerprint+`'`,
	)
	for _, unwanted := range []string{"crictl", "--issuer-ca", "/etc/kubernetes/manifests"} {
		if strings.Contains(script, unwanted) {
			t.Fatalf("systemd script contains %q", unwanted)
		}
	}
}

func TestRenderNodeSetupStaticPodOAuth(t *testing.T) {
	cfg := loadConfig(t, staticPodSamplePath)
	cfg.Auth.JWT.Source = config.JWTSourceOAuth2
	cfg.Auth.JWT.JWTFile = ""
	// #nosec G101 -- fixture configuration contains credential paths, not secrets.
	cfg.Auth.JWT.OAuth2 = config.OAuth2Config{
		TokenURL: "https://issuer.example/token", ClientID: "provider", AuthMethod: "client_secret_basic",
		ClientSecretFile: "/etc/openbao-kms/credentials/client-secret", CACertFile: "/etc/openbao-kms/issuer/ca.pem",
	}
	script, _ := renderNodeScript(t, cfg, staticPodNodeOptions())
	requireScriptLines(t, script,
		`as_runtime() { setpriv --reuid=65532 --regid=65532 --groups=1234 -- "$@"; }`,
		`[ "$gid" = '1234' ]`,
		`line='d /run/openbao-kms 2750 65532 1234 -'`,
		`ensure_dir '/run/openbao-kms' 65532 1234 2750 strict`,
		`ensure_dir '/var/lib/openbao-kms/state' '65532' '65532' 750 strict`,
		`ensure_dir '/etc/openbao-kms/credentials' 'root' '65532' 750`,
		`ensure_dir '/etc/openbao-kms/issuer' 'root' 'root' 755`,
		`install_new "$file1" '/etc/openbao-kms/credentials/client-secret' 'root' '65532' 640`,
		`install_new "$file2" '/etc/openbao-kms/issuer/ca.pem' 'root' 'root' 644`,
		`--issuer-ca) file2=$2 ;;`,
		`crictl inspecti '`+sampleImage+`'`,
		`manifest='/etc/kubernetes/manifests/bao-kms-provider.yaml'`,
		`install -o root -g root -m 644 -- "$here"/'bao-kms-provider.yaml' "$manifest"`,
	)
}

func TestRenderNodeSetupRejectsUnsupportedInput(t *testing.T) {
	for name, tc := range map[string]struct {
		sample string
		opts   func() NodeSetupOptions
		mutate func(*config.Config)
	}{
		"systemd socket outside unit paths": {
			systemdSamplePath, systemdNodeOptions,
			func(cfg *config.Config) { cfg.Server.SocketPath = "/run/other/kms.sock" },
		},
		"systemd state outside unit paths": {
			systemdSamplePath, systemdNodeOptions,
			func(cfg *config.Config) { cfg.State.Path = "/srv/state/key-registry.json" },
		},
		"certificate auth": {
			systemdSamplePath, systemdNodeOptions,
			func(cfg *config.Config) { cfg.Auth.Method = config.AuthMethodCert },
		},
		"missing fingerprint": {
			systemdSamplePath,
			func() NodeSetupOptions { opts := systemdNodeOptions(); opts.Fingerprint = ""; return opts },
			func(*config.Config) {},
		},
		"static pod tag image": {
			staticPodSamplePath,
			func() NodeSetupOptions {
				opts := staticPodNodeOptions()
				opts.Image = "ghcr.io/x/y:latest"
				return opts
			},
			func(*config.Config) {},
		},
		"static pod missing gid": {
			staticPodSamplePath,
			func() NodeSetupOptions { opts := staticPodNodeOptions(); opts.SocketGID = 0; return opts },
			func(*config.Config) {},
		},
		"unknown model": {
			systemdSamplePath,
			func() NodeSetupOptions { opts := systemdNodeOptions(); opts.Model = "daemonset"; return opts },
			func(*config.Config) {},
		},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := loadConfig(t, tc.sample)
			tc.mutate(&cfg)
			if _, err := RenderNodeSetup(cfg, tc.opts()); err == nil {
				t.Fatal("expected RenderNodeSetup to fail")
			}
		})
	}
}

func TestRenderNodeSetupQuotesConfigurationPaths(t *testing.T) {
	cfg := loadConfig(t, systemdSamplePath)
	cfg.OpenBao.CACertFile = "/etc/openbao-kms/tls/$(touch NEVER) it's.crt"
	script, _ := renderNodeScript(t, cfg, systemdNodeOptions())
	requireScriptLines(t, script, shellQuote(cfg.OpenBao.CACertFile))
	if strings.Contains(script, `"/etc/openbao-kms/tls/$(touch`) {
		t.Fatal("configuration path is expanded by the shell")
	}
}

func TestNodeSetupDispatchWithoutRoot(t *testing.T) {
	_, path := renderNodeScript(t, loadConfig(t, systemdSamplePath), systemdNodeOptions())
	run := func(args ...string) (int, string) {
		// #nosec G204 -- executes only the generated test script with fixed arguments.
		out, err := exec.Command("sh", append([]string{path}, args...)...).CombinedOutput()
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode(), string(out)
		}
		if err != nil {
			t.Fatal(err)
		}
		return 0, string(out)
	}
	if code, out := run(); code != 0 || !strings.Contains(out, nodeTestFingerprint) {
		t.Fatalf("listing = %d %q, want 0 with the fingerprint", code, out)
	}
	if code, _ := run("unknown"); code != 2 {
		t.Fatalf("unknown phase exit = %d, want 2", code)
	}
	if code, _ := run("prepare", "extra"); code != 2 {
		t.Fatalf("prepare with arguments exit = %d, want 2", code)
	}
	if os.Geteuid() == 0 {
		t.Skip("root checks need a non-root test user")
	}
	for _, phase := range []string{"prepare", "install", "check", "start"} {
		if code, out := run(phase); code != 3 || !strings.Contains(out, "run this phase as root") {
			t.Fatalf("%s as non-root = %d %q, want 3", phase, code, out)
		}
	}
}
