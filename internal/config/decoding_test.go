package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/pflag"
)

func TestLoadRejectsNonStringValues(t *testing.T) {
	tests := []struct {
		field string
		value string
	}{
		{field: "transit.keyName", value: "0123"},
		{field: "transit.mountPath", value: "1e3"},
		{field: "transit.keyIdScope.clusterId", value: "123"},
		{field: "transit.keyIdScope.providerName", value: "false"},
		{field: "transit.keyIdScope.transitMountId", value: "0x123"},
		{field: "transit.keyIdScope.keyLineageId", value: "1.25"},
		{field: "openbao.instanceId", value: "123"},
		{field: "openbao.namespace", value: "123"},
		{field: "server.socketMode", value: "0660"},
		{field: "server.socketGroup", value: "1234"},
		{field: "auth.jwt.role", value: "true"},
		{field: "auth.jwt.expectedAudience", value: "[123]"},
	}
	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			_, err := loadScalarConfig(t, tt.field, tt.value)
			if err == nil {
				t.Fatal("expected a non-string value to be rejected")
			}
			if !strings.Contains(err.Error(), tt.field) {
				t.Fatalf("error does not identify %s: %v", tt.field, err)
			}
		})
	}
}

func TestLoadRejectsInvalidDurationInputs(t *testing.T) {
	fields := []string{
		"openbao.timeout",
		"auth.loginBeforeTokenExpiry",
		"auth.tokenRenewalIncrement",
		"auth.loginTimeout",
		"auth.jwt.minRemainingTtl",
		"auth.jwt.clockSkewLeeway",
		"auth.cert.minRemainingTtl",
		"auth.cert.clockSkewLeeway",
		"bootstrap.graceTimeout",
		"bootstrap.retryInterval",
		"status.probeInterval",
		"status.deepProbeInterval",
		"status.statusMaxStaleness",
		"rotation.activationDelay",
		"logging.debugCorrelation.ttl",
	}
	for _, field := range fields {
		for _, value := range []string{"120", "0", "1.5", "true", "[120]", `"120"`} {
			t.Run(field+"/"+value, func(t *testing.T) {
				_, err := loadScalarConfig(t, field, value)
				if err == nil {
					t.Fatal("expected a duration without valid units to be rejected")
				}
				if !strings.Contains(err.Error(), field) {
					t.Fatalf("error does not identify %s: %v", field, err)
				}
			})
		}
	}
}

func TestLoadPreservesQuotedStringsAndDurationUnits(t *testing.T) {
	content, err := os.ReadFile("../../test/testdata/config/valid.yaml")
	if err != nil {
		t.Fatal(err)
	}
	document := strings.NewReplacer(
		"keyName: k8s-workload-a-etcd", `keyName: "0123"`,
		"mountPath: transit", `mountPath: "1e3"`,
		"clusterId: workload-a", `clusterId: "0123"`,
		"activationDelay: 2m", `activationDelay: "120s"`,
		"probeInterval: 30s", `probeInterval: "30s"`,
	).Replace(string(content))
	cfg, err := Load(NewRuntime(), LoadOptions{Path: writeConfigDocument(t, document)})
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(cfg, ValidationOptions{}); err != nil {
		t.Fatal(err)
	}
	if cfg.Transit.KeyName != "0123" || cfg.Transit.MountPath != "1e3" || cfg.Transit.KeyIDScope.ClusterID != "0123" {
		t.Fatalf("quoted identity changed: %+v", cfg.Transit)
	}
	if cfg.Rotation.ActivationDelay != 2*time.Minute || cfg.Status.ProbeInterval != 30*time.Second {
		t.Fatalf("duration units changed: activation=%s probe=%s", cfg.Rotation.ActivationDelay, cfg.Status.ProbeInterval)
	}
}

func TestLoadFlagOverridesRemainStrings(t *testing.T) {
	t.Setenv(envLogLevel, "warn")
	runtime := NewRuntime()
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.String("log-level", "", "")
	flags.String("metrics-address", "", "")
	flags.String("health-address", "", "")
	if err := BindRootFlags(runtime, flags); err != nil {
		t.Fatal(err)
	}
	if err := flags.Parse([]string{
		"--log-level=debug",
		"--metrics-address=127.0.0.1:19081",
		"--health-address=127.0.0.1:19082",
	}); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(runtime, LoadOptions{Path: "../../test/testdata/config/valid.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Logging.Level != testDebugLogLevel || cfg.Server.MetricsAddress != "127.0.0.1:19081" ||
		cfg.Server.HealthAddress != "127.0.0.1:19082" {
		t.Fatal("flag overrides were not preserved")
	}
}

func loadScalarConfig(t *testing.T, field, value string) (Config, error) {
	t.Helper()
	var document strings.Builder
	document.WriteString("configVersion: v1alpha1\n")
	parts := strings.Split(field, ".")
	for index, part := range parts {
		fmt.Fprintf(&document, "%s%s:", strings.Repeat("  ", index), part)
		if index == len(parts)-1 {
			fmt.Fprintf(&document, " %s", value)
		}
		document.WriteByte('\n')
	}
	return Load(NewRuntime(), LoadOptions{Path: writeConfigDocument(t, document.String())})
}

func writeConfigDocument(t *testing.T, document string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	// #nosec G703 -- fixed filename inside the test-owned temporary directory.
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
