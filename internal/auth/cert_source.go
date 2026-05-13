package auth

import (
	"context"
	"crypto"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
)

// CertificateProvider returns the current TLS client certificate for cert auth.
type CertificateProvider interface {
	CurrentCertificate(context.Context) (tls.Certificate, error)
}

// CertLoginSourceConfig contains certificate auth login settings.
type CertLoginSourceConfig struct {
	MountPath        string
	Name             string
	MinRemainingTTL  time.Duration
	ClockSkewLeeway  time.Duration
	ExpectedSPIFFEID string
	TrustDomain      string
}

// CertLoginSource performs local certificate validation and OpenBao cert login.
type CertLoginSource struct {
	cfg      CertLoginSourceConfig
	provider CertificateProvider
}

// NewCertLoginSource creates a certificate login source.
func NewCertLoginSource(
	cfg CertLoginSourceConfig,
	provider CertificateProvider,
) (*CertLoginSource, error) {
	normalized, err := validateCertLoginSourceConfig(cfg)
	if err != nil {
		return nil, err
	}
	if provider == nil {
		return nil, fmt.Errorf("%w: certificate provider is required", ErrAuthConfig)
	}
	return &CertLoginSource{cfg: normalized, provider: provider}, nil
}

// Login validates the current client certificate and exchanges it for an OpenBao token.
func (s *CertLoginSource) Login(
	ctx context.Context,
	client OpenBaoAuthClient,
	clock Clock,
) (LoginResult, error) {
	tlsCert, err := s.provider.CurrentCertificate(ctx)
	if err != nil {
		return LoginResult{}, err
	}
	cert, err := certificateFromTLS(tlsCert, CertificateValidationOptions{
		MinRemainingTTL:  s.cfg.MinRemainingTTL,
		ClockSkewLeeway:  s.cfg.ClockSkewLeeway,
		ExpectedSPIFFEID: s.cfg.ExpectedSPIFFEID,
		TrustDomain:      s.cfg.TrustDomain,
		Clock:            clock,
	})
	if err != nil {
		return LoginResult{}, err
	}
	signer, ok := tlsCert.PrivateKey.(crypto.Signer)
	if !ok {
		return LoginResult{}, fmt.Errorf("%w: TLS certificate private key is not a signer", ErrCertificateSignerMismatch)
	}
	if err := ValidateCertificateSigner(cert.Leaf, signer); err != nil {
		return LoginResult{}, err
	}

	authToken, err := client.LoginCert(ctx, openbao.CertLoginRequest{
		MountPath: s.cfg.MountPath,
		Name:      s.cfg.Name,
	})
	if err != nil {
		return LoginResult{}, publicAuthError(err)
	}
	return LoginResult{AuthToken: authToken, Certificate: cert}, nil
}

func validateCertLoginSourceConfig(cfg CertLoginSourceConfig) (CertLoginSourceConfig, error) {
	cfg.MountPath = strings.TrimSpace(cfg.MountPath)
	cfg.Name = strings.TrimSpace(cfg.Name)
	cfg.ExpectedSPIFFEID = strings.TrimSpace(cfg.ExpectedSPIFFEID)
	cfg.TrustDomain = strings.TrimSpace(cfg.TrustDomain)
	if cfg.MountPath == "" {
		return CertLoginSourceConfig{}, fmt.Errorf("%w: auth mount path is required", ErrAuthConfig)
	}
	if cfg.MountPath != path.Clean(cfg.MountPath) || strings.HasPrefix(cfg.MountPath, "/") || cfg.MountPath == "." {
		return CertLoginSourceConfig{}, fmt.Errorf("%w: auth mount path must be a relative OpenBao path", ErrAuthConfig)
	}
	if containsUnsafeIdentifierChars(cfg.MountPath) {
		return CertLoginSourceConfig{}, fmt.Errorf("%w: auth mount path contains unsafe characters", ErrAuthConfig)
	}
	if containsUnsafeIdentifierChars(cfg.Name) {
		return CertLoginSourceConfig{}, fmt.Errorf("%w: auth cert name contains unsafe characters", ErrAuthConfig)
	}
	if cfg.MinRemainingTTL <= 0 {
		return CertLoginSourceConfig{}, fmt.Errorf("%w: minimum certificate remaining TTL must be positive", ErrAuthConfig)
	}
	if cfg.ClockSkewLeeway < 0 {
		return CertLoginSourceConfig{}, fmt.Errorf("%w: clock skew leeway must not be negative", ErrAuthConfig)
	}
	return cfg, nil
}

func certificateFromTLS(tlsCert tls.Certificate, opts CertificateValidationOptions) (Certificate, error) {
	chain, err := certificateChainFromTLS(tlsCert)
	if err != nil {
		return Certificate{}, err
	}
	if err := ValidateCertificateChain(chain, opts); err != nil {
		return Certificate{}, err
	}
	return Certificate{
		Leaf:      chain[0],
		Chain:     chain,
		ReadAt:    clockOrReal(opts.Clock).Now(),
		ExpiresAt: chain[0].NotAfter,
	}, nil
}

func certificateChainFromTLS(tlsCert tls.Certificate) ([]*x509.Certificate, error) {
	if len(tlsCert.Certificate) == 0 {
		return nil, fmt.Errorf("%w: TLS certificate chain is empty", ErrCertificateMalformed)
	}
	chain := make([]*x509.Certificate, 0, len(tlsCert.Certificate))
	for _, der := range tlsCert.Certificate {
		cert, err := x509.ParseCertificate(der)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid X.509 certificate", ErrCertificateMalformed)
		}
		chain = append(chain, cert)
	}
	return chain, nil
}
