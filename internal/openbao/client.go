package openbao

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"
)

const (
	vaultTokenHeader     = "X-Vault-Token" // #nosec G101 -- header name, not a credential value.
	vaultNamespaceHeader = "X-Vault-Namespace"
	contentTypeHeader    = "Content-Type"
	contentTypeJSON      = "application/json"
	openBaoAPIVersion    = "v1"
	addressSchemeHTTPS   = "https"
	maxRequestIDLength   = 128
)

// TokenSource returns the current OpenBao token for one request.
type TokenSource interface {
	Token(context.Context) (string, error)
}

// StaticTokenSource is useful for tests and manually supplied integration tokens.
type StaticTokenSource struct {
	TokenValue string
}

// Token returns the configured in-memory token.
func (s StaticTokenSource) Token(context.Context) (string, error) {
	if s.TokenValue == "" {
		return "", fmt.Errorf("openbao token is required")
	}
	return s.TokenValue, nil
}

// ClientConfig contains OpenBao HTTP client construction settings.
type ClientConfig struct {
	Address       string
	Namespace     string
	CACertFile    string
	TLSServerName string
	Timeout       time.Duration
	TokenSource   TokenSource
	Observer      RequestObserver
}

// AuthClientConfig contains OpenBao auth client construction settings.
type AuthClientConfig struct {
	Address              string
	Namespace            string
	CACertFile           string
	TLSServerName        string
	Timeout              time.Duration
	GetClientCertificate func(*tls.CertificateRequestInfo) (*tls.Certificate, error)
	Observer             RequestObserver
}

// Client is a narrow OpenBao API client for Transit and diagnostics.
type Client struct {
	baseURL     *url.URL
	namespace   string
	tokenSource TokenSource
	httpClient  *http.Client
	observer    RequestObserver
}

// AuthClient is a narrow OpenBao API client for JWT login and token renewal.
type AuthClient struct {
	baseURL    *url.URL
	namespace  string
	httpClient *http.Client
	observer   RequestObserver
}

// RequestObservation is one redacted OpenBao HTTP request observation.
type RequestObservation struct {
	Operation  string
	Status     string
	Duration   time.Duration
	ErrorClass ErrorClass
	RequestID  string
}

// RequestObserver receives redacted OpenBao HTTP request observations.
type RequestObserver interface {
	ObserveOpenBaoRequest(context.Context, RequestObservation)
}

// NewClient builds an OpenBao client with pinned CA roots and server-name validation.
func NewClient(cfg ClientConfig) (*Client, error) {
	httpClient, err := NewHTTPClient(cfg.CACertFile, cfg.TLSServerName, cfg.Timeout)
	if err != nil {
		return nil, err
	}
	return NewClientWithHTTPClient(cfg, httpClient)
}

// NewClientWithHTTPClient builds a client with an injected HTTP client for tests.
func NewClientWithHTTPClient(cfg ClientConfig, httpClient *http.Client) (*Client, error) {
	if httpClient == nil {
		return nil, fmt.Errorf("http client is required")
	}
	if cfg.TokenSource == nil {
		return nil, fmt.Errorf("token source is required")
	}
	baseURL, err := parseAddress(cfg.Address)
	if err != nil {
		return nil, err
	}
	return &Client{
		baseURL:     baseURL,
		namespace:   cfg.Namespace,
		tokenSource: cfg.TokenSource,
		httpClient:  httpClient,
		observer:    cfg.Observer,
	}, nil
}

// NewAuthClient builds an OpenBao auth client with pinned CA roots and server-name validation.
func NewAuthClient(cfg AuthClientConfig) (*AuthClient, error) {
	httpClient, err := NewHTTPClientWithTLSOptions(HTTPClientConfig{
		CACertFile:           cfg.CACertFile,
		TLSServerName:        cfg.TLSServerName,
		Timeout:              cfg.Timeout,
		GetClientCertificate: cfg.GetClientCertificate,
	})
	if err != nil {
		return nil, err
	}
	return NewAuthClientWithHTTPClient(cfg, httpClient)
}

// NewAuthClientWithHTTPClient builds an auth client with an injected HTTP client for tests.
func NewAuthClientWithHTTPClient(cfg AuthClientConfig, httpClient *http.Client) (*AuthClient, error) {
	if httpClient == nil {
		return nil, fmt.Errorf("http client is required")
	}
	baseURL, err := parseAddress(cfg.Address)
	if err != nil {
		return nil, err
	}
	return &AuthClient{
		baseURL:    baseURL,
		namespace:  cfg.Namespace,
		httpClient: httpClient,
		observer:   cfg.Observer,
	}, nil
}

// NewTLSConfig returns a TLS config using the configured CA and server name.
func NewTLSConfig(caCertFile string, serverName string) (*tls.Config, error) {
	if strings.TrimSpace(serverName) == "" {
		return nil, fmt.Errorf("TLS server name is required")
	}
	if strings.TrimSpace(caCertFile) == "" {
		return nil, fmt.Errorf("CA certificate file is required")
	}

	// #nosec G304 -- CA bundle path comes from validated local provider configuration.
	caPEM, err := os.ReadFile(caCertFile)
	if err != nil {
		return nil, fmt.Errorf("read OpenBao CA bundle: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("OpenBao CA bundle does not contain PEM certificates")
	}

	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    pool,
		ServerName: serverName,
	}, nil
}

// HTTPClientConfig contains TLS policy for OpenBao HTTP clients.
type HTTPClientConfig struct {
	CACertFile           string
	TLSServerName        string
	Timeout              time.Duration
	GetClientCertificate func(*tls.CertificateRequestInfo) (*tls.Certificate, error)
}

// NewHTTPClient returns an HTTP client with bounded timeout and explicit TLS policy.
func NewHTTPClient(caCertFile string, serverName string, timeout time.Duration) (*http.Client, error) {
	return NewHTTPClientWithTLSOptions(HTTPClientConfig{
		CACertFile:    caCertFile,
		TLSServerName: serverName,
		Timeout:       timeout,
	})
}

// NewHTTPClientWithTLSOptions returns an HTTP client with bounded timeout and explicit TLS policy.
func NewHTTPClientWithTLSOptions(cfg HTTPClientConfig) (*http.Client, error) {
	if cfg.Timeout <= 0 {
		return nil, fmt.Errorf("OpenBao timeout must be positive")
	}
	tlsConfig, err := NewTLSConfig(cfg.CACertFile, cfg.TLSServerName)
	if err != nil {
		return nil, err
	}
	tlsConfig.GetClientCertificate = cfg.GetClientCertificate
	return &http.Client{
		Timeout: cfg.Timeout,
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}, nil
}

// CloseIdleConnections closes idle HTTP connections held by the auth client.
func (c *AuthClient) CloseIdleConnections() {
	c.httpClient.CloseIdleConnections()
}

func (c *Client) do(
	ctx context.Context,
	operation string,
	method string,
	apiPath string,
	requestBody requestPayload,
	response responsePayload,
) error {
	token, err := c.tokenSource.Token(ctx)
	if err != nil {
		return err
	}
	return doOpenBao(
		ctx,
		c.httpClient,
		c.baseURL,
		c.namespace,
		operation,
		method,
		apiPath,
		requestBody,
		response,
		token,
		true,
		c.observer,
	)
}

func (c *AuthClient) doUnauthenticated(
	ctx context.Context,
	operation string,
	method string,
	apiPath string,
	requestBody requestPayload,
	response responsePayload,
) error {
	return doOpenBao(
		ctx,
		c.httpClient,
		c.baseURL,
		c.namespace,
		operation,
		method,
		apiPath,
		requestBody,
		response,
		"",
		false,
		c.observer,
	)
}

func (c *AuthClient) doWithToken(
	ctx context.Context,
	operation string,
	method string,
	apiPath string,
	requestBody requestPayload,
	response responsePayload,
	token string,
) error {
	if token == "" {
		return fmt.Errorf("openbao token is required")
	}
	return doOpenBao(
		ctx,
		c.httpClient,
		c.baseURL,
		c.namespace,
		operation,
		method,
		apiPath,
		requestBody,
		response,
		token,
		true,
		c.observer,
	)
}

func doOpenBao(
	ctx context.Context,
	httpClient *http.Client,
	baseURL *url.URL,
	namespace string,
	operation string,
	method string,
	apiPath string,
	requestBody requestPayload,
	response responsePayload,
	token string,
	includeToken bool,
	observer RequestObserver,
) (err error) {
	start := time.Now()
	requestID := ""
	defer func() {
		if observer != nil {
			observer.ObserveOpenBaoRequest(ctx, RequestObservation{
				Operation:  operation,
				Status:     requestStatus(err),
				Duration:   time.Since(start),
				ErrorClass: requestErrorClass(err),
				RequestID:  requestID,
			})
		}
	}()

	var body io.Reader
	if requestBody != nil {
		encoded, err := json.Marshal(requestBody)
		if err != nil {
			return fmt.Errorf("encode OpenBao %s request: %w", operation, err)
		}
		body = bytes.NewReader(encoded)
	}

	requestURL := resolveOpenBao(baseURL, apiPath)
	req, err := http.NewRequestWithContext(ctx, method, requestURL, body)
	if err != nil {
		return fmt.Errorf("build OpenBao %s request: %w", operation, err)
	}
	if requestBody != nil {
		req.Header.Set(contentTypeHeader, contentTypeJSON)
	}
	if includeToken {
		req.Header.Set(vaultTokenHeader, token)
	}
	if namespace != "" {
		req.Header.Set(vaultNamespaceHeader, namespace)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return &Error{Class: ErrorClassUnavailable, Operation: operation}
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body := decodeErrorBody(resp.Body)
		requestID = safeRequestID(body.RequestID)
		return newHTTPError(operation, resp.StatusCode, body.Errors)
	}
	if response == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(response); err != nil {
		return fmt.Errorf("decode OpenBao %s response: %w", operation, err)
	}
	requestID = requestIDFromResponse(response)
	return nil
}

func (c *Client) resolve(apiPath string) string {
	return resolveOpenBao(c.baseURL, apiPath)
}

func resolveOpenBao(baseURL *url.URL, apiPath string) string {
	resolved := *baseURL
	resolved.Path = path.Join(baseURL.Path, openBaoAPIVersion, apiPath)
	return resolved.String()
}

type requestPayload interface {
	requestPayload()
}

type responsePayload interface {
	responsePayload()
}

type requestIDPayload interface {
	openBaoRequestID() string
}

type responseMetadata struct {
	RequestID string `json:"request_id"`
}

func (m responseMetadata) openBaoRequestID() string {
	return safeRequestID(m.RequestID)
}

type errorBody struct {
	RequestID string   `json:"request_id"`
	Errors    []string `json:"errors"`
}

func decodeErrorBody(reader io.Reader) errorBody {
	var body errorBody
	if err := json.NewDecoder(reader).Decode(&body); err != nil {
		return errorBody{}
	}
	messages := make([]string, len(body.Errors))
	copy(messages, body.Errors)
	body.Errors = messages
	return body
}

func requestIDFromResponse(response responsePayload) string {
	payload, ok := response.(requestIDPayload)
	if !ok {
		return ""
	}
	return payload.openBaoRequestID()
}

func safeRequestID(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || len(trimmed) > maxRequestIDLength {
		return ""
	}
	for _, char := range trimmed {
		if !safeRequestIDChar(char) {
			return ""
		}
	}
	return trimmed
}

func safeRequestIDChar(char rune) bool {
	return char >= 'a' && char <= 'z' ||
		char >= 'A' && char <= 'Z' ||
		char >= '0' && char <= '9' ||
		char == '-' ||
		char == '_' ||
		char == '.' ||
		char == ':'
}

func parseAddress(address string) (*url.URL, error) {
	parsed, err := url.Parse(address)
	if err != nil {
		return nil, fmt.Errorf("parse OpenBao address: %w", err)
	}
	if parsed.Scheme != addressSchemeHTTPS ||
		parsed.Host == "" ||
		parsed.User != nil ||
		parsed.RawQuery != "" ||
		parsed.Fragment != "" {
		return nil, fmt.Errorf("OpenBao address must be an https URL with no user info, query, or fragment")
	}
	return parsed, nil
}

func transitPath(mountPath string, operation string, keyName string) string {
	mount := strings.Trim(mountPath, "/")
	key := url.PathEscape(keyName)
	return path.Join(mount, operation, key)
}

func transitConfigPath(mountPath string) string {
	return path.Join(strings.Trim(mountPath, "/"), transitPathSegmentConfig, transitPathSegmentKeys)
}
