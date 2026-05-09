package main

import (
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/cli"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/config"
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
			if err := writeOpenBaoPolicy(cmd.OutOrStdout(), cfg); err != nil {
				return cli.WithExitCode(cli.ExitError, err)
			}
			return nil
		},
	}
}

func writeOpenBaoPolicy(out io.Writer, cfg config.Config) error {
	paths := openBaoPolicyPaths(cfg)
	for _, stanza := range []openBaoPolicyStanza{
		{Path: paths.Metadata, Capabilities: []string{"read"}, Comment: "Read Transit key metadata."},
		{Path: paths.Encrypt, Capabilities: []string{"update"}, Comment: "Encrypt with the existing key."},
		{Path: paths.Decrypt, Capabilities: []string{"update"}, Comment: "Decrypt existing ciphertext."},
		{Path: paths.DisableUpsert, Capabilities: []string{"read"}, Comment: "Inspect Transit disable_upsert."},
		{
			Path:         paths.CapabilitiesSelf,
			Capabilities: []string{"update"},
			Comment:      "Allow doctor to inspect this token's capabilities.",
		},
	} {
		if _, err := fmt.Fprintf(out, "# %s\n", stanza.Comment); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(out, "path %s {\n", strconv.Quote(stanza.Path)); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(out, "  capabilities = [%s]\n", quotedList(stanza.Capabilities)); err != nil {
			return err
		}
		if _, err := fmt.Fprint(out, "}\n\n"); err != nil {
			return err
		}
	}
	return nil
}

type openBaoPolicyPathSet struct {
	Metadata         string
	Encrypt          string
	Decrypt          string
	DisableUpsert    string
	CapabilitiesSelf string
}

type openBaoPolicyStanza struct {
	Path         string
	Capabilities []string
	Comment      string
}

func openBaoPolicyPaths(cfg config.Config) openBaoPolicyPathSet {
	mountPath := cfg.Transit.MountPath
	keyName := cfg.Transit.KeyName
	return openBaoPolicyPathSet{
		Metadata:         path.Join(mountPath, "keys", keyName),
		Encrypt:          path.Join(mountPath, "encrypt", keyName),
		Decrypt:          path.Join(mountPath, "decrypt", keyName),
		DisableUpsert:    path.Join(mountPath, "config", "keys"),
		CapabilitiesSelf: path.Join("sys", "capabilities-self"),
	}
}

func quotedList(values []string) string {
	var result strings.Builder
	for index, value := range values {
		if index > 0 {
			result.WriteString(", ")
		}
		result.WriteString(strconv.Quote(value))
	}
	return result.String()
}
