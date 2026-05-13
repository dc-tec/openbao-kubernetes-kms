package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const (
	testSPIFFEID       = "spiffe://example.org/openbao-kms/workload-a"
	testTrustDomain    = "example.org"
	testCertCommonName = "openbao-kms-control-plane"
)

func TestReadAndValidateCertificate(t *testing.T) {
	now := time.Unix(testCurrentUnix, 0).UTC()
	pemBytes, cert, signer := newCertificateFixture(t, certificateFixtureOptions{
		Now:      now,
		NotAfter: now.Add(48 * time.Hour),
		URIs:     []string{testSPIFFEID},
	})
	path := writeCertificateFile(t, pemBytes, 0o600)

	got, err := ReadAndValidateCertificate(path, CertificateValidationOptions{
		MinRemainingTTL:  24 * time.Hour,
		ClockSkewLeeway:  time.Minute,
		ExpectedSPIFFEID: testSPIFFEID,
		TrustDomain:      testTrustDomain,
		Clock:            &fakeClock{now: now},
	})
	if err != nil {
		t.Fatalf("validate certificate: %v", err)
	}
	if got.Leaf.SerialNumber.Cmp(cert.SerialNumber) != 0 {
		t.Fatalf("unexpected certificate leaf")
	}
	if err := ValidateCertificateSigner(got.Leaf, signer); err != nil {
		t.Fatalf("validate signer: %v", err)
	}
}

func TestValidateCertificateRejectsNearExpiry(t *testing.T) {
	now := time.Unix(testCurrentUnix, 0).UTC()
	_, cert, _ := newCertificateFixture(t, certificateFixtureOptions{
		Now:      now,
		NotAfter: now.Add(time.Hour),
	})

	err := ValidateCertificateChain([]*x509.Certificate{cert}, CertificateValidationOptions{
		MinRemainingTTL: 2 * time.Hour,
		Clock:           &fakeClock{now: now},
	})
	if !errors.Is(err, ErrCertificateNearExpiry) {
		t.Fatalf("expected near-expiry certificate, got %v", err)
	}
}

func TestValidateCertificateRequiresClientAuthUsage(t *testing.T) {
	now := time.Unix(testCurrentUnix, 0).UTC()
	_, cert, _ := newCertificateFixture(t, certificateFixtureOptions{
		Now:      now,
		NotAfter: now.Add(time.Hour),
		EKU:      []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	})

	err := ValidateCertificateChain([]*x509.Certificate{cert}, CertificateValidationOptions{
		Clock: &fakeClock{now: now},
	})
	if !errors.Is(err, ErrCertificateUsage) {
		t.Fatalf("expected certificate usage failure, got %v", err)
	}
}

func TestValidateCertificateSPIFFEIdentity(t *testing.T) {
	now := time.Unix(testCurrentUnix, 0).UTC()
	_, cert, _ := newCertificateFixture(t, certificateFixtureOptions{
		Now:      now,
		NotAfter: now.Add(time.Hour),
		URIs:     []string{testSPIFFEID},
	})

	err := ValidateCertificateChain([]*x509.Certificate{cert}, CertificateValidationOptions{
		ExpectedSPIFFEID: "spiffe://example.org/openbao-kms/other",
		TrustDomain:      testTrustDomain,
		Clock:            &fakeClock{now: now},
	})
	if !errors.Is(err, ErrCertificateIdentityMismatch) {
		t.Fatalf("expected spiffe identity mismatch, got %v", err)
	}
}

func TestReadCertificateRejectsUnsafeFile(t *testing.T) {
	now := time.Unix(testCurrentUnix, 0).UTC()
	pemBytes, _, _ := newCertificateFixture(t, certificateFixtureOptions{
		Now:      now,
		NotAfter: now.Add(time.Hour),
	})
	path := writeCertificateFile(t, pemBytes, 0o644)

	_, err := ReadAndValidateCertificate(path, CertificateValidationOptions{
		Clock: &fakeClock{now: now},
	})
	if !errors.Is(err, ErrCertificateRead) {
		t.Fatalf("expected certificate read error, got %v", err)
	}
}

func TestReadCertificateRejectsSymlink(t *testing.T) {
	now := time.Unix(testCurrentUnix, 0).UTC()
	pemBytes, _, _ := newCertificateFixture(t, certificateFixtureOptions{
		Now:      now,
		NotAfter: now.Add(time.Hour),
	})
	tempDir := t.TempDir()
	target := filepath.Join(tempDir, "client.crt")
	if err := os.WriteFile(target, pemBytes, 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	link := filepath.Join(tempDir, "identity.crt")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink cert: %v", err)
	}

	_, err := ReadAndValidateCertificate(link, CertificateValidationOptions{
		Clock: &fakeClock{now: now},
	})
	if !errors.Is(err, ErrCertificateRead) {
		t.Fatalf("expected certificate read error, got %v", err)
	}
}

func TestValidateCertificateSignerRejectsMismatch(t *testing.T) {
	now := time.Unix(testCurrentUnix, 0).UTC()
	_, cert, _ := newCertificateFixture(t, certificateFixtureOptions{
		Now:      now,
		NotAfter: now.Add(time.Hour),
	})
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate signer: %v", err)
	}

	err = ValidateCertificateSigner(cert, otherKey)
	if !errors.Is(err, ErrCertificateSignerMismatch) {
		t.Fatalf("expected signer mismatch, got %v", err)
	}
}

type certificateFixtureOptions struct {
	Now      time.Time
	NotAfter time.Time
	EKU      []x509.ExtKeyUsage
	URIs     []string
}

func newCertificateFixture(
	t *testing.T,
	opts certificateFixtureOptions,
) ([]byte, *x509.Certificate, *rsa.PrivateKey) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	eku := opts.EKU
	if eku == nil {
		eku = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	}
	notAfter := opts.NotAfter
	if notAfter.IsZero() {
		notAfter = opts.Now.Add(time.Hour)
	}
	uris := make([]*url.URL, 0, len(opts.URIs))
	for _, rawURI := range opts.URIs {
		parsed, err := url.Parse(rawURI)
		if err != nil {
			t.Fatalf("parse uri: %v", err)
		}
		uris = append(uris, parsed)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(42),
		Subject: pkix.Name{
			CommonName: testCertCommonName,
		},
		NotBefore:   opts.Now.Add(-time.Minute),
		NotAfter:    notAfter,
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: eku,
		URIs:        uris,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if pemBytes == nil {
		t.Fatal("encode certificate")
	}
	return pemBytes, cert, key
}

func writeCertificateFile(t *testing.T, content []byte, mode os.FileMode) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "client.crt")
	if err := os.WriteFile(path, content, mode); err != nil {
		t.Fatalf("write certificate: %v", err)
	}
	return path
}
