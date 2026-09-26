package config

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	kubernetesEncryptionConfigAPIVersion = "apiserver.config.k8s.io/v1"
	kubernetesEncryptionConfigKind       = "EncryptionConfiguration"
	kubernetesKMSAPIVersionV2            = "v2"
	unixEndpointScheme                   = "unix"
)

// EncryptionConfiguration is the subset of Kubernetes EncryptionConfiguration needed by doctor checks.
type EncryptionConfiguration struct {
	APIVersion string                        `yaml:"apiVersion"`
	Kind       string                        `yaml:"kind"`
	Resources  []EncryptionResourceSelection `yaml:"resources"`
}

// EncryptionResourceSelection describes one Kubernetes encrypted resource group.
type EncryptionResourceSelection struct {
	Resources []string             `yaml:"resources"`
	Providers []EncryptionProvider `yaml:"providers"`
}

// EncryptionProvider is one provider entry in a Kubernetes EncryptionConfiguration.
type EncryptionProvider struct {
	KMS       *KMSProvider       `yaml:"kms"`
	Identity  *IdentityProvider  `yaml:"identity"`
	AESCBC    *SymmetricProvider `yaml:"aescbc"`
	AESGCM    *SymmetricProvider `yaml:"aesgcm"`
	Secretbox *SymmetricProvider `yaml:"secretbox"`
}

// SymmetricProvider holds migration keys for a Kubernetes local encryption provider.
type SymmetricProvider struct {
	Keys []EncryptionKey `yaml:"keys"`
}

// EncryptionKey is a local encryption key. Diagnostic formatting redacts its contents.
type EncryptionKey struct {
	Name   string `yaml:"name"`
	Secret string `yaml:"secret"`
}

// String redacts key material in diagnostic output.
func (EncryptionKey) String() string { return "encryption key (redacted)" }

// GoString redacts key material in Go-syntax diagnostic output.
func (k EncryptionKey) GoString() string { return k.String() }

// KMSProvider is the Kubernetes KMS provider configuration.
type KMSProvider struct {
	APIVersion string `yaml:"apiVersion"`
	Name       string `yaml:"name"`
	Endpoint   string `yaml:"endpoint"`
	Timeout    string `yaml:"timeout"`
	CacheSize  *int32 `yaml:"cachesize"`
}

// IdentityProvider marks Kubernetes identity fallback in an EncryptionConfiguration.
type IdentityProvider struct{}

// EncryptionValidationOptions controls compatibility checks for Kubernetes provider config.
type EncryptionValidationOptions struct {
	AllowIdentityFallback bool
}

// EncryptionValidationResult summarizes security-sensitive provider config state.
type EncryptionValidationResult struct {
	KMSProviderCount    int
	IdentityFallback    bool
	MatchedProviderName string
	MatchedEndpoint     string
}

// LoadEncryptionConfiguration reads a Kubernetes EncryptionConfiguration from disk.
func LoadEncryptionConfiguration(path string) (EncryptionConfiguration, error) {
	// #nosec G304 -- doctor must read an operator-supplied local EncryptionConfiguration path.
	file, err := os.Open(path)
	if err != nil {
		return EncryptionConfiguration{}, fmt.Errorf("read encryption config: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	return ParseEncryptionConfiguration(file)
}

// ParseEncryptionConfiguration decodes Kubernetes EncryptionConfiguration YAML with unknown-field rejection.
func ParseEncryptionConfiguration(reader io.Reader) (EncryptionConfiguration, error) {
	var cfg EncryptionConfiguration
	decoder := yaml.NewDecoder(reader)
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		// YAML type errors can contain inline encryption keys. Do not expose them.
		return EncryptionConfiguration{}, fmt.Errorf(
			"%w: malformed YAML or unsupported field", ErrInvalidEncryptionConfiguration,
		)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return EncryptionConfiguration{}, fmt.Errorf("%w: expected one YAML document", ErrInvalidEncryptionConfiguration)
	}
	return cfg, nil
}

// ValidateEncryptionConfiguration checks Kubernetes provider identity against provider config.
func ValidateEncryptionConfiguration(
	cfg Config,
	encryptionConfig EncryptionConfiguration,
	opts EncryptionValidationOptions,
) (EncryptionValidationResult, error) {
	var problems []ValidationProblem
	result := EncryptionValidationResult{}

	if encryptionConfig.APIVersion != kubernetesEncryptionConfigAPIVersion {
		appendProblem(&problems, "encryptionConfig.apiVersion", "must be apiserver.config.k8s.io/v1")
	}
	if encryptionConfig.Kind != kubernetesEncryptionConfigKind {
		appendProblem(&problems, "encryptionConfig.kind", "must be EncryptionConfiguration")
	}
	if len(encryptionConfig.Resources) == 0 {
		appendProblem(&problems, "encryptionConfig.resources", "must contain at least one resource entry")
	}

	for resourceIndex, resource := range encryptionConfig.Resources {
		if len(resource.Resources) == 0 {
			appendProblem(&problems, resourceField(resourceIndex, "resources"), "must not be empty")
		}
		if len(resource.Providers) == 0 {
			appendProblem(&problems, resourceField(resourceIndex, "providers"), "must not be empty")
		}
		for providerIndex, provider := range resource.Providers {
			validateProviderEntry(&problems, resourceIndex, providerIndex, provider)
			if provider.Identity != nil {
				result.IdentityFallback = true
				continue
			}
			if provider.KMS == nil {
				continue
			}
			result.KMSProviderCount++
			validateKMSProvider(&problems, cfg, resourceIndex, providerIndex, *provider.KMS, &result)
		}
	}

	if result.KMSProviderCount == 0 {
		appendProblem(&problems, "encryptionConfig.providers", "must contain a kms provider")
	}
	if result.MatchedProviderName == "" {
		appendProblem(&problems, "encryptionConfig.providers.kms.name", "must include the configured KMS provider")
	}
	if result.IdentityFallback && !opts.AllowIdentityFallback {
		appendProblem(&problems, "encryptionConfig.identity", "identity fallback is not allowed")
	}
	if len(problems) > 0 {
		return result, ValidationError{Kind: ErrInvalidEncryptionConfiguration, Problems: problems}
	}
	return result, nil
}

func validateProviderEntry(
	problems *[]ValidationProblem,
	resourceIndex int,
	providerIndex int,
	provider EncryptionProvider,
) {
	count := 0
	if provider.KMS != nil {
		count++
	}
	if provider.Identity != nil {
		count++
	}
	for _, symmetric := range []*SymmetricProvider{provider.AESCBC, provider.AESGCM, provider.Secretbox} {
		if symmetric != nil {
			count++
			validateSymmetricProvider(problems, providerField(resourceIndex, providerIndex), symmetric)
		}
	}
	if count != 1 {
		appendProblem(problems, providerField(resourceIndex, providerIndex), "must configure exactly one provider type")
	}
}

func validateSymmetricProvider(problems *[]ValidationProblem, field string, provider *SymmetricProvider) {
	if len(provider.Keys) == 0 {
		appendProblem(problems, field+".keys", "must contain at least one key")
	}
	for keyIndex, key := range provider.Keys {
		if key.Name == "" || key.Secret == "" {
			appendProblem(problems, fmt.Sprintf("%s.keys[%d]", field, keyIndex), "key name and secret are required")
		}
	}
}

func validateKMSProvider(
	problems *[]ValidationProblem,
	cfg Config,
	resourceIndex int,
	providerIndex int,
	kms KMSProvider,
	result *EncryptionValidationResult,
) {
	field := providerField(resourceIndex, providerIndex)
	if kms.Name == "" {
		appendProblem(problems, field+".kms.name", "must not be empty")
	}
	if kms.APIVersion != "" && kms.APIVersion != "v1" && kms.APIVersion != kubernetesKMSAPIVersionV2 {
		appendProblem(problems, field+".kms.apiVersion", "must be v1 or v2")
	}
	socketPath := ""
	if kms.Name == cfg.Transit.KeyIDScope.ProviderName {
		socketPath = cfg.Server.SocketPath
		if kms.APIVersion != kubernetesKMSAPIVersionV2 {
			appendProblem(problems, field+".kms.apiVersion", "must be v2 for the configured provider")
		}
	}
	if kms.APIVersion == kubernetesKMSAPIVersionV2 && kms.CacheSize != nil {
		appendProblem(problems, field+".kms.cachesize", "is only allowed for KMS v1 providers")
	}
	if err := validateKMSEndpoint(kms.Endpoint, socketPath); err != nil {
		appendProblem(problems, field+".kms.endpoint", err.Error())
	}
	if kms.Timeout != "" {
		timeout, err := time.ParseDuration(kms.Timeout)
		if err != nil || timeout <= 0 {
			appendProblem(problems, field+".kms.timeout", "must be a positive duration")
		}
	}
	if kms.Name == cfg.Transit.KeyIDScope.ProviderName {
		result.MatchedProviderName = kms.Name
		result.MatchedEndpoint = kms.Endpoint
	}
}

func validateKMSEndpoint(endpoint string, socketPath string) error {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("must be a unix URL")
	}
	if parsed.Scheme != unixEndpointScheme || parsed.Path == "" || parsed.Host != "" {
		return fmt.Errorf("must be a unix URL with an absolute socket path")
	}
	if socketPath != "" && parsed.Path != socketPath {
		return fmt.Errorf("must match server.socketPath")
	}
	return nil
}

func resourceField(resourceIndex int, field string) string {
	return fmt.Sprintf("encryptionConfig.resources[%d].%s", resourceIndex, field)
}

func providerField(resourceIndex int, providerIndex int) string {
	return fmt.Sprintf("encryptionConfig.resources[%d].providers[%d]", resourceIndex, providerIndex)
}
