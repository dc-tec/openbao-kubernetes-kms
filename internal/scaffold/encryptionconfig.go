package scaffold

import (
	"github.com/dc-tec/openbao-kubernetes-kms/internal/config"
)

const (
	authMethodJWT  = "jwt"
	authMethodCert = "cert"

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
// encrypted through this provider, with the identity fallback for migration.
func RenderEncryptionConfig(cfg config.Config) ([]byte, error) {
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
	return encodeYAML(file)
}
