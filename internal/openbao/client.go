package openbao

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
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
}

// Client is a narrow OpenBao API client for Transit and diagnostics.
type Client struct {
	baseURL     *url.URL
	namespace   string
	tokenSource TokenSource
	httpClient  *http.Client
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

// NewHTTPClient returns an HTTP client with bounded timeout and explicit TLS policy.
func NewHTTPClient(caCertFile string, serverName string, timeout time.Duration) (*http.Client, error) {
	if timeout <= 0 {
		return nil, fmt.Errorf("OpenBao timeout must be positive")
	}
	tlsConfig, err := NewTLSConfig(caCertFile, serverName)
	if err != nil {
		return nil, err
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}, nil
}

func (c *Client) do(
	ctx context.Context,
	operation string,
	method string,
	apiPath string,
	requestBody requestPayload,
	response responsePayload,
) error {
	var body io.Reader
	if requestBody != nil {
		encoded, err := json.Marshal(requestBody)
		if err != nil {
			return fmt.Errorf("encode OpenBao %s request: %w", operation, err)
		}
		body = bytes.NewReader(encoded)
	}

	requestURL := c.resolve(apiPath)
	req, err := http.NewRequestWithContext(ctx, method, requestURL, body)
	if err != nil {
		return fmt.Errorf("build OpenBao %s request: %w", operation, err)
	}
	if requestBody != nil {
		req.Header.Set(contentTypeHeader, contentTypeJSON)
	}
	token, err := c.tokenSource.Token(ctx)
	if err != nil {
		return err
	}
	req.Header.Set(vaultTokenHeader, token)
	if c.namespace != "" {
		req.Header.Set(vaultNamespaceHeader, c.namespace)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return &Error{Class: ErrorClassUnavailable, Operation: operation}
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		messages := decodeErrorMessages(resp.Body)
		return newHTTPError(operation, resp.StatusCode, messages)
	}
	if response == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(response); err != nil {
		return fmt.Errorf("decode OpenBao %s response: %w", operation, err)
	}
	return nil
}

func (c *Client) resolve(apiPath string) string {
	resolved := *c.baseURL
	resolved.Path = path.Join(c.baseURL.Path, openBaoAPIVersion, apiPath)
	return resolved.String()
}

type requestPayload interface {
	requestPayload()
}

type responsePayload interface {
	responsePayload()
}

type errorBody struct {
	Errors []string `json:"errors"`
}

func decodeErrorMessages(reader io.Reader) []string {
	var body errorBody
	if err := json.NewDecoder(reader).Decode(&body); err != nil {
		return nil
	}
	if len(body.Errors) == 0 {
		return nil
	}
	messages := make([]string, len(body.Errors))
	copy(messages, body.Errors)
	return messages
}

func parseAddress(address string) (*url.URL, error) {
	parsed, err := url.Parse(address)
	if err != nil {
		return nil, fmt.Errorf("parse OpenBao address: %w", err)
	}
	if parsed.Scheme != addressSchemeHTTPS || parsed.Host == "" || parsed.User != nil {
		return nil, fmt.Errorf("OpenBao address must be an https URL with no user info")
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
