//go:build e2e

package framework

import (
	"context"
	"path"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
)

type oidcAuthConfigRequestBody struct {
	JWTValidationPubKeys []string `json:"jwt_validation_pubkeys"`
	OIDCDiscoveryURL     string   `json:"oidc_discovery_url"`
	OIDCDiscoveryCAPEM   string   `json:"oidc_discovery_ca_pem"`
	BoundIssuer          string   `json:"bound_issuer"`
}

func (oidcAuthConfigRequestBody) environmentSetupPayload() {}

// UseOIDCDiscovery replaces fixture signing keys with the external issuer's discovery/JWKS trust.
func (f *OpenBaoEnvironment) UseOIDCDiscovery(ctx context.Context, caPEM string) error {
	client, err := openbao.NewHTTPClient(f.CACertFile, f.TLSServerName, 5*time.Second)
	if err != nil {
		return err
	}
	defer client.CloseIdleConnections()
	return f.write(ctx, client, path.Join(f.AuthMount, "config"), oidcAuthConfigRequestBody{
		JWTValidationPubKeys: []string{}, OIDCDiscoveryURL: f.jwtIssuer,
		OIDCDiscoveryCAPEM: caPEM, BoundIssuer: f.jwtIssuer,
	})
}
