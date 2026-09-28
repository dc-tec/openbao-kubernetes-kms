package main

import (
	"fmt"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/cli"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/config"
	"github.com/spf13/cobra"
)

func newRestoreVersionsCommand(runtimeConfig *config.Runtime, configPath *string) *cobra.Command {
	var opts retirementOptions
	var output string
	cmd := &cobra.Command{
		Use: "restore-versions", Short: "Plan or apply restoration of accidentally removed historical decrypt keys",
		Long: "Restore selected removed versions to local decrypt lookup using matching Transit metadata. " +
			"Stop this node's provider and run as its OS user. This does not roll back state or restore deleted key material.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if len(opts.RestoreVersions) == 0 || (opts.Apply && opts.ExpectedStateHash == "") {
				return cli.WithExitCode(cli.ExitUsage, fmt.Errorf(
					"--versions is required; --apply also requires --expected-state-hash from a reviewed plan"))
			}
			if normalizeOutputFormat(output) != outputFormatText && normalizeOutputFormat(output) != outputFormatJSON {
				return unsupportedOutputFormat(output)
			}
			cfg, err := loadAndValidateConfig(runtimeConfig, *configPath, false)
			if err != nil {
				return err
			}
			report, err := runRetirement(commandContext(cmd), cfg, opts, readRetirementProfile)
			if err != nil {
				return cli.WithExitCode(cli.ExitCheckFailed, err)
			}
			return printRetirementReport(cmd.OutOrStdout(), report, output)
		},
	}
	cmd.Flags().IntSliceVar(&opts.RestoreVersions, "versions", nil,
		"Exact removed Transit versions to restore, comma-separated")
	cmd.Flags().BoolVar(&opts.Apply, "apply", false, "Persist restoration after reviewing the plan")
	cmd.Flags().StringVar(&opts.ExpectedStateHash, "expected-state-hash", "", "Exact stateHash from the reviewed plan")
	addOutputFlag(cmd, &output)
	return cmd
}
