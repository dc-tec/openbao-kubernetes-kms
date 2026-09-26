package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/cli"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/config"
)

const initValues = `configVersion: v1alpha1
openbao:
  address: https://bao.example.internal:8200
  tlsServerName: bao.example.internal
  instanceId: bao-prod-a
auth:
  jwt:
    mountPath: auth/k8s-workload-a-jwt
    role: openbao-kms-control-plane
    expectedIssuer: https://issuer.example.internal
    expectedAudience:
      - bao-kms-provider
    expectedSubject: system:openbao-kms:workload-a
transit:
  mountPath: transit
  keyName: k8s-workload-a-etcd
  keyIdScope:
    providerName: openbao-kms-workload-a
    clusterId: workload-a
    transitMountId: transit-prod-primary
`

const initImage = "ghcr.io/dc-tec/bao-kms-provider@sha256:" +
	"1111111111111111111111111111111111111111111111111111111111111111"

func writeValues(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "values.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func valuesWithLineage() string {
	return initValues + "    keyLineageId: 7d34fb7df15f4e4c95d6c2a50fe90d84\n"
}

func readGenerated(t *testing.T, path string) string {
	t.Helper()
	// #nosec G304 -- tests read files they generated under t.TempDir.
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}

func requireExitCode(t *testing.T, err error, want cli.ExitCode) {
	t.Helper()
	var coded cli.ExitErrorWithCode
	if !errors.As(err, &coded) || coded.Code != want {
		t.Fatalf("expected exit code %d, got %v", want, err)
	}
}

func TestInitWritesSystemdFiles(t *testing.T) {
	values := writeValues(t, valuesWithLineage())
	out := filepath.Join(t.TempDir(), "generated")

	output, err := executeCommand(t, "init", "--values", values, "--out", out)
	if err != nil {
		t.Fatalf("init failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "identityFingerprint: cfg1.") {
		t.Fatalf("summary missing fingerprint:\n%s", output)
	}

	modes := map[string]os.FileMode{
		"config.yaml":            0o640,
		"encryption-config.yaml": 0o644,
		"openbao-policy.hcl":     0o644,
		"openbao-setup.sh":       0o750,
	}
	for name, mode := range modes {
		info, err := os.Stat(filepath.Join(out, name))
		if err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
		if info.Mode().Perm() != mode {
			t.Fatalf("%s mode = %o, want %o", name, info.Mode().Perm(), mode)
		}
	}
	if _, err := os.Stat(filepath.Join(out, "bao-kms-provider.yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("systemd model must not write a static pod manifest")
	}

	cfg, err := config.Load(config.NewRuntime(), config.LoadOptions{Path: filepath.Join(out, "config.yaml")})
	if err != nil {
		t.Fatalf("generated config does not load: %v", err)
	}
	if cfg.Server.SocketGroup != "openbao-kms-socket" ||
		cfg.OpenBao.CACertFile != "/etc/openbao-kms/tls/ca.crt" ||
		cfg.Auth.JWT.JWTFile != "/var/lib/openbao-kms/identity.jwt" {
		t.Fatalf("host layout defaults not applied: %+v", cfg)
	}
	if cfg.Transit.KeyIDScope.KeyLineageID != "7d34fb7df15f4e4c95d6c2a50fe90d84" {
		t.Fatalf("lineage ID changed: %q", cfg.Transit.KeyIDScope.KeyLineageID)
	}
	requireGeneratedPolicyAndScript(t, out)
}

func requireGeneratedPolicyAndScript(t *testing.T, out string) {
	t.Helper()
	policy := readGenerated(t, filepath.Join(out, "openbao-policy.hcl"))
	if !strings.Contains(policy, "auth/token/renew-self") {
		t.Fatalf("policy must include token renewal:\n%s", policy)
	}
	script := readGenerated(t, filepath.Join(out, "openbao-setup.sh"))
	if !strings.Contains(script, "bao policy write 'openbao-kms-workload-a'") {
		t.Fatalf("setup script missing default policy name:\n%s", script)
	}
}

func TestInitWritesStaticPodFiles(t *testing.T) {
	values := writeValues(t, valuesWithLineage())
	out := filepath.Join(t.TempDir(), "generated")

	output, err := executeCommand(t, "init", "--values", values, "--out", out,
		"--model", "static-pod", "--image", initImage, "--socket-gid", "1234", "--policy-name", "kms-policy")
	if err != nil {
		t.Fatalf("init failed: %v\n%s", err, output)
	}
	manifest := readGenerated(t, filepath.Join(out, "bao-kms-provider.yaml"))
	for _, want := range []string{initImage, "- 1234", "--config=/etc/openbao-kms/config.yaml"} {
		if !strings.Contains(manifest, want) {
			t.Fatalf("manifest missing %q:\n%s", want, manifest)
		}
	}
	cfg, err := config.Load(config.NewRuntime(), config.LoadOptions{Path: filepath.Join(out, "config.yaml")})
	if err != nil || cfg.Server.SocketGroup != "1234" {
		t.Fatalf("static pod config must use the numeric socket GID: %v %q", err, cfg.Server.SocketGroup)
	}
	script := readGenerated(t, filepath.Join(out, "openbao-setup.sh"))
	if !strings.Contains(script, "bao policy write 'kms-policy'") {
		t.Fatalf("setup script must use --policy-name:\n%s", script)
	}
}

func TestInitGeneratesLineageOnlyWithNewKey(t *testing.T) {
	out := filepath.Join(t.TempDir(), "generated")
	_, err := executeCommand(t, "init", "--values", writeValues(t, initValues), "--out", out)
	requireExitCode(t, err, cli.ExitConfig)
	if _, statErr := os.Stat(out); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal("a failed init must not create the output directory")
	}

	output, err := executeCommand(t, "init", "--values", writeValues(t, initValues), "--out", out, "--new-key")
	if err != nil {
		t.Fatalf("init --new-key failed: %v\n%s", err, output)
	}
	cfg, err := config.Load(config.NewRuntime(), config.LoadOptions{Path: filepath.Join(out, "config.yaml")})
	if err != nil || len(cfg.Transit.KeyIDScope.KeyLineageID) != 32 {
		t.Fatalf("expected a generated 32-character lineage ID: %v %q", err, cfg.Transit.KeyIDScope.KeyLineageID)
	}

	_, err = executeCommand(t, "init", "--values", writeValues(t, valuesWithLineage()),
		"--out", filepath.Join(t.TempDir(), "other"), "--new-key")
	requireExitCode(t, err, cli.ExitConfig)
}

func TestInitRefusesNonEmptyOutput(t *testing.T) {
	out := t.TempDir()
	existing := filepath.Join(out, "config.yaml")
	if err := os.WriteFile(existing, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := executeCommand(t, "init", "--values", writeValues(t, valuesWithLineage()), "--out", out)
	requireExitCode(t, err, cli.ExitError)
	if content := readGenerated(t, existing); content != "keep" {
		t.Fatalf("init must not touch existing files: %q", content)
	}
}

func TestInitRejectsInvalidFlags(t *testing.T) {
	values := writeValues(t, valuesWithLineage())
	cases := [][]string{
		{"init", "--out", t.TempDir() + "/a"},
		{"init", "--values", values},
		{"init", "--values", values, "--out", t.TempDir() + "/b", "--model", "daemonset"},
		{"init", "--values", values, "--out", t.TempDir() + "/c", "--model", "static-pod", "--socket-gid", "1234"},
		{
			"init", "--values", values, "--out", t.TempDir() + "/d", "--model", "static-pod",
			"--image", "ghcr.io/dc-tec/bao-kms-provider:latest", "--socket-gid", "1234",
		},
		{"init", "--values", values, "--out", t.TempDir() + "/e", "--image", initImage},
	}
	for _, args := range cases {
		_, err := executeCommand(t, args...)
		requireExitCode(t, err, cli.ExitUsage)
	}
}

func TestInitRejectsInvalidValues(t *testing.T) {
	invalid := strings.Replace(valuesWithLineage(),
		"https://bao.example.internal:8200", "http://bao.example.internal:8200", 1)
	_, err := executeCommand(t, "init", "--values", writeValues(t, invalid), "--out", filepath.Join(t.TempDir(), "x"))
	requireExitCode(t, err, cli.ExitConfig)

	noSubject := strings.Replace(valuesWithLineage(), "    expectedSubject: system:openbao-kms:workload-a\n", "", 1)
	_, err = executeCommand(t, "init", "--values", writeValues(t, noSubject), "--out", filepath.Join(t.TempDir(), "y"))
	requireExitCode(t, err, cli.ExitConfig)
}
