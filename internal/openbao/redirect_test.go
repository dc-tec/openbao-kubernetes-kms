package openbao

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Exercise the public clients so redirects cannot forward tokens, login JWTs,
// or Transit plaintext, even when the destination would pass TLS validation.
func TestOpenBaoClientsRejectRedirects(t *testing.T) {
	for _, operation := range []string{"encrypt", "jwt-login"} {
		for _, destination := range []string{"http", "https", "same-origin"} {
			for _, code := range []int{301, 302, 303, 307, 308} {
				t.Run(fmt.Sprintf("%s/%s/%d", operation, destination, code), func(t *testing.T) {
					testOpenBaoRedirect(t, operation, destination, code)
				})
			}
		}
	}
}

func testOpenBaoRedirect(t *testing.T, operation, destination string, code int) {
	t.Helper()
	var destinationRequests atomic.Int32
	sink := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		destinationRequests.Add(1)
		_, _ = w.Write([]byte(`{
			"data":{"ciphertext":"vault:v1:redirected","key_version":1},
			"auth":{"client_token":"redirected","lease_duration":600,"renewable":true}
		}`))
	})
	location := "/redirect-target?credential=redirect-only-marker"
	if destination != "same-origin" {
		var target *httptest.Server
		if destination == "https" {
			target = httptest.NewTLSServer(sink)
		} else {
			target = httptest.NewServer(sink)
		}
		t.Cleanup(target.Close)
		location = target.URL + location
	}
	var originRequests atomic.Int32
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect-target" {
			sink.ServeHTTP(w, r)
			return
		}
		originRequests.Add(1)
		http.Redirect(w, r, location, code)
	}))
	t.Cleanup(origin.Close)

	err := callRedirectingOpenBao(t, operation, origin)
	if got := destinationRequests.Load(); got != 0 {
		t.Fatalf("redirect destination received %d requests; sensitive material can leave the configured endpoint", got)
	}
	if got := originRequests.Load(); got != 1 {
		t.Fatalf("expected one request to configured endpoint, got %d", got)
	}
	var openBaoErr *Error
	if !errors.As(err, &openBaoErr) || openBaoErr.Class != ErrorClassUnavailable {
		t.Fatalf("expected unavailable for redirect, got %v", err)
	}
	if openBaoErr.StatusCode != code {
		t.Fatalf("expected original HTTP status %d, got %d", code, openBaoErr.StatusCode)
	}
	for _, sensitive := range []string{testToken, testPlaintext, "redirect-test-jwt", "redirect-only-marker", location} {
		if strings.Contains(err.Error(), sensitive) {
			t.Fatal("redirect error contains sensitive request or destination data")
		}
	}
}

func callRedirectingOpenBao(t *testing.T, operation string, server *httptest.Server) error {
	t.Helper()
	caFile := writeServerCAFile(t, server)
	address, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if operation == "jwt-login" {
		client, clientErr := NewAuthClient(AuthClientConfig{
			Address: server.URL, CACertFile: caFile, TLSServerName: address.Hostname(), Timeout: time.Second,
		})
		if clientErr != nil {
			t.Fatal(clientErr)
		}
		t.Cleanup(client.CloseIdleConnections)
		_, err = client.LoginJWT(context.Background(), JWTLoginRequest{
			MountPath: "auth/jwt", Role: "kms", JWT: "redirect-test-jwt",
		})
		return err
	}
	client, err := NewClient(ClientConfig{
		Address: server.URL, CACertFile: caFile, TLSServerName: address.Hostname(), Timeout: time.Second,
		TokenSource: StaticTokenSource{TokenValue: testToken},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.httpClient.CloseIdleConnections)
	_, err = client.Encrypt(context.Background(), EncryptRequest{
		MountPath: testMountPath, KeyName: testKeyName, Plaintext: []byte(testPlaintext),
		AssociatedData: []byte(testAAD), KeyVersion: 1,
	})
	return err
}
