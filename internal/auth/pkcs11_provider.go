//go:build certauth_pkcs11

package auth

import (
	"context"
	"crypto"
	"crypto/tls"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ThalesGroup/crypto11"
)

const (
	maxPKCS11PINFileBytes = 4096
	pkcs11PINFileModeMask = os.FileMode(0o037)
)

// PKCS11CertificateProvider returns a client certificate backed by a PKCS#11 signer.
type PKCS11CertificateProvider struct {
	cfg    PKCS11ProviderConfig
	ctx    *crypto11.Context
	signer crypto.Signer
	mu     sync.Mutex
	closed bool
}

// NewPKCS11CertificateProvider creates a PKCS#11-backed certificate provider.
func NewPKCS11CertificateProvider(
	ctx context.Context,
	cfg PKCS11ProviderConfig,
) (ClientCertificateProvider, error) {
	normalized, err := validatePKCS11ProviderConfig(cfg)
	if err != nil {
		return nil, err
	}
	pin, err := readPKCS11PINFile(normalized.PINFile)
	if err != nil {
		return nil, err
	}
	pkcs11Ctx, err := crypto11.Configure(&crypto11.Config{
		Path:        normalized.ModulePath,
		TokenLabel:  normalized.TokenLabel,
		Pin:         pin,
		MaxSessions: normalized.MaxSessions,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: initialize pkcs11 context", ErrAuthConfig)
	}
	provider := &PKCS11CertificateProvider{
		cfg: normalized,
		ctx: pkcs11Ctx,
	}
	if err := provider.loadSigner(); err != nil {
		_ = provider.Close()
		return nil, err
	}
	if _, err := provider.CurrentCertificate(ctx); err != nil {
		_ = provider.Close()
		return nil, err
	}
	return provider, nil
}

// CurrentCertificate returns the configured certificate chain with the PKCS#11 signer.
func (p *PKCS11CertificateProvider) CurrentCertificate(ctx context.Context) (tls.Certificate, error) {
	select {
	case <-ctx.Done():
		return tls.Certificate{}, ctx.Err()
	default:
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return tls.Certificate{}, fmt.Errorf("%w: pkcs11 provider is closed", ErrAuthConfig)
	}
	chain, err := ReadCertificateChain(p.cfg.CertificateFile)
	if err != nil {
		return tls.Certificate{}, err
	}
	cert := tls.Certificate{
		Certificate: make([][]byte, 0, len(chain)),
		PrivateKey:  p.signer,
		Leaf:        chain[0],
	}
	for _, parsed := range chain {
		cert.Certificate = append(cert.Certificate, parsed.Raw)
	}
	return cert, nil
}

// GetClientCertificate returns the current client certificate for TLS handshakes.
func (p *PKCS11CertificateProvider) GetClientCertificate(
	*tls.CertificateRequestInfo,
) (*tls.Certificate, error) {
	cert, err := p.CurrentCertificate(context.Background())
	if err != nil {
		return nil, err
	}
	return &cert, nil
}

// Close releases the PKCS#11 context.
func (p *PKCS11CertificateProvider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	if p.ctx == nil {
		return nil
	}
	return p.ctx.Close()
}

func (p *PKCS11CertificateProvider) loadSigner() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	signer, err := p.ctx.FindKeyPair(nil, []byte(p.cfg.KeyLabel))
	if err != nil {
		return fmt.Errorf("%w: find pkcs11 key pair", ErrAuthConfig)
	}
	if signer == nil {
		return fmt.Errorf("%w: pkcs11 key pair not found", ErrAuthConfig)
	}
	p.signer = signer
	return nil
}

func validatePKCS11ProviderConfig(cfg PKCS11ProviderConfig) (PKCS11ProviderConfig, error) {
	cfg.CertificateFile = strings.TrimSpace(cfg.CertificateFile)
	cfg.ModulePath = strings.TrimSpace(cfg.ModulePath)
	cfg.TokenLabel = strings.TrimSpace(cfg.TokenLabel)
	cfg.KeyLabel = strings.TrimSpace(cfg.KeyLabel)
	cfg.PINFile = strings.TrimSpace(cfg.PINFile)
	if cfg.CertificateFile == "" || cfg.ModulePath == "" || cfg.TokenLabel == "" || cfg.KeyLabel == "" || cfg.PINFile == "" {
		return PKCS11ProviderConfig{}, fmt.Errorf("%w: pkcs11 provider settings are required", ErrAuthConfig)
	}
	if cfg.MaxSessions < 2 {
		return PKCS11ProviderConfig{}, fmt.Errorf("%w: pkcs11 max sessions must be at least 2", ErrAuthConfig)
	}
	return cfg, nil
}

func readPKCS11PINFile(path string) (string, error) {
	cleanPath := strings.TrimSpace(path)
	if cleanPath == "" {
		return "", fmt.Errorf("%w: file path is required", ErrPKCS11PINRead)
	}
	if !filepath.IsAbs(cleanPath) {
		return "", fmt.Errorf("%w: pin file path must be absolute", ErrPKCS11PINRead)
	}
	if err := validatePKCS11PINFileInfo(cleanPath); err != nil {
		return "", err
	}
	// #nosec G304 -- PIN file path comes from validated local provider configuration.
	file, err := os.Open(cleanPath)
	if err != nil {
		return "", fmt.Errorf("%w: pin file is unreadable", ErrPKCS11PINRead)
	}
	defer func() {
		_ = file.Close()
	}()
	if err := validateOpenedPKCS11PINFile(cleanPath, file); err != nil {
		return "", err
	}
	content, err := io.ReadAll(io.LimitReader(file, maxPKCS11PINFileBytes+1))
	if err != nil {
		return "", fmt.Errorf("%w: pin file is unreadable", ErrPKCS11PINRead)
	}
	if len(content) > maxPKCS11PINFileBytes {
		return "", fmt.Errorf("%w: pin file is too large", ErrPKCS11PINRead)
	}
	pin := strings.TrimRight(string(content), "\r\n")
	if pin == "" {
		return "", fmt.Errorf("%w: pin file is empty", ErrPKCS11PINRead)
	}
	return pin, nil
}

func validatePKCS11PINFileInfo(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("%w: pin file is unreadable", ErrPKCS11PINRead)
	}
	return validatePKCS11PINFileMode(info)
}

func validateOpenedPKCS11PINFile(path string, file *os.File) error {
	openedInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("%w: pin file is unreadable", ErrPKCS11PINRead)
	}
	if err := validatePKCS11PINFileMode(openedInfo); err != nil {
		return err
	}
	currentInfo, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("%w: pin file is unreadable", ErrPKCS11PINRead)
	}
	if err := validatePKCS11PINFileMode(currentInfo); err != nil {
		return err
	}
	if !os.SameFile(openedInfo, currentInfo) {
		return fmt.Errorf("%w: pin file changed while opening", ErrPKCS11PINRead)
	}
	return nil
}

func validatePKCS11PINFileMode(info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: pin file must not be a symlink", ErrPKCS11PINRead)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: pin file must be a regular file", ErrPKCS11PINRead)
	}
	if info.Mode().Perm()&pkcs11PINFileModeMask != 0 {
		return fmt.Errorf("%w: pin file permissions are too broad", ErrPKCS11PINRead)
	}
	return nil
}
