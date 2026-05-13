//go:build certauth_spiffe

package auth

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"

	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
)

// SPIFFECertificateProvider returns X.509 SVIDs from the SPIFFE Workload API.
type SPIFFECertificateProvider struct {
	cfg    SPIFFEProviderConfig
	source *workloadapi.X509Source
}

// NewSPIFFECertificateProvider creates a SPIFFE Workload API certificate provider.
func NewSPIFFECertificateProvider(
	ctx context.Context,
	cfg SPIFFEProviderConfig,
) (ClientCertificateProvider, error) {
	normalized, err := validateSPIFFEProviderConfig(cfg)
	if err != nil {
		return nil, err
	}
	options := []workloadapi.X509SourceOption{
		workloadapi.WithClientOptions(workloadapi.WithAddr(normalized.WorkloadAPISocket)),
	}
	if normalized.SPIFFEID != "" {
		expectedID, err := spiffeid.FromString(normalized.SPIFFEID)
		if err != nil {
			return nil, fmt.Errorf("%w: spiffe id is invalid", ErrAuthConfig)
		}
		options = append(options, workloadapi.WithDefaultX509SVIDPicker(
			func(svids []*x509svid.SVID) *x509svid.SVID {
				for _, svid := range svids {
					if svid != nil && svid.ID == expectedID {
						return svid
					}
				}
				return nil
			},
		))
	}
	source, err := workloadapi.NewX509Source(ctx, options...)
	if err != nil {
		return nil, fmt.Errorf("%w: initialize spiffe workload api source", ErrAuthConfig)
	}
	provider := &SPIFFECertificateProvider{
		cfg:    normalized,
		source: source,
	}
	if _, err := provider.CurrentCertificate(ctx); err != nil {
		_ = provider.Close()
		return nil, err
	}
	return provider, nil
}

// CurrentCertificate returns the current X.509 SVID as a TLS client certificate.
func (p *SPIFFECertificateProvider) CurrentCertificate(ctx context.Context) (tls.Certificate, error) {
	select {
	case <-ctx.Done():
		return tls.Certificate{}, ctx.Err()
	default:
	}
	svid, err := p.source.GetX509SVID()
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("%w: get spiffe x509 svid", ErrAuthConfig)
	}
	if svid == nil || len(svid.Certificates) == 0 || svid.PrivateKey == nil {
		return tls.Certificate{}, fmt.Errorf("%w: spiffe x509 svid is incomplete", ErrCertificateMalformed)
	}
	cert := tls.Certificate{
		Certificate: make([][]byte, 0, len(svid.Certificates)),
		PrivateKey:  svid.PrivateKey,
		Leaf:        svid.Certificates[0],
	}
	for _, parsed := range svid.Certificates {
		cert.Certificate = append(cert.Certificate, parsed.Raw)
	}
	return cert, nil
}

// GetClientCertificate returns the current SVID for TLS handshakes.
func (p *SPIFFECertificateProvider) GetClientCertificate(
	*tls.CertificateRequestInfo,
) (*tls.Certificate, error) {
	cert, err := p.CurrentCertificate(context.Background())
	if err != nil {
		return nil, err
	}
	return &cert, nil
}

// Close closes the SPIFFE Workload API source.
func (p *SPIFFECertificateProvider) Close() error {
	return p.source.Close()
}

func validateSPIFFEProviderConfig(cfg SPIFFEProviderConfig) (SPIFFEProviderConfig, error) {
	cfg.WorkloadAPISocket = strings.TrimSpace(cfg.WorkloadAPISocket)
	cfg.SPIFFEID = strings.TrimSpace(cfg.SPIFFEID)
	cfg.TrustDomain = strings.TrimSpace(cfg.TrustDomain)
	if cfg.WorkloadAPISocket == "" || cfg.SPIFFEID == "" {
		return SPIFFEProviderConfig{}, fmt.Errorf("%w: spiffe provider settings are required", ErrAuthConfig)
	}
	return cfg, nil
}
