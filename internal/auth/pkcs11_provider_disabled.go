//go:build !certauth_pkcs11

package auth

import (
	"context"
	"fmt"
)

// NewPKCS11CertificateProvider is available only in certauth_pkcs11 builds.
func NewPKCS11CertificateProvider(
	context.Context,
	PKCS11ProviderConfig,
) (ClientCertificateProvider, error) {
	return nil, fmt.Errorf("%w: pkcs11 certificate source requires certauth_pkcs11 build tag", ErrAuthConfig)
}
