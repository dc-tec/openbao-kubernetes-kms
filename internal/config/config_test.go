package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(NewRuntime(), LoadOptions{})
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}

	if cfg.Server.SocketPath != "/run/openbao-kms/kms.sock" {
		t.Fatalf("unexpected socket path: %q", cfg.Server.SocketPath)
	}
	if cfg.Auth.Method != "jwt" {
		t.Fatalf("unexpected auth method: %q", cfg.Auth.Method)
	}
	if !cfg.Transit.UseAssociatedData {
		t.Fatal("associated data should default to enabled")
	}
	if cfg.Status.ProbeInterval != 30*time.Second {
		t.Fatalf("unexpected probe interval: %s", cfg.Status.ProbeInterval)
	}
}

func TestLoadConfigFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := []byte(`
server:
  socketPath: /tmp/bao-kms-provider.sock
  metricsAddress: "127.0.0.1:18081"
openbao:
  address: https://bao.example.internal:8200
  timeout: 3s
auth:
  method: jwt
transit:
  useAssociatedData: true
status:
  probeInterval: 45s
`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write config fixture: %v", err)
	}

	cfg, err := Load(NewRuntime(), LoadOptions{Path: path})
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.Server.SocketPath != "/tmp/bao-kms-provider.sock" {
		t.Fatalf("unexpected socket path: %q", cfg.Server.SocketPath)
	}
	if cfg.Server.MetricsAddress != "127.0.0.1:18081" {
		t.Fatalf("unexpected metrics address: %q", cfg.Server.MetricsAddress)
	}
	if cfg.OpenBao.Timeout != 3*time.Second {
		t.Fatalf("unexpected OpenBao timeout: %s", cfg.OpenBao.Timeout)
	}
	if cfg.Status.ProbeInterval != 45*time.Second {
		t.Fatalf("unexpected probe interval: %s", cfg.Status.ProbeInterval)
	}
}

func TestLoadMissingConfigFile(t *testing.T) {
	_, err := Load(NewRuntime(), LoadOptions{Path: filepath.Join(t.TempDir(), "missing.yaml")})
	if err == nil {
		t.Fatal("expected missing config to fail")
	}
}
