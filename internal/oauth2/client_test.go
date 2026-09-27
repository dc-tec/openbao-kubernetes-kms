package oauth2

import (
	"context"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	testClientID     = "client:+ with space"
	testClientSecret = "secret:+ /&=" // #nosec G101 -- deliberate synthetic credential for form encoding tests.
)

func testConfig(t *testing.T, handler http.HandlerFunc) Config {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	dir := t.TempDir()
	ca := filepath.Join(dir, "ca.pem")
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := os.WriteFile(ca, caPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(dir, "secret")
	if err := os.WriteFile(secret, []byte(testClientSecret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return Config{
		TokenURL: server.URL + "/token?tenant=example", ClientID: testClientID, ClientSecretFile: secret,
		AuthMethod: ClientSecretBasic, CACertFile: ca, Timeout: time.Second,
	}
}

func TestClientCredentialsMethodsAndParameters(t *testing.T) {
	for _, method := range []string{ClientSecretBasic, ClientSecretPost} {
		t.Run(method, func(t *testing.T) {
			cfg := testConfig(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/token" || r.URL.Query().Get("tenant") != "example" {
					t.Error("unexpected token request target")
				}
				r.Body = http.MaxBytesReader(w, r.Body, 4096)
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				if r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
					t.Error("unexpected request encoding")
				}
				assertTokenParameters(t, r, method)
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				_, _ = w.Write([]byte(`{"access_token":"header.payload.signature","token_type":"bearer",
 "expires_in":"300","vendor_extension":{"ok":true}}`))
			})
			cfg.AuthMethod = method
			cfg.Scopes = []string{"read", "write"}
			cfg.Audience = "bao"
			cfg.Resources = []string{"https://resource.example/a", "urn:resource:b"}
			client, err := NewClient(cfg)
			if err != nil {
				t.Fatal(err)
			}
			token, err := client.Token(t.Context())
			if err != nil || token.AccessToken == "" || token.ExpiresIn != 5*time.Minute {
				t.Fatalf("token acquisition failed: %v", err)
			}
		})
	}
}

func TestClientRereadsAtomicallyReplacedSecret(t *testing.T) {
	var calls atomic.Int32
	cfg := testConfig(t, func(w http.ResponseWriter, r *http.Request) {
		_, secret, _ := r.BasicAuth()
		expected := testClientSecret
		if calls.Add(1) == 2 {
			expected = "replacement"
		}
		if secret != url.QueryEscape(expected) {
			t.Error("client did not reread rotated credential")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"jwt","token_type":"Bearer"}`))
	})
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Token(t.Context()); err != nil {
		t.Fatal(err)
	}
	replacement := cfg.ClientSecretFile + ".new"
	if err := os.WriteFile(replacement, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, cfg.ClientSecretFile); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Token(t.Context()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatal("unexpected acquisition count")
	}
}

func TestClientRejectsResponsesWithoutLeakingOrRetrying(t *testing.T) {
	cases := []struct {
		name              string
		status            int
		contentType, body string
		want              error
	}{
		{"unauthorized", 401, "application/json", `{"error_description":"sensitive upstream data"}`, ErrRejected},
		{"unavailable", 503, "text/plain", "sensitive upstream data", ErrRejected},
		{"redirect", 307, "text/plain", "sensitive upstream data", ErrRejected},
		{"html", 200, "text/html", "sensitive upstream data", ErrResponse},
		{"malformed", 200, "application/json", "sensitive upstream data", ErrResponse},
		{"missing access token", 200, "application/json", `{"token_type":"Bearer"}`, ErrResponse},
		{"wrong type", 200, "application/json", `{"access_token":"private","token_type":"DPoP"}`, ErrResponse},
		{"missing type", 200, "application/json", `{"access_token":"sensitive upstream data"}`, ErrResponse},
		{"expired", 200, "application/json", `{"access_token":"jwt","token_type":"Bearer","expires_in":0}`, ErrResponse},
		{
			"overflow", 200, "application/json",
			`{"access_token":"jwt","token_type":"Bearer","expires_in":9223372036854775807}`, ErrResponse,
		},
		{"fraction", 200, "application/json", `{"access_token":"jwt","token_type":"Bearer","expires_in":0.5}`, ErrResponse},
		{"trailing data", 200, "application/json", `{"access_token":"jwt","token_type":"Bearer"}{}`, ErrResponse},
		{"oversized", 200, "application/json", strings.Repeat("x", maxResponseBytes+1), ErrResponse},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			cfg := testConfig(t, func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", tc.contentType)
				w.Header().Set("Location", "/leak")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			client, err := NewClient(cfg)
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Token(t.Context())
			if !errors.Is(err, tc.want) || strings.Contains(err.Error(), "sensitive") {
				t.Fatalf("unexpected public error: %v", err)
			}
			if calls.Load() != 1 {
				t.Fatal("client followed redirect or retried with another method")
			}
		})
	}
}

func TestClientEnforcesTLSAndDeadline(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	cfg := testConfig(t, func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	})
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	if _, err := client.Token(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline not propagated: %v", err)
	}
	cfg.CACertFile = ""
	client, err = NewClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Token(t.Context()); !errors.Is(err, ErrRequest) {
		t.Fatalf("untrusted TLS accepted: %v", err)
	}
}

func TestDecodeTokenOptionalExpiration(t *testing.T) {
	token, err := decodeToken(strings.NewReader(`{"access_token":"jwt","token_type":"Bearer"}`))
	if err != nil || token.ExpiresIn != 0 {
		t.Fatalf("optional expires_in rejected: %v", err)
	}
}

func TestValidateConfig(t *testing.T) {
	// #nosec G101 -- credential path used only for configuration validation.
	valid := Config{
		TokenURL: "https://issuer.example/token", ClientID: "client", ClientSecretFile: "/run/secrets/client",
		AuthMethod: ClientSecretBasic, Timeout: time.Second,
	}
	cases := []struct {
		name   string
		change func(*Config)
	}{
		{"http", func(c *Config) { c.TokenURL = "http://issuer.example/token" }},
		{"userinfo", func(c *Config) { c.TokenURL = "https://user:password@issuer.example/token" }},
		{"fragment", func(c *Config) { c.TokenURL += "#x" }},
		{"empty client", func(c *Config) { c.ClientID = "" }},
		{"relative secret", func(c *Config) { c.ClientSecretFile = "secret" }},
		{"implicit method", func(c *Config) { c.AuthMethod = "" }},
		{"invalid scope", func(c *Config) { c.Scopes = []string{"two scopes"} }},
		{"relative resource", func(c *Config) { c.Resources = []string{"resource"} }},
		{"resource fragment", func(c *Config) { c.Resources = []string{"https://resource.example/#x"} }},
		{"timeout", func(c *Config) { c.Timeout = 0 }},
	}
	if err := ValidateConfig(valid); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := valid
			tc.change(&cfg)
			if err := ValidateConfig(cfg); err == nil {
				t.Fatal("unsafe config accepted")
			}
		})
	}
}

func TestReadClientSecretSafety(t *testing.T) {
	for _, tc := range []struct {
		name, content string
		mode          os.FileMode
	}{
		{"empty", "", 0o600},
		{"multiline", "one\ntwo", 0o600},
		{"nul", "one\x00two", 0o600},
		{"world readable", "secret", 0o644},
		{"group writable", "secret", 0o660},
		{"executable", "secret", 0o700},
		{"oversized", strings.Repeat("x", maxSecretBytes+1), 0o600},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "credential")
			if err := os.WriteFile(path, []byte(tc.content), tc.mode); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, tc.mode); err != nil {
				t.Fatal(err)
			}
			if _, err := ReadClientSecret(path); !errors.Is(err, ErrCredential) {
				t.Fatal("unsafe credential accepted")
			}
		})
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{link, dir, filepath.Join(dir, "missing"), "relative"} {
		if _, err := ReadClientSecret(path); !errors.Is(err, ErrCredential) {
			t.Fatal("unsafe credential path accepted")
		}
	}
}

func assertTokenParameters(t *testing.T, r *http.Request, method string) {
	t.Helper()

	want := url.Values{
		"grant_type": {"client_credentials"}, "scope": {"read write"}, "audience": {"bao"},
		"resource": {"https://resource.example/a", "urn:resource:b"},
	}
	if method == ClientSecretBasic {
		id, secret, ok := r.BasicAuth()
		if !ok || id != url.QueryEscape(testClientID) || secret != url.QueryEscape(testClientSecret) {
			t.Error("incorrect Basic encoding")
		}
	} else {
		if r.Header.Get("Authorization") != "" {
			t.Error("multiple client authentication methods")
		}
		want.Set("client_id", testClientID)
		want.Set("client_secret", testClientSecret)
	}
	if !reflect.DeepEqual(r.PostForm, want) {
		t.Error("incorrect token request parameters")
	}
}
