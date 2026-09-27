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
	var includeTokenRenewal bool
	cmd := &cobra.Command{
		Use:   "openbao",
		Short: "Generate the least-privilege OpenBao policy for this provider config",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadAndValidateConfig(runtimeConfig, *configPath, false)
			if err != nil {
				return err
			}
			opts := scaffold.PolicyOptions{IncludeTokenRenewal: includeTokenRenewal}
			if err := scaffold.WriteOpenBaoPolicy(cmd.OutOrStdout(), cfg, opts); err != nil {
				return cli.WithExitCode(cli.ExitError, err)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&includeTokenRenewal, "include-token-renewal", true,
		"Include auth/token/renew-self for renewable OpenBao tokens")
	return cmd
}
