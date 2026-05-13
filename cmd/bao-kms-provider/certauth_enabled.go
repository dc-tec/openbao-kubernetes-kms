//go:build certauth_pkcs11 || certauth_spiffe

package main

import (
	"context"
	"fmt"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/auth"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/cli"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/config"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
)

func newCertAuthManager(
	ctx context.Context,
	cfg config.Config,
	observer authRuntimeObserver,
) (*auth.Manager, error) {
	provider, err := newCertificateProvider(ctx, cfg)
	if err != nil {
		return nil, err
	}
	authClient, err := openbao.NewAuthClient(openbao.AuthClientConfig{
		Address:              cfg.OpenBao.Address,
		Namespace:            cfg.OpenBao.Namespace,
		CACertFile:           cfg.OpenBao.CACertFile,
		TLSServerName:        cfg.OpenBao.TLSServerName,
		Timeout:              authLoginTimeout(cfg),
		GetClientCertificate: provider.GetClientCertificate,
		Observer:             observer,
	})
	if err != nil {
		_ = provider.Close()
		return nil, err
	}
	source, err := auth.NewCertLoginSource(certLoginSourceConfig(cfg), provider)
	if err != nil {
		_ = provider.Close()
		return nil, err
	}
	return auth.NewManagerWithSource(lifecycleConfig(cfg), source, authClient, auth.ManagerOptions{
		RenewalEnabled: true,
		Observer:       observer,
	})
}

func checkLocalCertificateAuthForDoctor(
	ctx context.Context,
	report *cli.Report,
	cfg config.Config,
) bool {
	provider, err := newCertificateProvider(ctx, cfg)
	sourceCheck := certificateSourceCheckID(cfg)
	if err != nil {
		report.Fail(sourceCheck, certificateSourceCheckTitle(cfg), safeMessage(err))
		report.Skip(checkCertLocal, "Certificate identity", "certificate source is unavailable")
		report.Skip(checkCertSigner, "Certificate signer", "certificate source is unavailable")
		return false
	}
	defer func() {
		_ = provider.Close()
	}()
	report.Pass(sourceCheck, certificateSourceCheckTitle(cfg), "configured certificate source is reachable")

	source, err := auth.NewCertLoginSource(certLoginSourceConfig(cfg), provider)
	if err != nil {
		report.Fail(checkCertLocal, "Certificate identity", safeMessage(err))
		report.Skip(checkCertSigner, "Certificate signer", "certificate identity validation failed")
		return false
	}
	if _, err := source.ValidateLocal(ctx, nil); err != nil {
		report.Fail(checkCertLocal, "Certificate identity", safeMessage(err))
		report.Skip(checkCertSigner, "Certificate signer", "certificate identity validation failed")
		return false
	}
	report.Pass(checkCertLocal, "Certificate identity", "certificate is locally valid for cert auth")
	report.Pass(checkCertSigner, "Certificate signer", "signer matches certificate and accepts probe")
	return true
}

func newCertificateProvider(
	ctx context.Context,
	cfg config.Config,
) (auth.ClientCertificateProvider, error) {
	switch cfg.Auth.Cert.Source {
	case certSourcePKCS11:
		return auth.NewPKCS11CertificateProvider(ctx, auth.PKCS11ProviderConfig{
			CertificateFile: cfg.Auth.Cert.PKCS11.CertificateFile,
			ModulePath:      cfg.Auth.Cert.PKCS11.ModulePath,
			TokenLabel:      cfg.Auth.Cert.PKCS11.TokenLabel,
			KeyLabel:        cfg.Auth.Cert.PKCS11.KeyLabel,
			PINFile:         cfg.Auth.Cert.PKCS11.PINFile,
			MaxSessions:     cfg.Auth.Cert.PKCS11.MaxSessions,
		})
	case certSourceSPIFFE:
		return auth.NewSPIFFECertificateProvider(ctx, auth.SPIFFEProviderConfig{
			WorkloadAPISocket: cfg.Auth.Cert.SPIFFE.WorkloadAPISocket,
			SPIFFEID:          cfg.Auth.Cert.SPIFFE.SPIFFEID,
			TrustDomain:       cfg.Auth.Cert.SPIFFE.TrustDomain,
		})
	default:
		return nil, fmt.Errorf("%w: unsupported certificate source", auth.ErrAuthConfig)
	}
}

func certLoginSourceConfig(cfg config.Config) auth.CertLoginSourceConfig {
	return auth.CertLoginSourceConfig{
		MountPath:        cfg.Auth.Cert.MountPath,
		Name:             cfg.Auth.Cert.Name,
		Source:           cfg.Auth.Cert.Source,
		MinRemainingTTL:  cfg.Auth.Cert.MinRemainingTTL,
		ClockSkewLeeway:  cfg.Auth.Cert.ClockSkewLeeway,
		ExpectedSPIFFEID: cfg.Auth.Cert.SPIFFE.SPIFFEID,
		TrustDomain:      cfg.Auth.Cert.SPIFFE.TrustDomain,
	}
}

func lifecycleConfig(cfg config.Config) auth.LifecycleConfig {
	return auth.LifecycleConfig{
		LoginBeforeTokenExpiry: cfg.Auth.LoginBeforeTokenExpiry,
		TokenRenewalIncrement:  cfg.Auth.TokenRenewalIncrement,
	}
}
