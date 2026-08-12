package openbao

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
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
		"https://bao.example.internal:8200?token=test-token",
		"https://bao.example.internal:8200#fragment",
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

func TestNewAuthClientUsesClientCertificateCallback(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || len(r.TLS.PeerCertificates) != 1 {
			t.Fatalf("expected one TLS peer certificate, got %#v", r.TLS)
		}
		_, _ = w.Write([]byte(`{
			"auth": {
				"client_token": "` + testToken + `",
				"lease_duration": 600,
				"renewable": true
			}
		}`))
	}))
	server.TLS = &tls.Config{
		MinVersion: tls.VersionTLS12,
		ClientAuth: tls.RequireAnyClientCert,
	}
	server.StartTLS()
	t.Cleanup(server.Close)

	caFile := writeServerCAFile(t, server)
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	cert := newClientTLSCertificate(t)
	client, err := NewAuthClient(AuthClientConfig{
		Address:       server.URL,
		CACertFile:    caFile,
		TLSServerName: parsed.Hostname(),
		Timeout:       time.Second,
		GetClientCertificate: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
			return &cert, nil
		},
	})
	if err != nil {
		t.Fatalf("new auth client: %v", err)
	}
	if _, err := client.LoginCert(context.Background(), CertLoginRequest{MountPath: "auth/cert"}); err != nil {
		t.Fatalf("login cert with client certificate: %v", err)
	}
}

func TestNewHTTPTransportUsesExplicitControlPlaneDefaults(t *testing.T) {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	transport := newHTTPTransport(tlsConfig)

	if transport.TLSClientConfig != tlsConfig {
		t.Fatal("transport did not preserve TLS config")
	}
	if transport.DialContext == nil {
		t.Fatal("transport must use an explicit dialer")
	}
	if !transport.ForceAttemptHTTP2 {
		t.Fatal("transport should attempt HTTP/2")
	}
	if transport.TLSHandshakeTimeout != defaultHTTPTLSHandshakeTimeout {
		t.Fatalf("unexpected TLS handshake timeout: %s", transport.TLSHandshakeTimeout)
	}
	if transport.ResponseHeaderTimeout != defaultHTTPResponseHeaderTimeout {
		t.Fatalf("unexpected response header timeout: %s", transport.ResponseHeaderTimeout)
	}
	if transport.ExpectContinueTimeout != defaultHTTPExpectContinueTimeout {
		t.Fatalf("unexpected expect-continue timeout: %s", transport.ExpectContinueTimeout)
	}
	if transport.IdleConnTimeout != defaultHTTPIdleConnTimeout {
		t.Fatalf("unexpected idle connection timeout: %s", transport.IdleConnTimeout)
	}
	if transport.MaxIdleConns != defaultHTTPMaxIdleConns {
		t.Fatalf("unexpected max idle connections: %d", transport.MaxIdleConns)
	}
	if transport.MaxIdleConnsPerHost != defaultHTTPMaxIdleConnsPerHost {
		t.Fatalf("unexpected max idle connections per host: %d", transport.MaxIdleConnsPerHost)
	}
	if transport.MaxConnsPerHost != defaultHTTPMaxConnsPerHost {
		t.Fatalf("unexpected max connections per host: %d", transport.MaxConnsPerHost)
	}
}

func TestOpenBaoResponseLimits(t *testing.T) {
	tests := []struct {
		name      string
		operation string
		want      int64
	}{
		{
			name:      "small response",
			operation: transitOperationEncrypt,
			want:      maxOpenBaoSmallResponseBytes,
		},
		{
			name:      "key metadata response",
			operation: transitOperationMetadataRead,
			want:      maxOpenBaoLargeResponseBytes,
		},
		{
			name:      "batch decrypt response",
			operation: transitOperationBatchDecrypt,
			want:      maxOpenBaoLargeResponseBytes,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := openBaoResponseLimit(test.operation); got != test.want {
				t.Fatalf("unexpected response limit: got %d, want %d", got, test.want)
			}
		})
	}
}

func TestReadBoundedResponseBodyAcceptsExactLimit(t *testing.T) {
	body := strings.Repeat("a", 32)
	encoded, err := readBoundedResponseBody(transitOperationEncrypt, strings.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("read exact-limit response: %v", err)
	}
	if string(encoded) != body {
		t.Fatalf("unexpected response body: %q", string(encoded))
	}
}

func TestReadOpenBaoResponseRejectsOversizedSuccessBody(t *testing.T) {
	secretMarker := "secret-response-content"
	body := secretMarker + strings.Repeat("a", maxOpenBaoSmallResponseBytes)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	var response disableUpsertResponse
	_, err := readOpenBaoResponse(transitOperationDisableUpsertRead, resp, &response)
	assertResponseTooLargeError(t, err, secretMarker)
}

func TestReadOpenBaoResponseRejectsOversizedLargeBody(t *testing.T) {
	body := strings.Repeat("a", maxOpenBaoLargeResponseBytes+1)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	var response keyProfileResponse
	_, err := readOpenBaoResponse(transitOperationMetadataRead, resp, &response)
	assertResponseTooLargeError(t, err, "")
}

func TestReadOpenBaoResponseRejectsOversizedErrorBody(t *testing.T) {
	secretMarker := "secret-error-content"
	body := secretMarker + strings.Repeat("a", maxOpenBaoErrorResponseBytes)
	resp := &http.Response{
		StatusCode: http.StatusForbidden,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	_, err := readOpenBaoResponse(transitOperationEncrypt, resp, nil)
	assertResponseTooLargeError(t, err, secretMarker)
}

func TestReadOpenBaoResponseAllowsLargeMetadataBody(t *testing.T) {
	padding := strings.Repeat("a", maxOpenBaoSmallResponseBytes)
	body := `{"data":{"disable_upsert":true},"padding":"` + padding + `"}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	var response disableUpsertResponse
	if _, err := readOpenBaoResponse(transitOperationMetadataRead, resp, &response); err != nil {
		t.Fatalf("read large metadata response: %v", err)
	}
	if !response.Data.DisableUpsert {
		t.Fatal("metadata response was not decoded")
	}
}

func assertResponseTooLargeError(t *testing.T, err error, secretMarker string) {
	t.Helper()
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("expected response-too-large error, got %v", err)
	}
	var openBaoErr *Error
	if !errors.As(err, &openBaoErr) || openBaoErr.Class != ErrorClassUnavailable {
		t.Fatalf("expected unavailable OpenBao error, got %v", err)
	}
	if got := requestErrorClass(err); got != ErrorClassUnavailable {
		t.Fatalf("unexpected observed error class: %q", got)
	}
	if secretMarker != "" && strings.Contains(err.Error(), secretMarker) {
		t.Fatalf("error contains response content: %q", err.Error())
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
	if strings.Contains(openBaoErr.Error(), "permission denied") {
		t.Fatalf("error exposed raw OpenBao message: %q", openBaoErr.Error())
	}
}

func TestClientPreservesRequestContextCancellation(t *testing.T) {
	client, err := NewClientWithHTTPClient(ClientConfig{
		Address:     "https://bao.example.internal",
		TokenSource: StaticTokenSource{TokenValue: testToken},
	}, &http.Client{})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = client.ReadDisableUpsert(ctx, testMountPath)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
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

func TestClientObservesSafeOpenBaoRequestID(t *testing.T) {
	observer := &fakeRequestObserver{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(t, r, http.MethodGet, "/v1/transit/config/keys")
		_, _ = w.Write([]byte(`{
			"request_id":"req-123_ABC",
			"data":{"disable_upsert":true}
		}`))
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server)
	client.observer = observer
	if _, err := client.ReadDisableUpsert(context.Background(), testMountPath); err != nil {
		t.Fatalf("read disable_upsert: %v", err)
	}

	if len(observer.requests) != 1 {
		t.Fatalf("expected one observation, got %d", len(observer.requests))
	}
	if observer.requests[0].RequestID != "req-123_ABC" {
		t.Fatalf("unexpected request id: %#v", observer.requests[0])
	}
}

func TestClientDropsUnsafeOpenBaoRequestID(t *testing.T) {
	observer := &fakeRequestObserver{}
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusForbidden,
			Body: io.NopCloser(strings.NewReader(
				`{"request_id":"token value with spaces","errors":["permission denied"]}`,
			)),
			Header: make(http.Header),
		}, nil
	})
	client, err := NewClientWithHTTPClient(ClientConfig{
		Address:     "https://bao.example.internal",
		TokenSource: StaticTokenSource{TokenValue: testToken},
		Observer:    observer,
	}, &http.Client{Transport: transport})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	_, err = client.ReadDisableUpsert(context.Background(), testMountPath)
	if err == nil {
		t.Fatal("expected request to fail")
	}
	if len(observer.requests) != 1 {
		t.Fatalf("expected one observation, got %d", len(observer.requests))
	}
	if observer.requests[0].RequestID != "" {
		t.Fatalf("unsafe request id should be dropped: %#v", observer.requests[0])
	}
}

type fakeRequestObserver struct {
	requests []RequestObservation
}

func (f *fakeRequestObserver) ObserveOpenBaoRequest(_ context.Context, observation RequestObservation) {
	f.requests = append(f.requests, observation)
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

func newClientTLSCertificate(t *testing.T) tls.Certificate {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate client key: %v", err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "openbao-kms-control-plane",
		},
		NotBefore: time.Now().Add(-time.Minute),
		NotAfter:  time.Now().Add(time.Hour),
		KeyUsage:  x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageClientAuth,
		},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create client certificate: %v", err)
	}
	return tls.Certificate{
		Certificate: [][]byte{der},
		PrivateKey:  key,
	}
}
