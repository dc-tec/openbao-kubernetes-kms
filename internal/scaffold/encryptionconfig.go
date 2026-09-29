package scaffold

import (
	"github.com/dc-tec/openbao-kubernetes-kms/internal/config"
)

const (
	encryptionConfigAPIVersion = "apiserver.config.k8s.io/v1"
	encryptionConfigKind       = "EncryptionConfiguration"
	kmsAPIVersion              = "v2"
	// DefaultKMSTimeout is the documented starting timeout for KMS calls.
	DefaultKMSTimeout = "3s"
)

type encryptionConfigFile struct {
	APIVersion string                  `yaml:"apiVersion"`
	Kind       string                  `yaml:"kind"`
	Resources  []encryptionResourceSet `yaml:"resources"`
}

type encryptionResourceSet struct {
	Resources []string                 `yaml:"resources"`
	Providers []encryptionProviderFile `yaml:"providers"`
}

type encryptionProviderFile struct {
	KMS      *kmsProviderFile `yaml:"kms,omitempty"`
	Identity *emptyObject     `yaml:"identity,omitempty"`
}

type kmsProviderFile struct {
	APIVersion string `yaml:"apiVersion"`
	Name       string `yaml:"name"`
	Endpoint   string `yaml:"endpoint"`
	Timeout    string `yaml:"timeout"`
}

// emptyObject encodes as {}.
type emptyObject struct{}

// RenderEncryptionConfig renders the initial EncryptionConfiguration: Secrets
// encrypted through this provider, with the identity reader for plaintext objects.
func RenderEncryptionConfig(cfg config.Config) ([]byte, error) {
	return renderEncryptionConfig(cfg, false)
}

// RenderEncryptionReaderConfig stages KMS reads while identity remains the writer.
// Install it on every API server before enabling KMS writes in a fresh cluster.
func RenderEncryptionReaderConfig(cfg config.Config) ([]byte, error) {
	return renderEncryptionConfig(cfg, true)
}

func renderEncryptionConfig(cfg config.Config, identityFirst bool) ([]byte, error) {
	file := encryptionConfigFile{
		APIVersion: encryptionConfigAPIVersion,
		Kind:       encryptionConfigKind,
		Resources: []encryptionResourceSet{{
			Resources: []string{"secrets"},
			Providers: []encryptionProviderFile{
				{KMS: &kmsProviderFile{
					APIVersion: kmsAPIVersion,
					Name:       cfg.Transit.KeyIDScope.ProviderName,
					Endpoint:   "unix://" + cfg.Server.SocketPath,
					Timeout:    DefaultKMSTimeout,
				}},
				{Identity: &emptyObject{}},
			},
		}},
	}
	if identityFirst {
		providers := file.Resources[0].Providers
		providers[0], providers[1] = providers[1], providers[0]
	}
	return encodeYAML(file)
}
