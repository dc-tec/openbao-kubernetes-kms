package main

import (
	"fmt"
	"io"
	"os"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/config"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/version"
	"github.com/spf13/cobra"
)

const commandName = "bao-kms-provider"

const (
	envConfigPath    = "BAO_KMS_PROVIDER_CONFIG"
	envConfigPathAlt = "BAO_KMS_PROVIDER_CONFIG_PATH"
)

func newRootCommand(info version.Info) *cobra.Command {
	runtimeConfig := config.NewRuntime()
	configPath := defaultConfigPathFromEnv()

	cmd := &cobra.Command{
		Use:           commandName,
		Short:         "OpenBao-native Kubernetes KMS v2 provider",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       info.Version,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.PersistentFlags().StringVar(&configPath, "config", configPath, "Path to the provider configuration file")
	cmd.PersistentFlags().String("log-level", "", "Override configured log level")
	cmd.PersistentFlags().String("metrics-address", "", "Override metrics listen address")
	cmd.PersistentFlags().String("health-address", "", "Override health listen address")

	if err := config.BindRootFlags(runtimeConfig, cmd.PersistentFlags()); err != nil {
		bindingErr := err
		cmd.PersistentPreRunE = func(_ *cobra.Command, _ []string) error {
			return fmt.Errorf("bind root flags: %w", bindingErr)
		}
	}

	cmd.AddCommand(
		newVersionCommand(info),
		newConfigCommand(runtimeConfig, &configPath),
		newServeCommand(runtimeConfig, &configPath, info),
		newDoctorCommand(runtimeConfig, &configPath, info),
		newVerifyKeyCommand(runtimeConfig, &configPath),
		newBenchmarkCommand(runtimeConfig, &configPath),
		newRotationPlanCommand(runtimeConfig, &configPath),
		newVerifyRotationCommand(runtimeConfig, &configPath),
		newPolicyCommand(runtimeConfig, &configPath),
	)

	return cmd
}

func defaultConfigPathFromEnv() string {
	if value := os.Getenv(envConfigPath); value != "" {
		return value
	}
	return os.Getenv(envConfigPathAlt)
}

func newVersionCommand(info version.Info) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print build version metadata",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			printVersion(cmd.OutOrStdout(), info)
			return nil
		},
	}
}

func newConfigCommand(runtimeConfig *config.Runtime, configPath *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect typed configuration defaults",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			loaded, err := config.Load(runtimeConfig, config.LoadOptions{Path: *configPath})
			if err != nil {
				return err
			}
			printConfigSummary(cmd.OutOrStdout(), loaded)
			return nil
		},
	}
	cmd.AddCommand(newConfigSchemaCommand())
	return cmd
}

func newConfigSchemaCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "schema",
		Short: "Print configuration JSON Schema",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := cmd.OutOrStdout().Write(config.SchemaJSON())
			return err
		},
	}
}

func printVersion(out io.Writer, info version.Info) {
	_, _ = fmt.Fprintf(out, "version: %s\n", info.Version)
	_, _ = fmt.Fprintf(out, "commit: %s\n", info.Commit)
	_, _ = fmt.Fprintf(out, "buildDate: %s\n", info.BuildDate)
	_, _ = fmt.Fprintf(out, "dirty: %s\n", info.Dirty)
}

func printConfigSummary(out io.Writer, cfg config.Config) {
	_, _ = fmt.Fprintf(out, "configVersion: %s\n", cfg.ConfigVersion)
	_, _ = fmt.Fprintf(out, "socketPath: %s\n", cfg.Server.SocketPath)
	_, _ = fmt.Fprintf(out, "metricsAddress: %s\n", cfg.Server.MetricsAddress)
	_, _ = fmt.Fprintf(out, "healthAddress: %s\n", cfg.Server.HealthAddress)
	_, _ = fmt.Fprintf(out, "authMethod: %s\n", cfg.Auth.Method)
	_, _ = fmt.Fprintf(out, "transitAssociatedData: %t\n", cfg.Transit.UseAssociatedData)
	if fingerprint, err := config.IdentityFingerprint(cfg); err == nil {
		_, _ = fmt.Fprintf(out, "identityFingerprint: %s\n", fingerprint)
	}
}
