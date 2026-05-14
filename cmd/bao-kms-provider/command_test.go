package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/cli"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/version"
)

func executeCommand(t *testing.T, args ...string) (string, error) {
	t.Helper()

	var out bytes.Buffer
	cmd := newRootCommand(version.Info{
		Version:   "1.2.3",
		Commit:    "abc1234",
		BuildDate: "2026-05-08T00:00:00Z",
		Dirty:     "false",
	})
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)

	err := cmd.Execute()
	return out.String(), err
}

func TestRootHelp(t *testing.T) {
	output, err := executeCommand(t, "--help")
	if err != nil {
		t.Fatalf("expected help to succeed: %v", err)
	}

	required := []string{
		"OpenBao-native Kubernetes KMS v2 provider",
		"serve",
		"doctor",
		"policy",
		"--config",
	}
	for _, want := range required {
		if !strings.Contains(output, want) {
			t.Fatalf("help output missing %q:\n%s", want, output)
		}
	}
}

func TestPolicyOpenBaoCommand(t *testing.T) {
	output, err := executeCommand(t, "policy", "openbao", "--config", "../../test/testdata/config/valid.yaml")
	if err != nil {
		t.Fatalf("expected policy command to succeed: %v", err)
	}

	required := []string{
		`path "transit/keys/k8s-workload-a-etcd"`,
		`capabilities = ["read"]`,
		`path "transit/encrypt/k8s-workload-a-etcd"`,
		`path "transit/decrypt/k8s-workload-a-etcd"`,
		`path "transit/config/keys"`,
		`path "sys/capabilities-self"`,
	}
	for _, want := range required {
		if !strings.Contains(output, want) {
			t.Fatalf("policy output missing %q:\n%s", want, output)
		}
	}
}

func TestConfigCommandUsesConfigPathEnvironment(t *testing.T) {
	t.Setenv(envConfigPath, "../../test/testdata/config/valid.yaml")

	output, err := executeCommand(t, "policy", "openbao")
	if err != nil {
		t.Fatalf("expected command to use env config path: %v", err)
	}
	if !strings.Contains(output, `path "transit/keys/k8s-workload-a-etcd"`) {
		t.Fatalf("policy output did not use env config path:\n%s", output)
	}
}

func TestLookupGroupIDAcceptsNumericGID(t *testing.T) {
	gid, err := lookupGroupID("1234")
	if err != nil {
		t.Fatalf("lookup numeric group: %v", err)
	}
	if gid != 1234 {
		t.Fatalf("unexpected gid: %d", gid)
	}
}

func TestVersionCommand(t *testing.T) {
	output, err := executeCommand(t, "version")
	if err != nil {
		t.Fatalf("expected version command to succeed: %v", err)
	}

	required := []string{
		"version: 1.2.3",
		"commit: abc1234",
		"buildDate: 2026-05-08T00:00:00Z",
		"dirty: false",
	}
	for _, want := range required {
		if !strings.Contains(output, want) {
			t.Fatalf("version output missing %q:\n%s", want, output)
		}
	}
}

func TestConfigSchemaCommand(t *testing.T) {
	output, err := executeCommand(t, "config", "schema")
	if err != nil {
		t.Fatalf("expected config schema command to succeed: %v", err)
	}

	var schema struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal([]byte(output), &schema); err != nil {
		t.Fatalf("schema output is not JSON: %v", err)
	}
	if schema.Title == "" {
		t.Fatal("schema title is empty")
	}
}

func TestServeFailsClosedWithoutConfig(t *testing.T) {
	output, err := executeCommand(t, "serve")
	if err == nil {
		t.Fatalf("expected serve command without config to fail, output:\n%s", output)
	}
	if got := cli.ProcessExitCode(err); got != int(cli.ExitConfig) {
		t.Fatalf("unexpected serve exit code: %d", got)
	}
	if !strings.Contains(err.Error(), "config invalid") {
		t.Fatalf("unexpected serve error: %v", err)
	}
}

func TestDoctorPrintsReportForInvalidConfig(t *testing.T) {
	output, err := executeCommand(t, "doctor")
	if err == nil {
		t.Fatalf("expected doctor without config to fail, output:\n%s", output)
	}
	if got := cli.ProcessExitCode(err); got != int(cli.ExitConfig) {
		t.Fatalf("unexpected doctor exit code: %d", got)
	}
	for _, want := range []string{
		"doctor",
		"[pass] config.load",
		"[fail] config.validate",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, output)
		}
	}
}

func TestDoctorPrintsJSONReportForInvalidConfig(t *testing.T) {
	output, err := executeCommand(t, "doctor", "--output", "json")
	if err == nil {
		t.Fatalf("expected doctor without config to fail, output:\n%s", output)
	}
	if got := cli.ProcessExitCode(err); got != int(cli.ExitConfig) {
		t.Fatalf("unexpected doctor exit code: %d", got)
	}

	var report cli.Report
	if jsonErr := json.Unmarshal([]byte(output), &report); jsonErr != nil {
		t.Fatalf("doctor JSON output is not a report: %v\n%s", jsonErr, output)
	}
	if report.Name != reportNameDoctor {
		t.Fatalf("unexpected report name: %q", report.Name)
	}
	if len(report.Checks) < 2 {
		t.Fatalf("expected config checks in report: %#v", report.Checks)
	}
	if report.Checks[0].ID != checkConfigLoad || report.Checks[0].Status != cli.CheckPass {
		t.Fatalf("unexpected first check: %#v", report.Checks[0])
	}
	if report.Checks[1].ID != checkConfigValidate || report.Checks[1].Status != cli.CheckFail {
		t.Fatalf("unexpected second check: %#v", report.Checks[1])
	}
}
