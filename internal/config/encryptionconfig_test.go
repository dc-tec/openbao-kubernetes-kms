package config

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

const migrationKeySecret = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=" // #nosec G101 -- public synthetic test key.

func migrationEncryptionConfig(cfg Config, extra string) string {
	return fmt.Sprintf(`apiVersion: apiserver.config.k8s.io/v1
kind: EncryptionConfiguration
resources:
- resources: [secrets]
  providers:
  - kms:
      apiVersion: v2
      name: %s
      endpoint: unix://%s
%s`, cfg.Transit.KeyIDScope.ProviderName, cfg.Server.SocketPath, extra)
}

func TestEncryptionConfigurationAcceptsMigrationProviders(t *testing.T) {
	cfg := loadValidConfig(t)
	for _, extra := range []string{
		"  - aescbc:\n      keys:\n      - name: old-key\n        secret: " + migrationKeySecret,
		"  - aesgcm:\n      keys:\n      - name: old-key\n        secret: " + migrationKeySecret,
		"  - secretbox:\n      keys:\n      - name: old-key\n        secret: " + migrationKeySecret,
		"  - kms:\n      apiVersion: v2\n      name: old-kms\n      endpoint: unix:///run/old-kms.sock",
		"  - kms:\n      apiVersion: v1\n      cachesize: 1000\n" +
			"      name: old-kms\n      endpoint: unix:///run/old-kms.sock",
	} {
		parsed, err := ParseEncryptionConfiguration(strings.NewReader(migrationEncryptionConfig(cfg, extra)))
		if err != nil {
			t.Fatalf("parse migration config: %v", err)
		}
		for range 2 {
			result, err := ValidateEncryptionConfiguration(cfg, parsed, EncryptionValidationOptions{})
			if err != nil || result.MatchedProviderName != cfg.Transit.KeyIDScope.ProviderName {
				t.Fatalf("validate migration config: %v", err)
			}
			// The local provider may follow the old provider during staged migration.
			providers := parsed.Resources[0].Providers
			providers[0], providers[1] = providers[1], providers[0]
		}
	}
}

func TestEncryptionConfigurationRejectsMigrationDrift(t *testing.T) {
	cfg := loadValidConfig(t)
	base := migrationEncryptionConfig(cfg,
		"  - kms:\n      apiVersion: v2\n      name: old-kms\n      endpoint: unix:///run/old-kms.sock")
	for _, tc := range []struct {
		name   string
		change func(string) string
	}{
		{"target absent", func(s string) string {
			return strings.Replace(s, cfg.Transit.KeyIDScope.ProviderName, "unrelated", 1)
		}},
		{"target endpoint", func(s string) string { return strings.Replace(s, cfg.Server.SocketPath, "/run/wrong.sock", 1) }},
		{"target api", func(s string) string { return strings.Replace(s, "apiVersion: v2", "apiVersion: v1", 1) }},
		{"duplicate mismatched target", func(s string) string {
			return strings.Replace(s, "name: old-kms", "name: "+cfg.Transit.KeyIDScope.ProviderName, 1)
		}},
		{"multiple provider types", func(s string) string {
			return strings.Replace(s, "  - kms:", "  - identity: {}\n    kms:", 1)
		}},
		{"v2 cache size", func(s string) string {
			return strings.Replace(s, "apiVersion: v2", "apiVersion: v2\n      cachesize: 1000", 1)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := ParseEncryptionConfiguration(strings.NewReader(tc.change(base)))
			if err != nil {
				t.Fatal(err)
			}
			_, err = ValidateEncryptionConfiguration(cfg, parsed, EncryptionValidationOptions{})
			if !errors.Is(err, ErrInvalidEncryptionConfiguration) {
				t.Fatalf("accepted invalid migration: %v", err)
			}
		})
	}
}

func TestEncryptionConfigurationRejectsMalformedInputWithoutKeyDisclosure(t *testing.T) {
	cfg := loadValidConfig(t)
	for _, extra := range []string{
		"  - aescbc:\n      keys:\n      - name: old-key\n        secret: " + migrationKeySecret + "\n        typo: value",
		"  - aescbc:\n      keys: " + migrationKeySecret,
		"---\nkind: EncryptionConfiguration",
		"  - unknown: {}",
	} {
		_, err := ParseEncryptionConfiguration(strings.NewReader(migrationEncryptionConfig(cfg, extra)))
		if !errors.Is(err, ErrInvalidEncryptionConfiguration) {
			t.Fatalf("accepted malformed input: %v", err)
		}
		if strings.Contains(err.Error(), migrationKeySecret) {
			t.Fatal("parser exposed encryption key")
		}
	}
	key := EncryptionKey{Name: "old-key", Secret: migrationKeySecret}
	if strings.Contains(fmt.Sprintf("%v %+v %#v", key, key, key), migrationKeySecret) {
		t.Fatal("key formatting exposed secret")
	}
}
