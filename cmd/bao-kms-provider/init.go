package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/cli"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/config"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/scaffold"
	"github.com/spf13/cobra"
)

const (
	initModelSystemd   = "systemd"
	initModelStaticPod = "static-pod"
	initAuthMethodJWT  = "jwt"

	initDefaultCACertFile  = "/etc/openbao-kms/tls/ca.crt"
	initDefaultJWTFile     = "/var/lib/openbao-kms/identity.jwt"
	initDefaultSocketGroup = "openbao-kms-socket"
	initLineageIDBytes     = 16

	initFileConfig           = "config.yaml"
	initFileEncryptionConfig = "encryption-config.yaml"
	initFileSetupScript      = "openbao-setup.sh"
	initFileStaticPod        = "bao-kms-provider.yaml"
)

var (
	errInitNewKeyConflict = errors.New(
		"--new-key conflicts with transit.keyIdScope.keyLineageId in the values file; " +
			"a key lineage ID is generated only once")
	errInitLineageRequired = errors.New(
		"transit.keyIdScope.keyLineageId is required; " +
			"pass --new-key only when you are creating the Transit key")
	errInitStaticPodAuth = errors.New(
		"--model static-pod supports JWT auth only; mount certificate auth material by hand")
)

type initOptions struct {
	valuesPath string
	outDir     string
	model      string
	newKey     bool
	policyName string
	image      string
	socketGID  int64
}

type initFile struct {
	name    string
	mode    os.FileMode
	content []byte
	purpose string
}

func newInitCommand() *cobra.Command {
	var opts initOptions
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Generate matching provider, Kubernetes, and OpenBao files from one values file",
		Long: "Generate the provider configuration, Kubernetes EncryptionConfiguration, OpenBao policy,\n" +
			"OpenBao setup script, and for static pods the pod manifest, from one values file in the\n" +
			"provider configuration format. init never contacts OpenBao and never writes outside --out.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runInit(cmd.OutOrStdout(), opts)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&opts.valuesPath, "values", "",
		"Values file in the provider configuration format (required)")
	flags.StringVar(&opts.outDir, "out", "",
		"Directory to write the generated files to; must be empty or absent (required)")
	flags.StringVar(&opts.model, "model", initModelSystemd,
		"Deployment model: systemd or static-pod")
	flags.BoolVar(&opts.newKey, "new-key", false,
		"Generate transit.keyIdScope.keyLineageId for a Transit key you are about to create")
	flags.StringVar(&opts.policyName, "policy-name", "",
		"OpenBao policy name (default openbao-kms-<clusterId>)")
	flags.StringVar(&opts.image, "image", "",
		"Provider image pinned by digest (static-pod)")
	flags.Int64Var(&opts.socketGID, "socket-gid", 0,
		"Numeric host GID of the openbao-kms-socket group (static-pod)")
	return cmd
}

func runInit(out io.Writer, opts initOptions) error {
	if err := validateInitFlags(opts); err != nil {
		return cli.WithExitCode(cli.ExitUsage, err)
	}
	cfg, err := loadInitValues(opts)
	if err != nil {
		return cli.WithExitCode(cli.ExitConfig, err)
	}
	files, err := renderInitFiles(cfg, opts)
	if err != nil {
		return cli.WithExitCode(cli.ExitConfig, err)
	}
	fingerprint, err := config.IdentityFingerprint(cfg)
	if err != nil {
		return cli.WithExitCode(cli.ExitConfig, err)
	}
	if err := writeInitFiles(opts.outDir, files); err != nil {
		return cli.WithExitCode(cli.ExitError, err)
	}
	printInitSummary(out, opts, cfg, fingerprint, files)
	return nil
}

func validateInitFlags(opts initOptions) error {
	var problems []string
	if opts.valuesPath == "" {
		problems = append(problems, "--values is required")
	}
	if opts.outDir == "" {
		problems = append(problems, "--out is required")
	}
	switch opts.model {
	case initModelSystemd:
		if opts.image != "" || opts.socketGID != 0 {
			problems = append(problems, "--image and --socket-gid apply only to --model static-pod")
		}
	case initModelStaticPod:
		problems = append(problems, validateStaticPodFlags(opts)...)
	default:
		problems = append(problems, fmt.Sprintf("--model must be %s or %s", initModelSystemd, initModelStaticPod))
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func validateStaticPodFlags(opts initOptions) []string {
	var problems []string
	if opts.image == "" {
		problems = append(problems, "--image is required for --model static-pod")
	} else if err := scaffold.ValidateImageDigest(opts.image); err != nil {
		problems = append(problems, "--image: "+err.Error())
	}
	if opts.socketGID <= 0 {
		problems = append(problems, "--socket-gid is required for --model static-pod")
	}
	return problems
}

// loadInitValues reads the values file through the same loader and validator
// as serve, after filling the documented host layout for omitted paths.
func loadInitValues(opts initOptions) (config.Config, error) {
	cfg, err := config.Load(config.NewRuntime(), config.LoadOptions{Path: opts.valuesPath})
	if err != nil {
		return config.Config{}, err
	}
	if err := applyInitHostLayout(&cfg, opts); err != nil {
		return config.Config{}, err
	}
	if err := resolveInitLineage(&cfg, opts.newKey); err != nil {
		return config.Config{}, err
	}
	if err := config.Validate(cfg, config.ValidationOptions{}); err != nil {
		return config.Config{}, err
	}
	if opts.model == initModelStaticPod && cfg.Auth.Method != initAuthMethodJWT {
		return config.Config{}, errInitStaticPodAuth
	}
	return cfg, nil
}

func applyInitHostLayout(cfg *config.Config, opts initOptions) error {
	if cfg.OpenBao.CACertFile == "" {
		cfg.OpenBao.CACertFile = initDefaultCACertFile
	}
	if cfg.Auth.Method == initAuthMethodJWT && cfg.Auth.JWT.Source == config.JWTSourceFile && cfg.Auth.JWT.JWTFile == "" {
		cfg.Auth.JWT.JWTFile = initDefaultJWTFile
	}
	if opts.model != initModelStaticPod {
		if cfg.Server.SocketGroup == "" {
			cfg.Server.SocketGroup = initDefaultSocketGroup
		}
		return nil
	}
	gid := strconv.FormatInt(opts.socketGID, 10)
	if cfg.Server.SocketGroup != "" && cfg.Server.SocketGroup != gid {
		return fmt.Errorf("server.socketGroup %q does not match --socket-gid %d",
			cfg.Server.SocketGroup, opts.socketGID)
	}
	cfg.Server.SocketGroup = gid
	return nil
}

func resolveInitLineage(cfg *config.Config, newKey bool) error {
	lineage := cfg.Transit.KeyIDScope.KeyLineageID
	switch {
	case newKey && lineage != "":
		return errInitNewKeyConflict
	case newKey:
		generated, err := newLineageID()
		if err != nil {
			return err
		}
		cfg.Transit.KeyIDScope.KeyLineageID = generated
	case lineage == "":
		return errInitLineageRequired
	}
	return nil
}

func newLineageID() (string, error) {
	buffer := make([]byte, initLineageIDBytes)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate key lineage ID: %w", err)
	}
	return hex.EncodeToString(buffer), nil
}

func initPolicyName(cfg config.Config, opts initOptions) string {
	if opts.policyName != "" {
		return opts.policyName
	}
	return "openbao-kms-" + cfg.Transit.KeyIDScope.ClusterID
}

func renderInitFiles(cfg config.Config, opts initOptions) ([]initFile, error) {
	providerConfig, err := scaffold.RenderProviderConfig(cfg)
	if err != nil {
		return nil, err
	}
	encryptionConfig, err := scaffold.RenderEncryptionConfig(cfg)
	if err != nil {
		return nil, err
	}
	if err := checkGeneratedFiles(providerConfig, encryptionConfig); err != nil {
		return nil, err
	}

	var policy bytes.Buffer
	policyOpts := scaffold.PolicyOptions{IncludeTokenRenewal: true}
	if err := scaffold.WriteOpenBaoPolicy(&policy, cfg, policyOpts); err != nil {
		return nil, err
	}
	setupOpts := scaffold.SetupOptions{PolicyName: initPolicyName(cfg, opts)}
	setup, err := scaffold.RenderOpenBaoSetup(cfg, setupOpts)
	if err != nil {
		return nil, err
	}

	files := []initFile{
		{
			name: initFileConfig, mode: 0o640, content: providerConfig,
			purpose: "provider configuration for every control-plane node",
		},
		{
			name: initFileEncryptionConfig, mode: 0o644, content: encryptionConfig,
			purpose: "Kubernetes EncryptionConfiguration with the identity fallback",
		},
		{
			name: scaffold.PolicyFileName, mode: 0o644, content: policy.Bytes(),
			purpose: "least-privilege OpenBao policy",
		},
		{
			name: initFileSetupScript, mode: 0o750, content: setup,
			purpose: "OpenBao commands to review and run as an administrator",
		},
	}
	if opts.model == initModelStaticPod {
		podOpts := scaffold.StaticPodOptions{Image: opts.image, SocketGID: opts.socketGID}
		manifest, err := scaffold.RenderStaticPod(cfg, podOpts)
		if err != nil {
			return nil, err
		}
		files = append(files, initFile{
			name: initFileStaticPod, mode: 0o644, content: manifest,
			purpose: "static pod manifest for /etc/kubernetes/manifests",
		})
	}
	return files, nil
}

// checkGeneratedFiles loads the rendered files back through the provider's own
// parsers and cross-checks them, so init never writes a pair doctor would reject.
func checkGeneratedFiles(providerConfig []byte, encryptionConfig []byte) error {
	reloaded, err := config.Load(config.NewRuntime(), config.LoadOptions{Content: providerConfig})
	if err != nil {
		return fmt.Errorf("generated configuration does not load: %w", err)
	}
	if err := config.Validate(reloaded, config.ValidationOptions{}); err != nil {
		return fmt.Errorf("generated configuration is invalid: %w", err)
	}
	parsed, err := config.ParseEncryptionConfiguration(bytes.NewReader(encryptionConfig))
	if err != nil {
		return fmt.Errorf("generated EncryptionConfiguration does not parse: %w", err)
	}
	validation := config.EncryptionValidationOptions{AllowIdentityFallback: true}
	if _, err := config.ValidateEncryptionConfiguration(reloaded, parsed, validation); err != nil {
		return fmt.Errorf("generated EncryptionConfiguration does not match the configuration: %w", err)
	}
	return nil
}

func writeInitFiles(outDir string, files []initFile) error {
	if err := ensureEmptyDir(outDir); err != nil {
		return err
	}
	for _, file := range files {
		target := filepath.Join(outDir, file.name)
		// O_EXCL keeps init from ever replacing a file it did not just create.
		// #nosec G304 -- init writes only to the operator-supplied --out directory.
		handle, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, file.mode)
		if err != nil {
			return fmt.Errorf("write %s: %w", target, err)
		}
		if _, err := handle.Write(file.content); err != nil {
			_ = handle.Close()
			return fmt.Errorf("write %s: %w", target, err)
		}
		if err := handle.Close(); err != nil {
			return fmt.Errorf("write %s: %w", target, err)
		}
	}
	return nil
}

func ensureEmptyDir(dir string) error {
	entries, err := os.ReadDir(dir)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return os.MkdirAll(dir, 0o750)
	case err != nil:
		return fmt.Errorf("read %s: %w", dir, err)
	case len(entries) > 0:
		return fmt.Errorf("%s is not empty; init never overwrites generated files", dir)
	}
	return nil
}

func printInitSummary(out io.Writer, opts initOptions, cfg config.Config, fingerprint string, files []initFile) {
	_, _ = fmt.Fprintf(out, "Wrote %d files to %s:\n", len(files), opts.outDir)
	for _, file := range files {
		_, _ = fmt.Fprintf(out, "  %-24s %s\n", file.name, file.purpose)
	}
	_, _ = fmt.Fprintf(out, "\nidentityFingerprint: %s\n", fingerprint)
	if opts.newKey {
		_, _ = fmt.Fprintf(out, "keyLineageId: %s (generated; record it with your values file)\n",
			cfg.Transit.KeyIDScope.KeyLineageID)
	}
	_, _ = fmt.Fprintf(out, "\nNext: review %s and have an OpenBao administrator run it.\n", initFileSetupScript)
}
