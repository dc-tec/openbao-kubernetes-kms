//go:build certauth_pkcs11

package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadPKCS11PINFileRejectsUnsafeMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pin")
	if err := os.WriteFile(path, []byte("1234\n"), 0o644); err != nil {
		t.Fatalf("write pin fixture: %v", err)
	}

	_, err := readPKCS11PINFile(path)
	if !errors.Is(err, ErrPKCS11PINRead) {
		t.Fatalf("expected unsafe PIN file mode, got %v", err)
	}
}

func TestReadPKCS11PINFileRejectsSymlink(t *testing.T) {
	tempDir := t.TempDir()
	target := filepath.Join(tempDir, "pin-target")
	if err := os.WriteFile(target, []byte("1234\n"), 0o600); err != nil {
		t.Fatalf("write pin fixture: %v", err)
	}
	link := filepath.Join(tempDir, "pin")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink pin fixture: %v", err)
	}

	_, err := readPKCS11PINFile(link)
	if !errors.Is(err, ErrPKCS11PINRead) {
		t.Fatalf("expected symlink PIN file failure, got %v", err)
	}
}

func TestReadPKCS11PINFileRejectsEmbeddedLineBreak(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pin")
	if err := os.WriteFile(path, []byte("1234\n5678\n"), 0o600); err != nil {
		t.Fatalf("write pin fixture: %v", err)
	}

	_, err := readPKCS11PINFile(path)
	if !errors.Is(err, ErrPKCS11PINRead) {
		t.Fatalf("expected multi-line PIN file failure, got %v", err)
	}
}

func TestReadPKCS11PINFileRejectsNUL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pin")
	if err := os.WriteFile(path, []byte("1234\x005678\n"), 0o600); err != nil {
		t.Fatalf("write pin fixture: %v", err)
	}

	_, err := readPKCS11PINFile(path)
	if !errors.Is(err, ErrPKCS11PINRead) {
		t.Fatalf("expected NUL-containing PIN file failure, got %v", err)
	}
}

func TestValidatePKCS11ProviderConfigRejectsRelativeModulePath(t *testing.T) {
	cfg := validPKCS11ProviderConfig(t)
	cfg.ModulePath = "libsofthsm2.so"

	_, err := validatePKCS11ProviderConfig(cfg)
	if !errors.Is(err, ErrAuthConfig) {
		t.Fatalf("expected auth config error, got %v", err)
	}
}

func TestValidatePKCS11ProviderConfigRejectsUnsafeLabels(t *testing.T) {
	cfg := validPKCS11ProviderConfig(t)
	cfg.KeyLabel = "openbao-kms\nclient"

	_, err := validatePKCS11ProviderConfig(cfg)
	if !errors.Is(err, ErrAuthConfig) {
		t.Fatalf("expected auth config error, got %v", err)
	}
}

func TestPKCS11ProviderCurrentCertificateRejectsSignerMismatch(t *testing.T) {
	now := time.Unix(testCurrentUnix, 0).UTC()
	pemBytes, _, _ := newCertificateFixture(t, certificateFixtureOptions{
		Now:      now,
		NotAfter: now.Add(time.Hour),
	})
	path := writeCertificateFile(t, pemBytes, 0o600)
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate signer: %v", err)
	}
	provider := &PKCS11CertificateProvider{
		cfg: PKCS11ProviderConfig{
			CertificateFile: path,
		},
		signer: otherKey,
	}

	_, err = provider.CurrentCertificate(t.Context())
	if !errors.Is(err, ErrCertificateSignerMismatch) {
		t.Fatalf("expected signer mismatch, got %v", err)
	}
}

func validPKCS11ProviderConfig(t *testing.T) PKCS11ProviderConfig {
	t.Helper()

	dir := t.TempDir()
	return PKCS11ProviderConfig{
		CertificateFile: filepath.Join(dir, "client-chain.pem"),
		ModulePath:      filepath.Join(dir, "pkcs11.so"),
		TokenLabel:      "openbao-kms",
		KeyLabel:        "openbao-kms-client",
		PINFile:         filepath.Join(dir, "pin"),
		MaxSessions:     2,
	}
}
