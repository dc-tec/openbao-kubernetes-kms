package main

import (
	"github.com/dc-tec/openbao-kubernetes-kms/internal/cli"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/config"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/scaffold"
	"github.com/spf13/cobra"
)

func newPolicyCommand(runtimeConfig *config.Runtime, configPath *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "policy",
		Short: "Generate access-control policy snippets",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newOpenBaoPolicyCommand(runtimeConfig, configPath))
	return cmd
}

func newOpenBaoPolicyCommand(runtimeConfig *config.Runtime, configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "openbao",
		Short: "Generate the least-privilege OpenBao policy for this provider config",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadAndValidateConfig(runtimeConfig, *configPath, false)
			if err != nil {
				return err
			}
			if err := scaffold.WriteOpenBaoPolicy(cmd.OutOrStdout(), cfg, scaffold.PolicyOptions{}); err != nil {
				return cli.WithExitCode(cli.ExitError, err)
			}
			return nil
		},
	}
}
