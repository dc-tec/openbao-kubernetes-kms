package openbao

import (
	"context"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

const (
	testNamespace  = "admin"
	testToken      = "test-token"
	testMountPath  = "transit"
	testKeyName    = "k8s"
	testPlaintext  = "plain"
	testAAD        = "aad"
	testCiphertext = "vault:v1:ciphertext"
)

func TestStaticTokenSourceRequiresToken(t *testing.T) {
	if _, err := (StaticTokenSource{}).Token(context.Background()); err == nil {
		t.Fatal("expected empty static token to fail")
	}

	token, err := (StaticTokenSource{TokenValue: testToken}).Token(context.Background())
	if err != nil {
		t.Fatalf("read static token: %v", err)
	}
	if token != testToken {
		t.Fatalf("unexpected token: %q", token)
	}
}

func TestNewClientWithHTTPClientValidatesInputs(t *testing.T) {
	_, err := NewClientWithHTTPClient(ClientConfig{
		Address:     "https://bao.example.internal:8200",
		TokenSource: StaticTokenSource{TokenValue: testToken},
	}, nil)
	if err == nil {
		t.Fatal("expected nil HTTP client to fail")
	}

	_, err = NewClientWithHTTPClient(ClientConfig{
		Address: "https://bao.example.internal:8200",
	}, &http.Client{})
	if err == nil {
		t.Fatal("expected missing token source to fail")
	}
}

func TestNewClientRejectsUnsafeAddresses(t *testing.T) {
	for _, address := range []string{
		"http://bao.example.internal:8200",
		"https://",
		"https://user:pass@bao.example.internal:8200",
	} {
		t.Run(address, func(t *testing.T) {
			_, err := NewClientWithHTTPClient(ClientConfig{
				Address:     address,
				TokenSource: StaticTokenSource{TokenValue: testToken},
			}, &http.Client{})
			if err == nil {
				t.Fatal("expected unsafe address to fail")
			}
		})
	}
}

func TestNewClientUsesCAAndServerNameValidation(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(t, r, http.MethodGet, "/v1/transit/config/keys")
		_, _ = w.Write([]byte(`{"data":{"disable_upsert":true}}`))
	}))
	t.Cleanup(server.Close)

	caFile := writeServerCAFile(t, server)
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	client, err := NewClient(ClientConfig{
		Address:       server.URL,
		Namespace:     testNamespace,
		CACertFile:    caFile,
		TLSServerName: parsed.Hostname(),
		Timeout:       time.Second,
		TokenSource:   StaticTokenSource{TokenValue: testToken},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if disableUpsert, err := client.ReadDisableUpsert(context.Background(), testMountPath); err != nil || !disableUpsert {
		t.Fatalf("read disable_upsert through TLS client: %v, %v", disableUpsert, err)
	}

	mismatchClient, err := NewClient(ClientConfig{
		Address:       server.URL,
		Namespace:     testNamespace,
		CACertFile:    caFile,
		TLSServerName: "wrong.example.internal",
		Timeout:       time.Second,
		TokenSource:   StaticTokenSource{TokenValue: testToken},
	})
	if err != nil {
		t.Fatalf("new mismatch client: %v", err)
	}
	_, err = mismatchClient.ReadDisableUpsert(context.Background(), testMountPath)
	var openBaoErr *Error
	if !errors.As(err, &openBaoErr) || openBaoErr.Class != ErrorClassUnavailable {
		t.Fatalf("expected unavailable on TLS name mismatch, got %v", err)
	}
}

func TestClientRequestFailureIsRedacted(t *testing.T) {
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/v1/sys/capabilities-self" {
			t.Fatalf("unexpected path: %s", req.URL.Path)
		}
		if req.Header.Get(vaultTokenHeader) != testToken {
			t.Fatal("missing token header")
		}
		if req.Header.Get(contentTypeHeader) != contentTypeJSON {
			t.Fatalf("unexpected content type: %s", req.Header.Get(contentTypeHeader))
		}
		return &http.Response{
			StatusCode: http.StatusForbidden,
			Body: io.NopCloser(strings.NewReader(
				`{"errors":["permission denied for transit/keys/k8s with token test-token"]}`,
			)),
			Header: make(http.Header),
		}, nil
	})
	client, err := NewClientWithHTTPClient(ClientConfig{
		Address:     "https://bao.example.internal",
		Namespace:   testNamespace,
		TokenSource: StaticTokenSource{TokenValue: testToken},
	}, &http.Client{Transport: transport})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	_, err = client.Capabilities(context.Background(), []string{"transit/keys/k8s"})
	var openBaoErr *Error
	if !errors.As(err, &openBaoErr) || openBaoErr.Class != ErrorClassPermissionDenied {
		t.Fatalf("expected permission denied error, got %v", err)
	}
	if got := openBaoErr.Error(); strings.Contains(got, testToken) || strings.Contains(got, testKeyName) {
		t.Fatalf("error was not redacted: %q", got)
	}
	messages := openBaoErr.Messages()
	messages[0] = "modified"
	if openBaoErr.Messages()[0] == "modified" {
		t.Fatal("expected Messages to return a copy")
	}
}

func TestClientResolvePreservesBasePath(t *testing.T) {
	client, err := NewClientWithHTTPClient(ClientConfig{
		Address:     "https://bao.example.internal/base",
		TokenSource: StaticTokenSource{TokenValue: testToken},
	}, &http.Client{})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	got := client.resolve("sys/health")
	want := "https://bao.example.internal/base/v1/sys/health"
	if got != want {
		t.Fatalf("unexpected resolved URL: got %q, want %q", got, want)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func writeServerCAFile(t *testing.T, server *httptest.Server) string {
	t.Helper()

	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: server.Certificate().Raw,
	})
	if certPEM == nil {
		t.Fatal("encode server certificate")
	}
	path := t.TempDir() + "/ca.pem"
	if err := os.WriteFile(path, certPEM, 0o600); err != nil {
		t.Fatalf("write CA file: %v", err)
	}
	return path
}
