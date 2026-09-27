package main

import (
	"fmt"
	"strings"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/cli"
	"github.com/spf13/cobra"
)

func executeRootCommand(cmd *cobra.Command) error {
	// Create public built-ins before wrapping argument validation. Completion
	// captures the output writer, so initialize it after callers set that writer.
	cmd.InitDefaultHelpCmd()
	cmd.InitDefaultCompletionCmd()
	for _, child := range cmd.Commands() {
		if child.Name() == "help" {
			child.Args = validateHelpTopic
		}
	}
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return cli.WithExitCode(cli.ExitUsage, err)
	})
	classifyArgumentErrors(cmd)
	return cmd.Execute()
}

func classifyArgumentErrors(cmd *cobra.Command) {
	if !cmd.Runnable() {
		// Cobra skips argument validation for command groups without a runner.
		// Keep their help behavior while rejecting unknown subcommands.
		cmd.RunE = func(cmd *cobra.Command, _ []string) error { return cmd.Help() }
		if cmd.Args == nil {
			cmd.Args = cobra.NoArgs
		}
	}
	if validate := cmd.Args; validate != nil {
		cmd.Args = func(cmd *cobra.Command, args []string) error {
			return cli.WithExitCode(cli.ExitUsage, validate(cmd, args))
		}
	}
	for _, child := range cmd.Commands() {
		classifyArgumentErrors(child)
	}
}

func validateHelpTopic(cmd *cobra.Command, args []string) error {
	_, remaining, err := cmd.Root().Find(args)
	if err != nil {
		return err
	}
	if len(remaining) != 0 {
		return fmt.Errorf("unknown help topic %q", strings.Join(args, " "))
	}
	return nil
}
