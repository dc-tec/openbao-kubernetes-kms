// Package main provides the bao-kms-provider command.
package main

import (
	"fmt"
	"os"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/cli"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/version"
)

func main() {
	cmd := newRootCommand(version.BuildInfo())
	if err := cmd.Execute(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(cli.ProcessExitCode(err))
	}
}
