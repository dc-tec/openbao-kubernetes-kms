package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/cli"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/version"
	"github.com/spf13/cobra"
)

func TestCommandUsageExitCodes(t *testing.T) {
	for _, args := range [][]string{
		{"unknown-command"},
		{"--unknown-flag"},
		{"serve", "--unknown-flag"},
		{"version", "extra"},
		{"policy", "unknown-command"},
		{"config", "schema", "extra"},
		{"serve", "--config"},
		{"init", "--socket-gid", "invalid"},
		{"completion", "unknown-shell"},
		{"completion", "bash", "extra"},
		{"completion", "bash", "--no-descriptions=invalid"},
		{"help", "unknown-command"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			_, err := executeCommand(t, args...)
			if got := cli.ProcessExitCode(err); got != int(cli.ExitUsage) {
				t.Fatalf("exit code = %d, want 2; error = %v", got, err)
			}
		})
	}
}

func TestPublicBuiltinsKeepOutputAndSuccess(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"--help"},
		{"help", "config", "schema"},
		{"completion"},
		{"completion", "bash"},
		{"completion", "zsh"},
		{"completion", "fish"},
		{"completion", "powershell"},
		{"config"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			output, err := executeCommand(t, args...)
			if err != nil || output == "" {
				t.Fatalf("expected successful output, got %q; error = %v", output, err)
			}
		})
	}
}

func TestInvalidConfigurationFlagReturnsConfigExitCode(t *testing.T) {
	_, err := executeCommand(t, "serve", "--log-level", "trace")
	if cli.ProcessExitCode(err) != int(cli.ExitConfig) || !strings.Contains(err.Error(), "logging.level") {
		t.Fatalf("expected a logging configuration error with exit code 3, got %v", err)
	}
}

func TestCommandExecutionPreservesNonUsageFailures(t *testing.T) {
	for _, failure := range []struct {
		err  error
		code cli.ExitCode
	}{
		{io.ErrClosedPipe, cli.ExitError},
		{cli.WithExitCode(cli.ExitCheckFailed, errors.New("diagnostic failed")), cli.ExitCheckFailed},
		{cli.WithExitCode(cli.ExitRuntime, errors.New("runtime failed")), cli.ExitRuntime},
	} {
		cmd := newRootCommand(version.Info{})
		cmd.AddCommand(&cobra.Command{
			Use: "failing", Args: cobra.NoArgs,
			RunE: func(_ *cobra.Command, _ []string) error { return failure.err },
		})
		cmd.SetArgs([]string{"failing"})
		err := executeRootCommand(cmd)
		if got := cli.ProcessExitCode(err); got != int(failure.code) || !errors.Is(err, failure.err) {
			t.Fatalf("command failure changed: code %d, error %v", got, err)
		}
	}
}

func TestCLIProcessExitCodes(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	malformed := filepath.Join(dir, "malformed.yaml")
	if err := os.WriteFile(malformed, []byte("server: ["), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		args []string
		code int
	}{
		{[]string{"version"}, 0},
		{[]string{"unknown-command"}, 2},
		{[]string{"serve", "--unknown-flag"}, 2},
		{[]string{"version", "extra"}, 2},
		{[]string{"config", "--config", filepath.Join(dir, "missing.yaml")}, 3},
		{[]string{"config", "--config", malformed}, 3},
		{[]string{"serve"}, 3},
	} {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			command := exec.Command(executable, "-test.run=^TestCLIProcessHelper$", "--")
			command.Args = append(command.Args, tt.args...)
			command.Env = append(os.Environ(), "BAO_KMS_TEST_CLI_PROCESS=1",
				envConfigPath+"=", envConfigPathAlt+"=")
			output, err := command.CombinedOutput()
			code := 0
			if err != nil {
				var exitError *exec.ExitError
				if !errors.As(err, &exitError) {
					t.Fatal(err)
				}
				code = exitError.ExitCode()
			}
			if code != tt.code {
				t.Fatalf("process exit code = %d, want %d; output = %s", code, tt.code, output)
			}
			if tt.code != 0 && len(bytes.TrimSpace(output)) == 0 {
				t.Fatal("failed process did not print its error")
			}
		})
	}
}

func TestCLIProcessHelper(t *testing.T) {
	if os.Getenv("BAO_KMS_TEST_CLI_PROCESS") != "1" {
		return
	}
	separator := slices.Index(os.Args, "--")
	if separator < 0 {
		t.Fatal("missing CLI argument separator")
	}
	os.Args = append([]string{commandName}, os.Args[separator+1:]...)
	main()
	os.Exit(0)
}
