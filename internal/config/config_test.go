package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	testDebugLogLevel            = "debug"
	testDebugCorrelationIncident = "INC-123"
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
	if cfg.OpenBao.Timeout != 2*time.Second {
		t.Fatalf("unexpected OpenBao timeout: %s", cfg.OpenBao.Timeout)
	}
	if cfg.State.Path != "/var/lib/openbao-kms/state/key-registry.json" {
		t.Fatalf("unexpected state path: %q", cfg.State.Path)
	}
	if cfg.Auth.ClockSkewLeeway != 30*time.Second {
		t.Fatalf("unexpected auth clock skew leeway: %s", cfg.Auth.ClockSkewLeeway)
	}
	if cfg.Auth.TokenRenewalIncrement != time.Hour {
		t.Fatalf("unexpected token renewal increment: %s", cfg.Auth.TokenRenewalIncrement)
	}
	if cfg.Auth.LoginTimeout != 0 {
		t.Fatalf("auth login timeout should default to derived value, got %s", cfg.Auth.LoginTimeout)
	}
	if cfg.Bootstrap.GraceTimeout != time.Minute || cfg.Bootstrap.RetryInterval != 5*time.Second {
		t.Fatalf("unexpected bootstrap defaults: %#v", cfg.Bootstrap)
	}
	if cfg.Logging.DebugCorrelation.Enabled {
		t.Fatal("debug correlation should default to disabled")
	}
	if cfg.Logging.DebugCorrelation.TTL != 15*time.Minute {
		t.Fatalf("unexpected debug correlation ttl: %s", cfg.Logging.DebugCorrelation.TTL)
	}
}

func TestLoadConfigFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := []byte(`
server:
  socketPath: /tmp/bao-kms-provider.sock
  socketGroup: kube-apiserver
  metricsAddress: "127.0.0.1:18081"
openbao:
  address: https://bao.example.internal:8200
  caCertFile: /etc/openbao-kms/tls/ca.crt
  tlsServerName: bao.example.internal
  timeout: 3s
  instanceId: bao-prod-a
auth:
  method: jwt
  mountPath: auth/k8s-workload-a-jwt
  role: openbao-kms-control-plane
  jwtFile: /var/lib/openbao-kms/identity.jwt
  clockSkewLeeway: 45s
  tokenRenewalIncrement: 2h
  loginTimeout: 9s
  expectedIssuer: https://issuer.example.internal
  expectedAudience:
    - openbao
  expectedSubject: system:serviceaccount:kube-system:bao-kms-provider
transit:
  mountPath: transit
  keyName: k8s-workload-a-etcd
  keyIdScope:
    providerName: openbao-kms-workload-a
    clusterId: workload-a
    transitMountId: transit-prod-primary
    keyLineageId: "01HXEXAMPLEKEYLINEAGEID"
  useAssociatedData: true
status:
  probeInterval: 45s
bootstrap:
  graceTimeout: 30s
  retryInterval: 3s
state:
  path: /var/lib/openbao-kms/state/custom-key-registry.json
`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write config fixture: %v", err)
	}

	cfg, err := Load(NewRuntime(), LoadOptions{Path: path})
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	assertLoadedConfigFile(t, cfg)
}

func assertLoadedConfigFile(t *testing.T, cfg Config) {
	t.Helper()

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
	if cfg.State.Path != "/var/lib/openbao-kms/state/custom-key-registry.json" {
		t.Fatalf("unexpected state path: %q", cfg.State.Path)
	}
	if cfg.Auth.ClockSkewLeeway != 45*time.Second {
		t.Fatalf("unexpected clock skew leeway: %s", cfg.Auth.ClockSkewLeeway)
	}
	assertLoadedConfigFileAuth(t, cfg.Auth)
	if cfg.Bootstrap.GraceTimeout != 30*time.Second || cfg.Bootstrap.RetryInterval != 3*time.Second {
		t.Fatalf("unexpected bootstrap config: %#v", cfg.Bootstrap)
	}
}

func assertLoadedConfigFileAuth(t *testing.T, cfg AuthConfig) {
	t.Helper()

	if cfg.TokenRenewalIncrement != 2*time.Hour {
		t.Fatalf("unexpected token renewal increment: %s", cfg.TokenRenewalIncrement)
	}
	if cfg.LoginTimeout != 9*time.Second {
		t.Fatalf("unexpected login timeout: %s", cfg.LoginTimeout)
	}
	if cfg.ExpectedIssuer != "https://issuer.example.internal" {
		t.Fatalf("unexpected expected issuer: %q", cfg.ExpectedIssuer)
	}
	if len(cfg.ExpectedAudience) != 1 || cfg.ExpectedAudience[0] != "openbao" {
		t.Fatalf("unexpected expected audience: %#v", cfg.ExpectedAudience)
	}
	if cfg.ExpectedSubject != "system:serviceaccount:kube-system:bao-kms-provider" {
		t.Fatalf("unexpected expected subject: %q", cfg.ExpectedSubject)
	}
}

func TestLoadRejectsUnknownConfigField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := []byte(`
server:
  socketPath: /run/openbao-kms/kms.sock
  unexpected: true
`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write config fixture: %v", err)
	}

	_, err := Load(NewRuntime(), LoadOptions{Path: path})
	if err == nil {
		t.Fatal("expected unknown config field to fail")
	}
	if !strings.Contains(err.Error(), "invalid keys") {
		t.Fatalf("unexpected unknown field error: %v", err)
	}
}

func TestLoadMissingConfigFile(t *testing.T) {
	_, err := Load(NewRuntime(), LoadOptions{Path: filepath.Join(t.TempDir(), "missing.yaml")})
	if err == nil {
		t.Fatal("expected missing config to fail")
	}
}

func TestValidateCompleteConfig(t *testing.T) {
	cfg := loadValidConfig(t)

	if err := Validate(cfg, ValidationOptions{}); err != nil {
		t.Fatalf("validate config: %v", err)
	}
}

func TestLoadLegacyConfigWithoutVersion(t *testing.T) {
	cfg, err := Load(NewRuntime(), LoadOptions{Path: "../../test/testdata/config/legacy-no-version.yaml"})
	if err != nil {
		t.Fatalf("load legacy config: %v", err)
	}
	if cfg.ConfigVersion != "v1alpha1" {
		t.Fatalf("unexpected default config version: %s", cfg.ConfigVersion)
	}
	if err := Validate(cfg, ValidationOptions{}); err != nil {
		t.Fatalf("validate legacy config: %v", err)
	}
}

func TestValidateReportsMissingRequiredFields(t *testing.T) {
	cfg := Config{}

	err := Validate(cfg, ValidationOptions{})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected invalid config error, got %v", err)
	}
	var validationErr ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected validation error, got %T", err)
	}
	if len(validationErr.Problems) == 0 {
		t.Fatal("expected validation problems")
	}
}

func TestValidateRejectsUnsafeValues(t *testing.T) {
	tests := []struct {
		name   string
		field  string
		mutate func(*Config)
	}{
		{
			name:  "bad OpenBao address",
			field: "openbao.address",
			mutate: func(cfg *Config) {
				cfg.OpenBao.Address = "http://bao.example.internal:8200"
			},
		},
		{
			name:  "OpenBao address with query",
			field: "openbao.address",
			mutate: func(cfg *Config) {
				cfg.OpenBao.Address = "https://bao.example.internal:8200?token=secret"
			},
		},
		{
			name:  "invalid duration",
			field: "openbao.timeout",
			mutate: func(cfg *Config) {
				cfg.OpenBao.Timeout = 0
			},
		},
		{
			name:  "negative auth clock skew leeway",
			field: "auth.clockSkewLeeway",
			mutate: func(cfg *Config) {
				cfg.Auth.ClockSkewLeeway = -time.Second
			},
		},
		{
			name:  "invalid token renewal increment",
			field: "auth.tokenRenewalIncrement",
			mutate: func(cfg *Config) {
				cfg.Auth.TokenRenewalIncrement = 0
			},
		},
		{
			name:  "negative auth login timeout",
			field: "auth.loginTimeout",
			mutate: func(cfg *Config) {
				cfg.Auth.LoginTimeout = -time.Second
			},
		},
		{
			name:  "negative bootstrap grace timeout",
			field: "bootstrap.graceTimeout",
			mutate: func(cfg *Config) {
				cfg.Bootstrap.GraceTimeout = -time.Second
			},
		},
		{
			name:  "invalid bootstrap retry interval",
			field: "bootstrap.retryInterval",
			mutate: func(cfg *Config) {
				cfg.Bootstrap.RetryInterval = 0
			},
		},
		{
			name:  "expected issuer with whitespace",
			field: "auth.expectedIssuer",
			mutate: func(cfg *Config) {
				cfg.Auth.ExpectedIssuer = " https://issuer.example.internal"
			},
		},
		{
			name:  "empty expected audience",
			field: "auth.expectedAudience",
			mutate: func(cfg *Config) {
				cfg.Auth.ExpectedAudience = []string{""}
			},
		},
		{
			name:  "auth role with surrounding whitespace",
			field: "auth.role",
			mutate: func(cfg *Config) {
				cfg.Auth.Role = " openbao-kms-control-plane"
			},
		},
		{
			name:  "auth mount with surrounding whitespace",
			field: "auth.mountPath",
			mutate: func(cfg *Config) {
				cfg.Auth.MountPath = " auth/k8s-workload-a-jwt"
			},
		},
		{
			name:  "bad socket path",
			field: "server.socketPath",
			mutate: func(cfg *Config) {
				cfg.Server.SocketPath = "/tmp/kms.sock"
			},
		},
		{
			name:  "broad socket mode",
			field: "server.socketMode",
			mutate: func(cfg *Config) {
				cfg.Server.SocketMode = "0666"
			},
		},
		{
			name:  "AAD disabled",
			field: "transit.useAssociatedData",
			mutate: func(cfg *Config) {
				cfg.Transit.UseAssociatedData = false
			},
		},
		{
			name:  "decrypt micro-batching enabled",
			field: "performance.decryptMicroBatching.enabled",
			mutate: func(cfg *Config) {
				cfg.Performance.DecryptMicroBatching.Enabled = true
			},
		},
		{
			name:  "relative state path",
			field: "state.path",
			mutate: func(cfg *Config) {
				cfg.State.Path = "key-registry.json"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := loadValidConfig(t)
			tt.mutate(&cfg)

			err := Validate(cfg, ValidationOptions{})
			if !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("expected invalid config error, got %v", err)
			}
			if !strings.Contains(err.Error(), tt.field) {
				t.Fatalf("expected %s problem, got %v", tt.field, err)
			}
		})
	}
}

func TestValidateDebugCorrelationGuardrails(t *testing.T) {
	tests := []struct {
		name   string
		field  string
		mutate func(*Config)
	}{
		{
			name:  "requires debug log level",
			field: "logging.debugCorrelation.enabled",
			mutate: func(cfg *Config) {
				cfg.Logging.Level = "info"
				cfg.Logging.DebugCorrelation.Enabled = true
				cfg.Logging.DebugCorrelation.TTL = 15 * time.Minute
				cfg.Logging.DebugCorrelation.IncidentID = testDebugCorrelationIncident
			},
		},
		{
			name:  "requires OpenBao request IDs",
			field: "logging.logOpenBaoRequestIDs",
			mutate: func(cfg *Config) {
				cfg.Logging.Level = testDebugLogLevel
				cfg.Logging.LogOpenBaoRequestIDs = false
				cfg.Logging.DebugCorrelation.Enabled = true
				cfg.Logging.DebugCorrelation.TTL = 15 * time.Minute
				cfg.Logging.DebugCorrelation.IncidentID = testDebugCorrelationIncident
			},
		},
		{
			name:  "requires ttl",
			field: "logging.debugCorrelation.ttl",
			mutate: func(cfg *Config) {
				cfg.Logging.Level = testDebugLogLevel
				cfg.Logging.DebugCorrelation.Enabled = true
				cfg.Logging.DebugCorrelation.TTL = 0
				cfg.Logging.DebugCorrelation.IncidentID = testDebugCorrelationIncident
			},
		},
		{
			name:  "caps ttl",
			field: "logging.debugCorrelation.ttl",
			mutate: func(cfg *Config) {
				cfg.Logging.Level = testDebugLogLevel
				cfg.Logging.DebugCorrelation.Enabled = true
				cfg.Logging.DebugCorrelation.TTL = 2 * time.Hour
				cfg.Logging.DebugCorrelation.IncidentID = testDebugCorrelationIncident
			},
		},
		{
			name:  "requires incident id",
			field: "logging.debugCorrelation.incidentId",
			mutate: func(cfg *Config) {
				cfg.Logging.Level = testDebugLogLevel
				cfg.Logging.DebugCorrelation.Enabled = true
				cfg.Logging.DebugCorrelation.TTL = 15 * time.Minute
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := loadValidConfig(t)
			tt.mutate(&cfg)

			err := Validate(cfg, ValidationOptions{})
			if !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("expected invalid config error, got %v", err)
			}
			if !strings.Contains(err.Error(), tt.field) {
				t.Fatalf("expected %s problem, got %v", tt.field, err)
			}
		})
	}
}

func TestValidateAcceptsGuardedDebugCorrelation(t *testing.T) {
	cfg := loadValidConfig(t)
	cfg.Logging.Level = testDebugLogLevel
	cfg.Logging.LogOpenBaoRequestIDs = true
	cfg.Logging.DebugCorrelation.Enabled = true
	cfg.Logging.DebugCorrelation.TTL = 15 * time.Minute
	cfg.Logging.DebugCorrelation.IncidentID = testDebugCorrelationIncident

	if err := Validate(cfg, ValidationOptions{}); err != nil {
		t.Fatalf("validate debug correlation config: %v", err)
	}
}

func TestValidateRejectsUnsafeLocalFiles(t *testing.T) {
	tempDir := t.TempDir()
	cfg := loadValidConfig(t)
	cfg.Server.SocketPath = filepath.Join(tempDir, "kms.sock")
	cfg.OpenBao.CACertFile = filepath.Join(tempDir, "ca.crt")
	cfg.Auth.JWTFile = filepath.Join(tempDir, "identity.jwt")
	configPath := filepath.Join(tempDir, "config.yaml")

	writeFile(t, cfg.OpenBao.CACertFile, 0o644, "ca")
	writeFile(t, cfg.Auth.JWTFile, 0o644, "jwt")
	writeFile(t, configPath, 0o644, "config")

	err := Validate(cfg, ValidationOptions{
		ConfigFilePath:          configPath,
		CheckFilesystem:         true,
		AllowedSocketParentDirs: []string{tempDir},
	})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected invalid config error, got %v", err)
	}
	if !strings.Contains(err.Error(), "auth.jwtFile") {
		t.Fatalf("expected JWT permission problem, got %v", err)
	}
	if !strings.Contains(err.Error(), "config") {
		t.Fatalf("expected config permission problem, got %v", err)
	}
}

func TestValidateRejectsGroupWritableSocketParent(t *testing.T) {
	tempDir := t.TempDir()
	// #nosec G302 -- this test intentionally creates an unsafe socket parent mode.
	if err := os.Chmod(tempDir, 0o770); err != nil {
		t.Fatalf("chmod socket parent: %v", err)
	}
	cfg := loadValidConfig(t)
	cfg.Server.SocketPath = filepath.Join(tempDir, "kms.sock")
	cfg.OpenBao.CACertFile = filepath.Join(tempDir, "ca.crt")
	cfg.Auth.JWTFile = filepath.Join(tempDir, "identity.jwt")

	writeFile(t, cfg.OpenBao.CACertFile, 0o644, "ca")
	writeFile(t, cfg.Auth.JWTFile, 0o640, "jwt")

	err := Validate(cfg, ValidationOptions{
		CheckFilesystem:         true,
		AllowedSocketParentDirs: []string{tempDir},
	})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected invalid config error, got %v", err)
	}
	if !strings.Contains(err.Error(), "server.socketPath") {
		t.Fatalf("expected socket path problem, got %v", err)
	}
}

func TestValidateAcceptsSafeLocalFiles(t *testing.T) {
	tempDir := t.TempDir()
	cfg := loadValidConfig(t)
	cfg.Server.SocketPath = filepath.Join(tempDir, "kms.sock")
	cfg.OpenBao.CACertFile = filepath.Join(tempDir, "ca.crt")
	cfg.Auth.JWTFile = filepath.Join(tempDir, "identity.jwt")
	configPath := filepath.Join(tempDir, "config.yaml")

	writeFile(t, cfg.OpenBao.CACertFile, 0o644, "ca")
	writeFile(t, cfg.Auth.JWTFile, 0o640, "jwt")
	writeFile(t, configPath, 0o640, "config")

	err := Validate(cfg, ValidationOptions{
		ConfigFilePath:          configPath,
		CheckFilesystem:         true,
		AllowedSocketParentDirs: []string{tempDir},
	})
	if err != nil {
		t.Fatalf("validate filesystem config: %v", err)
	}
}

func TestIdentityFingerprintIsStableAndSensitiveToIdentity(t *testing.T) {
	cfg := loadValidConfig(t)

	first, err := IdentityFingerprint(cfg)
	if err != nil {
		t.Fatalf("fingerprint config: %v", err)
	}
	second, err := IdentityFingerprint(cfg)
	if err != nil {
		t.Fatalf("fingerprint config again: %v", err)
	}
	if first != second {
		t.Fatalf("fingerprint is not stable: %s != %s", first, second)
	}

	cfg.Transit.KeyIDScope.ClusterID = "workload-b"
	changed, err := IdentityFingerprint(cfg)
	if err != nil {
		t.Fatalf("fingerprint changed config: %v", err)
	}
	if changed == first {
		t.Fatal("fingerprint did not change after identity-bearing field changed")
	}
}

func TestParseAndValidateEncryptionConfiguration(t *testing.T) {
	cfg := loadValidConfig(t)
	encryptionConfig := loadEncryptionConfig(t, "valid-with-identity.yaml")

	result, err := ValidateEncryptionConfiguration(
		cfg,
		encryptionConfig,
		EncryptionValidationOptions{AllowIdentityFallback: true},
	)
	if err != nil {
		t.Fatalf("validate encryption config: %v", err)
	}
	if !result.IdentityFallback {
		t.Fatal("expected identity fallback to be detected")
	}
	if result.MatchedProviderName != cfg.Transit.KeyIDScope.ProviderName {
		t.Fatalf("unexpected provider match: %s", result.MatchedProviderName)
	}
}

func TestValidateEncryptionConfigurationRejectsIdentityFallbackWhenDisallowed(t *testing.T) {
	cfg := loadValidConfig(t)
	encryptionConfig := loadEncryptionConfig(t, "valid-with-identity.yaml")

	_, err := ValidateEncryptionConfiguration(cfg, encryptionConfig, EncryptionValidationOptions{})
	if !errors.Is(err, ErrInvalidEncryptionConfiguration) {
		t.Fatalf("expected invalid encryption config error, got %v", err)
	}
}

func TestValidateEncryptionConfigurationRejectsProviderMismatch(t *testing.T) {
	cfg := loadValidConfig(t)
	encryptionConfig := loadEncryptionConfig(t, "provider-mismatch.yaml")

	_, err := ValidateEncryptionConfiguration(cfg, encryptionConfig, EncryptionValidationOptions{})
	if !errors.Is(err, ErrInvalidEncryptionConfiguration) {
		t.Fatalf("expected invalid encryption config error, got %v", err)
	}
	if !strings.Contains(err.Error(), "kms.name") {
		t.Fatalf("expected provider name mismatch, got %v", err)
	}
}

func TestSchemaJSONIsValid(t *testing.T) {
	var decoded struct {
		Schema string `json:"$schema"`
	}
	if err := json.Unmarshal(SchemaJSON(), &decoded); err != nil {
		t.Fatalf("schema JSON is invalid: %v", err)
	}
}

func loadValidConfig(t *testing.T) Config {
	t.Helper()

	cfg, err := Load(NewRuntime(), LoadOptions{Path: "../../test/testdata/config/valid.yaml"})
	if err != nil {
		t.Fatalf("load valid config: %v", err)
	}
	return cfg
}

func loadEncryptionConfig(t *testing.T, name string) EncryptionConfiguration {
	t.Helper()

	cfg, err := LoadEncryptionConfiguration(filepath.Join("../../test/testdata/encryptionconfig", name))
	if err != nil {
		t.Fatalf("load encryption config: %v", err)
	}
	return cfg
}

func writeFile(t *testing.T, path string, mode os.FileMode, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod %s: %v", path, err)
	}
}
