package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
	"github.com/dc-tec/openbao-kubernetes-kms/test/fakes"
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

func TestValidateCertificateRejectsExpired(t *testing.T) {
	now := time.Unix(testCurrentUnix, 0).UTC()
	_, cert, _ := newCertificateFixture(t, certificateFixtureOptions{
		Now:       now,
		NotBefore: now.Add(-2 * time.Hour),
		NotAfter:  now.Add(-time.Hour),
	})

	err := ValidateCertificateChain([]*x509.Certificate{cert}, CertificateValidationOptions{
		Clock: &fakeClock{now: now},
	})
	if !errors.Is(err, ErrCertificateExpired) {
		t.Fatalf("expected expired certificate, got %v", err)
	}
}

func TestValidateCertificateRejectsNotYetValidOutsideSkew(t *testing.T) {
	now := time.Unix(testCurrentUnix, 0).UTC()
	_, cert, _ := newCertificateFixture(t, certificateFixtureOptions{
		Now:       now,
		NotBefore: now.Add(time.Hour),
		NotAfter:  now.Add(2 * time.Hour),
	})

	err := ValidateCertificateChain([]*x509.Certificate{cert}, CertificateValidationOptions{
		ClockSkewLeeway: time.Minute,
		Clock:           &fakeClock{now: now},
	})
	if !errors.Is(err, ErrCertificateNotYetValid) {
		t.Fatalf("expected not-yet-valid certificate, got %v", err)
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

func TestValidateCertificateAcceptsAnyExtKeyUsage(t *testing.T) {
	now := time.Unix(testCurrentUnix, 0).UTC()
	_, cert, _ := newCertificateFixture(t, certificateFixtureOptions{
		Now:      now,
		NotAfter: now.Add(time.Hour),
		EKU:      []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	})

	if err := ValidateCertificateChain([]*x509.Certificate{cert}, CertificateValidationOptions{
		Clock: &fakeClock{now: now},
	}); err != nil {
		t.Fatalf("validate any-EKU certificate: %v", err)
	}
}

func TestValidateCertificateRejectsWeakSignatureInChain(t *testing.T) {
	now := time.Unix(testCurrentUnix, 0).UTC()
	_, leaf, _ := newCertificateFixture(t, certificateFixtureOptions{
		Now:      now,
		NotAfter: now.Add(time.Hour),
	})
	intermediate := *leaf
	intermediate.SignatureAlgorithm = x509.SHA1WithRSA

	err := ValidateCertificateChain([]*x509.Certificate{leaf, &intermediate}, CertificateValidationOptions{
		Clock: &fakeClock{now: now},
	})
	if !errors.Is(err, ErrCertificateWeakSignature) {
		t.Fatalf("expected weak chain signature failure, got %v", err)
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

func TestValidateCertificateSignerRejectsProbeFailure(t *testing.T) {
	now := time.Unix(testCurrentUnix, 0).UTC()
	_, cert, signer := newCertificateFixture(t, certificateFixtureOptions{
		Now:      now,
		NotAfter: now.Add(time.Hour),
	})

	err := ValidateCertificateSigner(cert, failingSigner{public: signer.Public()})
	if !errors.Is(err, ErrCertificateSignerProbe) {
		t.Fatalf("expected signer probe failure, got %v", err)
	}
}

func TestCertLoginSourceLogsInWithValidatedCertificate(t *testing.T) {
	now := time.Unix(testCurrentUnix, 0).UTC()
	_, cert, signer := newCertificateFixture(t, certificateFixtureOptions{
		Now:      now,
		NotAfter: now.Add(48 * time.Hour),
		URIs:     []string{testSPIFFEID},
	})
	source, err := NewCertLoginSource(CertLoginSourceConfig{
		MountPath:        "auth/k8s-workload-a-cert",
		Name:             "openbao-kms-control-plane",
		Source:           certSourceSPIFFE,
		MinRemainingTTL:  24 * time.Hour,
		ClockSkewLeeway:  time.Minute,
		ExpectedSPIFFEID: testSPIFFEID,
		TrustDomain:      testTrustDomain,
	}, fakeCertificateProvider{
		cert: tls.Certificate{
			Certificate: [][]byte{cert.Raw},
			PrivateKey:  signer,
		},
	})
	if err != nil {
		t.Fatalf("new cert login source: %v", err)
	}
	client := &fakes.OpenBaoAuthClient{
		CertResponses: []openbao.AuthToken{{
			ClientToken:   testBaoToken1,
			LeaseDuration: time.Hour,
			Renewable:     true,
		}},
	}
	manager, err := NewManagerWithSource(LifecycleConfig{
		LoginBeforeTokenExpiry: testLoginBeforeExpiry,
		TokenRenewalIncrement:  testRenewalIncrement,
	}, source, client, ManagerOptions{
		Clock:              &fakeClock{now: now},
		RefreshRetryJitter: noRetryJitter,
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	token, err := manager.Token(context.Background())
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	if token != testBaoToken1 {
		t.Fatalf("unexpected token")
	}
	logins := client.CertLogins()
	if len(logins) != 1 ||
		logins[0].MountPath != "auth/k8s-workload-a-cert" ||
		logins[0].Name != "openbao-kms-control-plane" {
		t.Fatalf("unexpected cert logins: %#v", logins)
	}
	state := manager.State()
	if state.CertExpiresAt.IsZero() || state.CertTTL <= 0 {
		t.Fatalf("expected certificate TTL state, got %#v", state)
	}
	if state.AuthMethod != authMethodCert || state.CertificateSource != certSourceSPIFFE {
		t.Fatalf("unexpected cert auth source state: %#v", state)
	}
}

func TestCertLoginSourceFailsClosedBeforeOpenBao(t *testing.T) {
	now := time.Unix(testCurrentUnix, 0).UTC()
	_, cert, signer := newCertificateFixture(t, certificateFixtureOptions{
		Now:      now,
		NotAfter: now.Add(48 * time.Hour),
		URIs:     []string{testSPIFFEID},
	})
	source, err := NewCertLoginSource(CertLoginSourceConfig{
		MountPath:        "auth/k8s-workload-a-cert",
		MinRemainingTTL:  24 * time.Hour,
		ClockSkewLeeway:  time.Minute,
		ExpectedSPIFFEID: "spiffe://example.org/openbao-kms/other",
		TrustDomain:      testTrustDomain,
	}, fakeCertificateProvider{
		cert: tls.Certificate{
			Certificate: [][]byte{cert.Raw},
			PrivateKey:  signer,
		},
	})
	if err != nil {
		t.Fatalf("new cert login source: %v", err)
	}
	client := &fakes.OpenBaoAuthClient{}
	manager, err := NewManagerWithSource(LifecycleConfig{
		LoginBeforeTokenExpiry: testLoginBeforeExpiry,
		TokenRenewalIncrement:  testRenewalIncrement,
	}, source, client, ManagerOptions{
		Clock:              &fakeClock{now: now},
		RefreshRetryJitter: noRetryJitter,
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	_, err = manager.Token(context.Background())
	if !errors.Is(err, ErrCertificateIdentityMismatch) {
		t.Fatalf("expected certificate identity mismatch, got %v", err)
	}
	if len(client.CertLogins()) != 0 {
		t.Fatal("invalid certificate should fail before OpenBao login")
	}
}

type certificateFixtureOptions struct {
	Now       time.Time
	NotBefore time.Time
	NotAfter  time.Time
	EKU       []x509.ExtKeyUsage
	URIs      []string
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
	notBefore := opts.NotBefore
	if notBefore.IsZero() {
		notBefore = opts.Now.Add(-time.Minute)
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
		NotBefore:   notBefore,
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

type fakeCertificateProvider struct {
	cert tls.Certificate
	err  error
}

type failingSigner struct {
	public crypto.PublicKey
}

func (s failingSigner) Public() crypto.PublicKey {
	return s.public
}

func (s failingSigner) Sign(io.Reader, []byte, crypto.SignerOpts) ([]byte, error) {
	return nil, errors.New("signer unavailable")
}

func (f fakeCertificateProvider) CurrentCertificate(context.Context) (tls.Certificate, error) {
	if f.err != nil {
		return tls.Certificate{}, f.err
	}
	return f.cert, nil
}
