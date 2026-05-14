//go:build !certauth_spiffe

package auth

import (
	"context"
	"fmt"
)

// NewSPIFFECertificateProvider is available only in certauth_spiffe builds.
func NewSPIFFECertificateProvider(
	context.Context,
	SPIFFEProviderConfig,
) (ClientCertificateProvider, error) {
	return nil, fmt.Errorf("%w: spiffe certificate source requires certauth_spiffe build tag", ErrAuthConfig)
}
