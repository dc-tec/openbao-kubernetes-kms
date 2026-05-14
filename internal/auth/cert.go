package auth

import (
	"bytes"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	maxCertificateFileBytes = 1 << 20
	certificateFileModeMask = os.FileMode(0o037)
	signerProbeMessage      = "openbao-kubernetes-kms/cert-signer-probe/v1"
	spiffeURIScheme         = "spiffe"
)

var (
	// ErrCertificateRead identifies an unreadable or unsafe certificate file.
	ErrCertificateRead = errors.New("certificate read failed")
	// ErrCertificateMalformed identifies a certificate file that cannot be parsed.
	ErrCertificateMalformed = errors.New("certificate malformed")
	// ErrCertificateExpired identifies a certificate whose NotAfter is not in the future.
	ErrCertificateExpired = errors.New("certificate expired")
	// ErrCertificateNearExpiry identifies a certificate too close to expiry for login.
	ErrCertificateNearExpiry = errors.New("certificate near expiry")
	// ErrCertificateNotYetValid identifies a certificate whose NotBefore is in the future.
	ErrCertificateNotYetValid = errors.New("certificate not yet valid")
	// ErrCertificateWeakSignature identifies a certificate using a disallowed signature algorithm.
	ErrCertificateWeakSignature = errors.New("certificate weak signature")
	// ErrCertificateUsage identifies a certificate without client-auth usage.
	ErrCertificateUsage = errors.New("certificate usage invalid")
	// ErrCertificateIdentityMismatch identifies a certificate identity mismatch.
	ErrCertificateIdentityMismatch = errors.New("certificate identity mismatch")
	// ErrCertificateSignerMismatch identifies a signer that does not match the certificate public key.
	ErrCertificateSignerMismatch = errors.New("certificate signer mismatch")
	// ErrCertificateSignerProbe identifies a signer that cannot perform a non-secret probe signature.
	ErrCertificateSignerProbe = errors.New("certificate signer probe failed")
	// ErrPKCS11PINRead identifies an unreadable or unsafe PKCS#11 PIN file.
	ErrPKCS11PINRead = errors.New("pkcs11 pin read failed")
)

// Certificate contains one locally validated client certificate chain.
type Certificate struct {
	Leaf      *x509.Certificate
	Chain     []*x509.Certificate
	ReadAt    time.Time
	ExpiresAt time.Time
}

// CertificateValidationOptions controls local certificate checks before OpenBao login.
type CertificateValidationOptions struct {
	MinRemainingTTL  time.Duration
	ClockSkewLeeway  time.Duration
	ExpectedSPIFFEID string
	TrustDomain      string
	Clock            Clock
}

// ReadAndValidateCertificate reads one PEM certificate chain and enforces local policy.
func ReadAndValidateCertificate(path string, opts CertificateValidationOptions) (Certificate, error) {
	chain, err := ReadCertificateChain(path)
	if err != nil {
		return Certificate{}, err
	}
	if err := ValidateCertificateChain(chain, opts); err != nil {
		return Certificate{}, err
	}
	return Certificate{
		Leaf:      chain[0],
		Chain:     slices.Clone(chain),
		ReadAt:    clockOrReal(opts.Clock).Now(),
		ExpiresAt: chain[0].NotAfter,
	}, nil
}

// ReadCertificateChain reads a PEM X.509 certificate chain from a local file.
func ReadCertificateChain(path string) ([]*x509.Certificate, error) {
	cleanPath := strings.TrimSpace(path)
	if cleanPath == "" {
		return nil, fmt.Errorf("%w: file path is required", ErrCertificateRead)
	}
	if !filepath.IsAbs(cleanPath) {
		return nil, fmt.Errorf("%w: certificate file path must be absolute", ErrCertificateRead)
	}
	if err := validateCertificateFileInfo(cleanPath); err != nil {
		return nil, err
	}

	// #nosec G304 -- certificate file path comes from validated local provider configuration.
	file, err := os.Open(cleanPath)
	if err != nil {
		return nil, fmt.Errorf("%w: certificate file is unreadable", ErrCertificateRead)
	}
	defer func() {
		_ = file.Close()
	}()
	if err := validateOpenedCertificateFile(cleanPath, file); err != nil {
		return nil, err
	}

	content, err := io.ReadAll(io.LimitReader(file, maxCertificateFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: certificate file is unreadable", ErrCertificateRead)
	}
	if len(content) > maxCertificateFileBytes {
		return nil, fmt.Errorf("%w: certificate file is too large", ErrCertificateRead)
	}
	return ParseCertificateChainPEM(content)
}

// ParseCertificateChainPEM parses PEM certificate blocks, preserving leaf-first order.
func ParseCertificateChainPEM(content []byte) ([]*x509.Certificate, error) {
	remaining := bytes.TrimSpace(content)
	if len(remaining) == 0 {
		return nil, fmt.Errorf("%w: certificate file is empty", ErrCertificateMalformed)
	}
	certs := make([]*x509.Certificate, 0, 2)
	for len(remaining) > 0 {
		var block *pem.Block
		block, remaining = pem.Decode(remaining)
		if block == nil {
			return nil, fmt.Errorf("%w: invalid PEM data", ErrCertificateMalformed)
		}
		remaining = bytes.TrimSpace(remaining)
		if block.Type != "CERTIFICATE" {
			return nil, fmt.Errorf("%w: unexpected PEM block type", ErrCertificateMalformed)
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid X.509 certificate", ErrCertificateMalformed)
		}
		certs = append(certs, cert)
	}
	if len(certs) == 0 {
		return nil, fmt.Errorf("%w: no certificate blocks found", ErrCertificateMalformed)
	}
	return certs, nil
}

// ValidateCertificateChain enforces local certificate validity and identity checks.
func ValidateCertificateChain(chain []*x509.Certificate, opts CertificateValidationOptions) error {
	leaf, err := validatedCertificateLeaf(chain)
	if err != nil {
		return err
	}
	if err := validateCertificateSignatureAlgorithms(chain); err != nil {
		return err
	}
	now := clockOrReal(opts.Clock).Now()
	leeway := opts.ClockSkewLeeway
	if leeway < 0 {
		leeway = 0
	}
	latestAcceptedNow := now.Add(leeway)
	if latestAcceptedNow.Before(leaf.NotBefore) {
		return fmt.Errorf("%w: certificate not-before is in the future", ErrCertificateNotYetValid)
	}
	if now.Add(-leeway).After(leaf.NotAfter) || now.Add(-leeway).Equal(leaf.NotAfter) {
		return fmt.Errorf("%w: certificate not-after is not in the future", ErrCertificateExpired)
	}
	if opts.MinRemainingTTL > 0 && leaf.NotAfter.Sub(now) <= opts.MinRemainingTTL {
		return fmt.Errorf("%w: remaining TTL is below minimum", ErrCertificateNearExpiry)
	}
	if !hasClientAuthUsage(leaf.ExtKeyUsage) {
		return fmt.Errorf("%w: client auth EKU is required", ErrCertificateUsage)
	}
	if err := validateCertificateSPIFFEIdentity(leaf, opts); err != nil {
		return err
	}
	return nil
}

func validatedCertificateLeaf(chain []*x509.Certificate) (*x509.Certificate, error) {
	if len(chain) == 0 || chain[0] == nil {
		return nil, fmt.Errorf("%w: certificate chain is empty", ErrCertificateMalformed)
	}
	for _, cert := range chain {
		if cert == nil {
			return nil, fmt.Errorf("%w: certificate chain contains an empty certificate", ErrCertificateMalformed)
		}
	}
	return chain[0], nil
}

func validateCertificateSignatureAlgorithms(chain []*x509.Certificate) error {
	for _, cert := range chain {
		if usesWeakSignature(cert.SignatureAlgorithm) {
			return fmt.Errorf("%w: certificate signature algorithm is disallowed", ErrCertificateWeakSignature)
		}
	}
	return nil
}

// ValidateCertificateSigner checks signer public-key match and performs a non-secret signature probe.
func ValidateCertificateSigner(cert *x509.Certificate, signer crypto.Signer) error {
	if err := validateCertificateSignerPublicKey(cert, signer); err != nil {
		return err
	}
	if err := probeSigner(signer); err != nil {
		return err
	}
	return nil
}

func validateCertificateSignerPublicKey(cert *x509.Certificate, signer crypto.Signer) error {
	if cert == nil {
		return fmt.Errorf("%w: certificate is required", ErrCertificateMalformed)
	}
	if signer == nil {
		return fmt.Errorf("%w: signer is required", ErrCertificateSignerMismatch)
	}
	certPublic, err := x509.MarshalPKIXPublicKey(cert.PublicKey)
	if err != nil {
		return fmt.Errorf("%w: certificate public key is unsupported", ErrCertificateSignerMismatch)
	}
	signerPublic, err := x509.MarshalPKIXPublicKey(signer.Public())
	if err != nil {
		return fmt.Errorf("%w: signer public key is unsupported", ErrCertificateSignerMismatch)
	}
	if !bytes.Equal(certPublic, signerPublic) {
		return fmt.Errorf("%w: signer public key does not match certificate", ErrCertificateSignerMismatch)
	}
	return nil
}

func probeSigner(signer crypto.Signer) error {
	if _, ok := signer.Public().(ed25519.PublicKey); ok {
		if _, err := signer.Sign(rand.Reader, []byte(signerProbeMessage), crypto.Hash(0)); err != nil {
			return fmt.Errorf("%w: signer rejected probe", ErrCertificateSignerProbe)
		}
		return nil
	}
	digest := sha256.Sum256([]byte(signerProbeMessage))
	if _, err := signer.Sign(rand.Reader, digest[:], crypto.SHA256); err != nil {
		return fmt.Errorf("%w: signer rejected probe", ErrCertificateSignerProbe)
	}
	return nil
}

func validateCertificateSPIFFEIdentity(cert *x509.Certificate, opts CertificateValidationOptions) error {
	expectedID := strings.TrimSpace(opts.ExpectedSPIFFEID)
	trustDomain := strings.TrimSpace(opts.TrustDomain)
	if expectedID == "" && trustDomain == "" {
		return nil
	}
	if err := validateExpectedSPIFFEID(expectedID, trustDomain, cert.URIs); err != nil {
		return err
	}
	if err := validateSPIFFETrustDomain(trustDomain, cert.URIs); err != nil {
		return err
	}
	return nil
}

func validateExpectedSPIFFEID(expectedID string, trustDomain string, uris []*url.URL) error {
	if expectedID == "" {
		return nil
	}
	parsed, err := parseSPIFFEURI(expectedID)
	if err != nil {
		return err
	}
	if trustDomain != "" && parsed.Host != trustDomain {
		return ErrCertificateIdentityMismatch
	}
	if !slices.ContainsFunc(uris, func(uri *url.URL) bool {
		return uri != nil && uri.String() == expectedID
	}) {
		return ErrCertificateIdentityMismatch
	}
	return nil
}

func validateSPIFFETrustDomain(trustDomain string, uris []*url.URL) error {
	if trustDomain == "" {
		return nil
	}
	matched := false
	for _, uri := range uris {
		if uri == nil || uri.Scheme != spiffeURIScheme {
			continue
		}
		if uri.Host != trustDomain {
			return ErrCertificateIdentityMismatch
		}
		matched = true
	}
	if !matched {
		return ErrCertificateIdentityMismatch
	}
	return nil
}

func hasClientAuthUsage(usages []x509.ExtKeyUsage) bool {
	return slices.Contains(usages, x509.ExtKeyUsageClientAuth) ||
		slices.Contains(usages, x509.ExtKeyUsageAny)
}

func usesWeakSignature(algorithm x509.SignatureAlgorithm) bool {
	switch algorithm {
	case x509.MD2WithRSA,
		x509.MD5WithRSA,
		x509.SHA1WithRSA,
		x509.DSAWithSHA1,
		x509.ECDSAWithSHA1:
		return true
	default:
		return false
	}
}

func validateCertificateFileInfo(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("%w: certificate file is unreadable", ErrCertificateRead)
	}
	return validateCertificateFileMode(info)
}

func validateOpenedCertificateFile(path string, file *os.File) error {
	openedInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("%w: certificate file is unreadable", ErrCertificateRead)
	}
	if err := validateCertificateFileMode(openedInfo); err != nil {
		return err
	}
	currentInfo, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("%w: certificate file is unreadable", ErrCertificateRead)
	}
	if err := validateCertificateFileMode(currentInfo); err != nil {
		return err
	}
	if !os.SameFile(openedInfo, currentInfo) {
		return fmt.Errorf("%w: certificate file changed while opening", ErrCertificateRead)
	}
	return nil
}

func validateCertificateFileMode(info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: certificate file must not be a symlink", ErrCertificateRead)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: certificate file must be a regular file", ErrCertificateRead)
	}
	if info.Mode().Perm()&certificateFileModeMask != 0 {
		return fmt.Errorf("%w: certificate file permissions are too broad", ErrCertificateRead)
	}
	return nil
}

func parseSPIFFEURI(value string) (*url.URL, error) {
	parsed, err := url.Parse(value)
	if err != nil ||
		parsed.Scheme != spiffeURIScheme ||
		parsed.Host == "" ||
		parsed.Path == "" ||
		parsed.User != nil ||
		parsed.RawQuery != "" ||
		parsed.Fragment != "" {
		return nil, ErrCertificateIdentityMismatch
	}
	return parsed, nil
}
