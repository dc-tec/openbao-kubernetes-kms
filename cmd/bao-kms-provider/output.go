package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/cli"
	"github.com/spf13/cobra"
)

const (
	outputFormatText = "text"
	outputFormatJSON = "json"
)

func addOutputFlag(cmd *cobra.Command, output *string) {
	cmd.Flags().StringVar(output, "output", outputFormatText, "Output format: text or json")
}

func printCLIReport(out io.Writer, report cli.Report, output string) error {
	switch normalizeOutputFormat(output) {
	case outputFormatText:
		cli.PrintText(out, report)
		return nil
	case outputFormatJSON:
		return cli.PrintJSON(out, report)
	default:
		return unsupportedOutputFormat(output)
	}
}

func unsupportedOutputFormat(output string) error {
	return cli.WithExitCode(
		cli.ExitUsage,
		fmt.Errorf("unsupported output format %q: expected text or json", output),
	)
}

func normalizeOutputFormat(output string) string {
	return strings.ToLower(strings.TrimSpace(output))
}
