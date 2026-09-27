package config

import (
	"strings"
	"testing"
)

func oauthConfig(t *testing.T) Config {
	t.Helper()
	cfg, err := Load(NewRuntime(), LoadOptions{Path: "../../test/testdata/config/valid.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	cfg.Auth.JWT.Source = JWTSourceOAuth2
	cfg.Auth.JWT.JWTFile = ""
	cfg.Auth.JWT.ExpectedIssuer = "https://issuer.example"
	cfg.Auth.JWT.ExpectedAudience = []string{"bao"}
	// #nosec G101 -- synthetic client configuration contains a file path, not a secret.
	cfg.Auth.JWT.OAuth2 = OAuth2Config{
		TokenURL: "https://issuer.example/token", ClientID: "provider",
		ClientSecretFile: "/etc/openbao-kms/credentials/client-secret", AuthMethod: "client_secret_basic",
	}
	return cfg
}

func TestOAuth2ConfigRequiresUnambiguousSource(t *testing.T) {
	cfg := oauthConfig(t)
	if err := Validate(cfg, ValidationOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, field string
		change      func(*Config)
	}{
		{"missing source", "auth.jwt.source", func(c *Config) { c.Auth.JWT.Source = "" }},
		{"unknown source", "auth.jwt.source", func(c *Config) { c.Auth.JWT.Source = "keycloak" }},
		{"file fallback", "auth.jwt.jwtFile", func(c *Config) { c.Auth.JWT.JWTFile = "/run/identity.jwt" }},
		{"file with oauth config", "auth.jwt.oauth2", func(c *Config) {
			c.Auth.JWT.Source = JWTSourceFile
			c.Auth.JWT.JWTFile = "/run/identity.jwt"
		}},
		{"missing issuer", "auth.jwt.expectedIssuer", func(c *Config) { c.Auth.JWT.ExpectedIssuer = "" }},
		{"missing audience", "auth.jwt.expectedAudience", func(c *Config) { c.Auth.JWT.ExpectedAudience = nil }},
		{"missing method", "auth.jwt.oauth2", func(c *Config) { c.Auth.JWT.OAuth2.AuthMethod = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := cfg
			tc.change(&c)
			err := Validate(c, ValidationOptions{})
			if err == nil || !strings.Contains(err.Error(), tc.field) {
				t.Fatalf("unexpected validation: %v", err)
			}
		})
	}
}
