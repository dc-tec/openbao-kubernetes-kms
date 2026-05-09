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
		"--config",
	}
	for _, want := range required {
		if !strings.Contains(output, want) {
			t.Fatalf("help output missing %q:\n%s", want, output)
		}
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
