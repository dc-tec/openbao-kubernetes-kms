// Package scaffold renders the deployment files that share the provider's
// identity values: provider configuration, Kubernetes EncryptionConfiguration,
// OpenBao policy, OpenBao setup script, and static pod manifest.
package scaffold

import (
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/config"
)

// PolicyOptions controls optional OpenBao policy stanzas.
type PolicyOptions struct {
	// IncludeTokenRenewal adds auth/token/renew-self for roles that disable
	// the default policy while the provider renews its token.
	IncludeTokenRenewal bool
}

// PolicyPaths lists the OpenBao paths the provider token needs.
type PolicyPaths struct {
	Metadata         string
	Encrypt          string
	Decrypt          string
	DisableUpsert    string
	CapabilitiesSelf string
	RenewSelf        string
}

type policyStanza struct {
	Path         string
	Capabilities []string
	Comment      string
}

// OpenBaoPolicyPaths derives the policy paths for the configured Transit key.
func OpenBaoPolicyPaths(cfg config.Config) PolicyPaths {
	mountPath := cfg.Transit.MountPath
	keyName := cfg.Transit.KeyName
	return PolicyPaths{
		Metadata:         path.Join(mountPath, "keys", keyName),
		Encrypt:          path.Join(mountPath, "encrypt", keyName),
		Decrypt:          path.Join(mountPath, "decrypt", keyName),
		DisableUpsert:    path.Join(mountPath, "config", "keys"),
		CapabilitiesSelf: path.Join("sys", "capabilities-self"),
		RenewSelf:        path.Join("auth", "token", "renew-self"),
	}
}

// WriteOpenBaoPolicy writes the least-privilege OpenBao policy in HCL.
func WriteOpenBaoPolicy(out io.Writer, cfg config.Config, opts PolicyOptions) error {
	paths := OpenBaoPolicyPaths(cfg)
	stanzas := []policyStanza{
		{Path: paths.Metadata, Capabilities: []string{"read"}, Comment: "Read Transit key metadata."},
		{Path: paths.Encrypt, Capabilities: []string{"update"}, Comment: "Encrypt with the existing key."},
		{Path: paths.Decrypt, Capabilities: []string{"update"}, Comment: "Decrypt existing ciphertext."},
		{Path: paths.DisableUpsert, Capabilities: []string{"read"}, Comment: "Inspect Transit disable_upsert."},
		{
			Path:         paths.CapabilitiesSelf,
			Capabilities: []string{"update"},
			Comment:      "Allow doctor to inspect this token's capabilities.",
		},
	}
	if opts.IncludeTokenRenewal {
		stanzas = append(stanzas, policyStanza{
			Path:         paths.RenewSelf,
			Capabilities: []string{"update"},
			Comment:      "Allow token renewal when the auth role disables the default policy.",
		})
	}
	for _, stanza := range stanzas {
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
