//go:build certauth_pkcs11

package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"flag"
	"fmt"
	"math/big"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ThalesGroup/crypto11"
)

const (
	defaultKeyBits      = 2048
	defaultOwnerUserID  = 65532
	defaultOwnerGroupID = 65532
)

type setupConfig struct {
	SoftHSMConfigFile string
	TokenDirectory    string
	ModulePath        string
	TokenLabel        string
	KeyLabel          string
	PINFile           string
	CertificateFile   string
	CAFile            string
	SPIFFEID          string
	OwnerUserID       int
	OwnerGroupID      int
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := setupConfig{}
	flag.StringVar(&cfg.SoftHSMConfigFile, "softhsm-config", "/hsm/softhsm2.conf", "SoftHSM configuration file path")
	flag.StringVar(&cfg.TokenDirectory, "token-directory", "/hsm/tokens", "SoftHSM token directory")
	flag.StringVar(&cfg.ModulePath, "module-path", "/usr/lib/softhsm/libsofthsm2.so", "PKCS#11 module path")
	flag.StringVar(&cfg.TokenLabel, "token-label", "openbao-kms-e2e", "SoftHSM token label")
	flag.StringVar(&cfg.KeyLabel, "key-label", "openbao-kms-client", "PKCS#11 key label")
	flag.StringVar(&cfg.PINFile, "pin-file", "/bao/tls/pkcs11-pin", "PIN file to write for the provider")
	flag.StringVar(&cfg.CertificateFile, "certificate-file", "/bao/tls/client-chain.pem", "client certificate chain to write for the provider")
	flag.StringVar(&cfg.CAFile, "ca-file", "/out/client-ca.pem", "client CA certificate to write for OpenBao cert auth")
	flag.StringVar(&cfg.SPIFFEID, "spiffe-id", "spiffe://example.org/openbao-kms/workload-a", "SPIFFE URI SAN for the client certificate")
	flag.IntVar(&cfg.OwnerUserID, "owner-uid", defaultOwnerUserID, "provider runtime UID")
	flag.IntVar(&cfg.OwnerGroupID, "owner-gid", defaultOwnerGroupID, "provider runtime GID")
	flag.Parse()

	if err := validateConfig(cfg); err != nil {
		return err
	}
	pin, err := randomHex(24)
	if err != nil {
		return err
	}
	soPIN, err := randomHex(24)
	if err != nil {
		return err
	}
	if err := writeSoftHSMConfig(cfg); err != nil {
		return err
	}
	if err := initializeToken(cfg, pin, soPIN); err != nil {
		return err
	}
	if err := writeCertificateMaterial(cfg, pin); err != nil {
		return err
	}
	if err := writeProviderPIN(cfg, pin); err != nil {
		return err
	}
	return chownProviderInputs(cfg)
}

func validateConfig(cfg setupConfig) error {
	required := []string{
		cfg.SoftHSMConfigFile,
		cfg.TokenDirectory,
		cfg.ModulePath,
		cfg.TokenLabel,
		cfg.KeyLabel,
		cfg.PINFile,
		cfg.CertificateFile,
		cfg.CAFile,
		cfg.SPIFFEID,
	}
	for _, value := range required {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("all setup paths, labels, and SPIFFE ID are required")
		}
	}
	if _, err := os.Stat(cfg.ModulePath); err != nil {
		return fmt.Errorf("stat PKCS#11 module: %w", err)
	}
	return nil
}

func writeSoftHSMConfig(cfg setupConfig) error {
	if err := os.MkdirAll(cfg.TokenDirectory, 0o700); err != nil {
		return fmt.Errorf("create SoftHSM token directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(cfg.SoftHSMConfigFile), 0o700); err != nil {
		return fmt.Errorf("create SoftHSM config directory: %w", err)
	}
	raw := fmt.Sprintf(`directories.tokendir = %s
objectstore.backend = file
log.level = ERROR
slots.removable = false
`, cfg.TokenDirectory)
	if err := os.WriteFile(cfg.SoftHSMConfigFile, []byte(raw), 0o600); err != nil {
		return fmt.Errorf("write SoftHSM config: %w", err)
	}
	return nil
}

func initializeToken(cfg setupConfig, pin string, soPIN string) error {
	cmd := exec.Command(
		"softhsm2-util",
		"--init-token",
		"--free",
		"--label", cfg.TokenLabel,
		"--so-pin", soPIN,
		"--pin", pin,
	)
	cmd.Env = append(os.Environ(), "SOFTHSM2_CONF="+cfg.SoftHSMConfigFile)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("initialize SoftHSM token: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func writeCertificateMaterial(cfg setupConfig, pin string) error {
	if err := os.Setenv("SOFTHSM2_CONF", cfg.SoftHSMConfigFile); err != nil {
		return fmt.Errorf("set SoftHSM config environment: %w", err)
	}
	ctx, err := crypto11.Configure(&crypto11.Config{
		Path:        cfg.ModulePath,
		TokenLabel:  cfg.TokenLabel,
		Pin:         pin,
		MaxSessions: 4,
	})
	if err != nil {
		return fmt.Errorf("configure PKCS#11 context: %w", err)
	}
	defer func() {
		_ = ctx.Close()
	}()
	signer, err := ctx.GenerateRSAKeyPairWithLabel([]byte("openbao-kms-e2e"), []byte(cfg.KeyLabel), defaultKeyBits)
	if err != nil {
		return fmt.Errorf("generate PKCS#11 RSA key pair: %w", err)
	}

	caKey, err := rsa.GenerateKey(rand.Reader, defaultKeyBits)
	if err != nil {
		return fmt.Errorf("generate test CA key: %w", err)
	}
	leafURI, err := url.Parse(cfg.SPIFFEID)
	if err != nil {
		return fmt.Errorf("parse SPIFFE URI: %w", err)
	}
	now := time.Now().UTC()
	caTemplate, err := certificateTemplate("openbao-kms-e2e-pkcs11-ca", now, 24*time.Hour)
	if err != nil {
		return err
	}
	caTemplate.IsCA = true
	caTemplate.KeyUsage = x509.KeyUsageCertSign | x509.KeyUsageCRLSign

	leafTemplate, err := certificateTemplate("openbao-kms-e2e-pkcs11-client", now, time.Hour)
	if err != nil {
		return err
	}
	leafTemplate.KeyUsage = x509.KeyUsageDigitalSignature
	leafTemplate.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	leafTemplate.URIs = []*url.URL{leafURI}

	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return fmt.Errorf("create test CA certificate: %w", err)
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, caTemplate, signer.Public(), caKey)
	if err != nil {
		return fmt.Errorf("create PKCS#11 client certificate: %w", err)
	}
	if err := writePEMFile(cfg.CAFile, 0o644, pemBlock("CERTIFICATE", caDER)); err != nil {
		return fmt.Errorf("write test CA certificate: %w", err)
	}
	if err := writePEMFile(cfg.CertificateFile, 0o640, pemBlock("CERTIFICATE", leafDER)); err != nil {
		return fmt.Errorf("write provider certificate chain: %w", err)
	}
	return nil
}

func writeProviderPIN(cfg setupConfig, pin string) error {
	if err := os.MkdirAll(filepath.Dir(cfg.PINFile), 0o700); err != nil {
		return fmt.Errorf("create PIN directory: %w", err)
	}
	if err := os.WriteFile(cfg.PINFile, []byte(pin+"\n"), 0o600); err != nil {
		return fmt.Errorf("write PIN file: %w", err)
	}
	return nil
}

func chownProviderInputs(cfg setupConfig) error {
	for _, path := range []string{cfg.TokenDirectory, cfg.SoftHSMConfigFile, cfg.CertificateFile, cfg.PINFile} {
		if err := chownRecursive(path, cfg.OwnerUserID, cfg.OwnerGroupID); err != nil {
			return err
		}
	}
	return nil
}

func chownRecursive(path string, uid int, gid int) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if !info.IsDir() {
		if err := os.Chown(path, uid, gid); err != nil {
			return fmt.Errorf("chown %s: %w", path, err)
		}
		return nil
	}
	return filepath.WalkDir(path, func(current string, _ os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := os.Chown(current, uid, gid); err != nil {
			return fmt.Errorf("chown %s: %w", current, err)
		}
		return nil
	})
}

func certificateTemplate(commonName string, now time.Time, ttl time.Duration) (*x509.Certificate, error) {
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return nil, fmt.Errorf("generate certificate serial: %w", err)
	}
	return &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: commonName,
		},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(ttl),
		BasicConstraintsValid: true,
	}, nil
}

func randomHex(byteCount int) (string, error) {
	buffer := make([]byte, byteCount)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate random setup value: %w", err)
	}
	return hex.EncodeToString(buffer), nil
}

func pemBlock(blockType string, der []byte) *pem.Block {
	return &pem.Block{Type: blockType, Bytes: der}
}

func writePEMFile(path string, mode os.FileMode, blocks ...*pem.Block) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	for _, block := range blocks {
		if err := pem.Encode(file, block); err != nil {
			_ = file.Close()
			return err
		}
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Chmod(path, mode)
}
