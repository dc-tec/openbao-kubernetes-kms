package main

import (
	"path/filepath"
	"testing"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/config"
)

func TestGeneratedProviderConfigValidates(t *testing.T) {
	for _, tc := range []struct {
		name, socketGroup string
	}{
		{"systemd", "openbao-kms-socket"},
		{"static-pod", "1234"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lab := &labConfig{providerAssetDir: t.TempDir()}
			if err := writeProviderConfig(lab, "provider.yaml", tc.socketGroup, "harvester-test", "192.0.2.10"); err != nil {
				t.Fatal(err)
			}
			cfg, err := config.Load(config.NewFileRuntime(), config.LoadOptions{
				Path: filepath.Join(lab.providerAssetDir, "provider.yaml"),
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := config.Validate(cfg, config.ValidationOptions{}); err != nil {
				t.Fatal(err)
			}
			if cfg.Auth.JWT.Source != config.JWTSourceFile || cfg.Server.SocketGroup != tc.socketGroup {
				t.Fatal("generated config lost the host JWT source or socket group")
			}
			if tc.name == "static-pod" {
				sample, err := config.Load(config.NewFileRuntime(), config.LoadOptions{
					Path: filepath.Join("..", "..", "..", "deploy", "config", "provider-static-pod.yaml"),
				})
				if err != nil {
					t.Fatal(err)
				}
				if cfg.Auth.JWT.JWTFile != sample.Auth.JWT.JWTFile {
					t.Fatal("lab JWT path differs from the static-pod deployment sample")
				}
			}
			encryptionPath := filepath.Join(lab.providerAssetDir, "encryption.yaml")
			if err := writeEncryptionConfig(encryptionPath); err != nil {
				t.Fatal(err)
			}
			encryption, err := config.LoadEncryptionConfiguration(encryptionPath)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := config.ValidateEncryptionConfiguration(cfg, encryption, config.EncryptionValidationOptions{
				AllowIdentityFallback: true,
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
