package cli_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/cli"
)

func TestReportPrintTextAndFailureState(t *testing.T) {
	report := cli.Report{Name: "doctor"}
	report.Pass("config.load", "Config load", "loaded")
	report.Warn("fallback.identity", "Identity fallback", "configured")
	report.Fail("transit.profile", "Transit profile", "export is enabled")

	if !report.HasFailures() {
		t.Fatal("expected report to have failures")
	}

	var out bytes.Buffer
	cli.PrintText(&out, report)
	output := out.String()
	for _, want := range []string{
		"doctor",
		"[pass] config.load: Config load - loaded",
		"[warn] fallback.identity: Identity fallback - configured",
		"[fail] transit.profile: Transit profile - export is enabled",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("report output missing %q:\n%s", want, output)
		}
	}
}

func TestProcessExitCode(t *testing.T) {
	err := cli.WithExitCode(cli.ExitCheckFailed, errors.New("failed"))
	if got := cli.ProcessExitCode(err); got != int(cli.ExitCheckFailed) {
		t.Fatalf("unexpected exit code: %d", got)
	}
	if got := cli.ProcessExitCode(errors.New("plain")); got != int(cli.ExitError) {
		t.Fatalf("unexpected plain error exit code: %d", got)
	}
}
