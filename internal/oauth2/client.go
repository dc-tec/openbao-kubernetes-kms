// Package oauth2 obtains JWT access tokens with the OAuth 2.0 client credentials grant.
package oauth2

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	// ClientSecretBasic sends form-encoded client credentials using HTTP Basic authentication.
	ClientSecretBasic = "client_secret_basic"
	// ClientSecretPost sends client credentials in the token request body.
	ClientSecretPost = "client_secret_post"
	maxResponseBytes = 1 << 20
	maxSecretBytes   = 64 << 10
)

var (
	// ErrConfig identifies invalid OAuth client settings or TLS trust.
	ErrConfig = errors.New("oauth2 config invalid")
	// ErrCredential identifies an unreadable or unsafe local client credential.
	ErrCredential = errors.New("oauth2 client credential unavailable")
	// ErrRequest identifies a failed token endpoint request, without remote error details.
	ErrRequest = errors.New("oauth2 token request failed")
	// ErrRejected identifies a token request rejected by the authorization server.
	ErrRejected = errors.New("oauth2 token request rejected")
	// ErrResponse identifies a token response outside the supported contract.
	ErrResponse = errors.New("oauth2 token response invalid")
)

// Config contains one fixed token endpoint and its client credentials.
type Config struct {
	TokenURL         string
	ClientID         string
	ClientSecretFile string
	AuthMethod       string
	Scopes           []string
	Audience         string
	Resources        []string
	CACertFile       string
	Timeout          time.Duration
}

// Token is an access token with an optional server-declared lifetime.
type Token struct {
	AccessToken string
	ExpiresIn   time.Duration
}

// Client makes bounded requests to a single OAuth token endpoint.
type Client struct {
	cfg  Config
	http *http.Client
}

// ValidateConfig checks configuration without network or filesystem access.
func ValidateConfig(cfg Config) error {
	if err := validateEndpoint(cfg.TokenURL); err != nil {
		return err
	}
	if cfg.ClientID == "" || strings.ContainsFunc(cfg.ClientID, unicode.IsControl) {
		return errors.New("clientId must be nonempty and contain no control characters")
	}
	if !filepath.IsAbs(cfg.ClientSecretFile) {
		return errors.New("clientSecretFile must be an absolute path")
	}
	if cfg.AuthMethod != ClientSecretBasic && cfg.AuthMethod != ClientSecretPost {
		return errors.New("authMethod must be client_secret_basic or client_secret_post")
	}
	if cfg.CACertFile != "" && !filepath.IsAbs(cfg.CACertFile) {
		return errors.New("caCertFile must be an absolute path")
	}
	if cfg.Timeout <= 0 {
		return errors.New("timeout must be positive")
	}
	return validateTokenParameters(cfg)
}

func validateEndpoint(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil ||
		u.Fragment != "" || u.Opaque != "" {
		return errors.New("tokenUrl must be an https URL without user info or fragment")
	}
	return nil
}

func validateTokenParameters(cfg Config) error {
	for _, scope := range cfg.Scopes {
		if scope == "" || strings.ContainsFunc(scope, func(r rune) bool {
			return r < 0x21 || r > 0x7e || r == '"' || r == '\\'
		}) {
			return errors.New("scopes must contain nonempty OAuth scope tokens")
		}
	}
	if strings.ContainsFunc(cfg.Audience, unicode.IsControl) {
		return errors.New("audience must contain no control characters")
	}
	for _, resource := range cfg.Resources {
		u, err := url.Parse(resource)
		if err != nil || !u.IsAbs() || u.Fragment != "" || u.User != nil {
			return errors.New("resources must contain absolute URIs without user info or fragments")
		}
	}
	return nil
}

// NewClient validates settings and builds a private HTTP transport with verified TLS.
func NewClient(cfg Config) (*Client, error) {
	if err := ValidateConfig(cfg); err != nil {
		return nil, errors.Join(ErrConfig, err)
	}
	var roots *x509.CertPool
	if cfg.CACertFile != "" {
		// #nosec G304 -- trust path is supplied by the administrator and validated above.
		pem, err := os.ReadFile(cfg.CACertFile)
		if err != nil {
			return nil, errors.Join(ErrConfig, errors.New("caCertFile is unreadable"))
		}
		roots = x509.NewCertPool()
		if !roots.AppendCertsFromPEM(pem) {
			return nil, errors.Join(ErrConfig, errors.New("caCertFile contains no certificates"))
		}
	}
	cfg.Scopes = slices.Clone(cfg.Scopes)
	cfg.Resources = slices.Clone(cfg.Resources)
	return &Client{cfg: cfg, http: &http.Client{
		Timeout:       cfg.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Transport: &http.Transport{
			Proxy:                  http.ProxyFromEnvironment,
			DialContext:            (&net.Dialer{Timeout: cfg.Timeout, KeepAlive: 30 * time.Second}).DialContext,
			TLSClientConfig:        &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots},
			TLSHandshakeTimeout:    cfg.Timeout,
			ResponseHeaderTimeout:  cfg.Timeout,
			MaxResponseHeaderBytes: 64 << 10,
			MaxIdleConns:           2,
			MaxIdleConnsPerHost:    2,
			IdleConnTimeout:        30 * time.Second,
		},
	}}, nil
}

// Token obtains a fresh access token and rereads the client secret on every call.
func (c *Client) Token(ctx context.Context) (Token, error) {
	secret, err := ReadClientSecret(c.cfg.ClientSecretFile)
	if err != nil {
		return Token{}, err
	}
	form := url.Values{"grant_type": {"client_credentials"}}
	if len(c.cfg.Scopes) > 0 {
		form.Set("scope", strings.Join(c.cfg.Scopes, " "))
	}
	if c.cfg.Audience != "" {
		form.Set("audience", c.cfg.Audience)
	}
	for _, resource := range c.cfg.Resources {
		form.Add("resource", resource)
	}
	if c.cfg.AuthMethod == ClientSecretPost {
		form.Set("client_id", c.cfg.ClientID)
		form.Set("client_secret", secret)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return Token{}, ErrRequest
	}
	// RFC 6749 section 2.3.1 requires form encoding before HTTP Basic encoding.
	if c.cfg.AuthMethod == ClientSecretBasic {
		req.SetBasicAuth(url.QueryEscape(c.cfg.ClientID), url.QueryEscape(secret))
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	response, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return Token{}, errors.Join(ErrRequest, ctx.Err())
		}
		return Token{}, ErrRequest
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return Token{}, ErrRejected
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return Token{}, ErrResponse
	}
	return decodeToken(response.Body)
}

func decodeToken(body io.Reader) (Token, error) {
	content, err := io.ReadAll(io.LimitReader(body, maxResponseBytes+1))
	if err != nil || len(content) > maxResponseBytes {
		return Token{}, ErrResponse
	}
	// RFC 6749 section 5.1 requires clients to ignore unrecognized response fields.
	// Decode into a typed DTO while allowing extensions such as scope and refresh_token.
	var response struct {
		AccessToken string      `json:"access_token"`
		TokenType   string      `json:"token_type"`
		ExpiresIn   json.Number `json:"expires_in"`
	}
	if err := json.Unmarshal(content, &response); err != nil ||
		response.AccessToken == "" || !strings.EqualFold(response.TokenType, "Bearer") {
		return Token{}, ErrResponse
	}
	token := Token{AccessToken: response.AccessToken}
	if response.ExpiresIn != "" {
		seconds, err := strconv.ParseInt(string(response.ExpiresIn), 10, 64)
		if err != nil || seconds <= 0 || seconds > int64((1<<63-1)/time.Second) {
			return Token{}, ErrResponse
		}
		token.ExpiresIn = time.Duration(seconds) * time.Second
	}
	return token, nil
}
