package openbao

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"slices"
	"strings"
	"time"
)

const (
	authOperationJWTLogin  = "jwt login"
	authOperationCertLogin = "cert login"
	authOperationRenewSelf = "token renew self"
	authTokenRenewSelfPath = "auth/token/renew-self" // #nosec G101 -- OpenBao API path, not a credential.
	authLoginPathSegment   = "login"
	durationSecondsSuffix  = "s"
)

// JWTLoginRequest is an OpenBao JWT auth login request.
type JWTLoginRequest struct {
	MountPath string
	Role      string
	JWT       string
}

// CertLoginRequest is an OpenBao cert auth login request.
type CertLoginRequest struct {
	MountPath string
	Name      string
}

// AuthToken is a redaction-sensitive OpenBao client token response.
type AuthToken struct {
	ClientToken   string
	LeaseDuration time.Duration
	Renewable     bool
	Policies      []string
	TokenPolicies []string
}

// LoginJWT exchanges a local JWT for an OpenBao client token.
func (c *AuthClient) LoginJWT(ctx context.Context, req JWTLoginRequest) (AuthToken, error) {
	mountPath := strings.TrimSpace(req.MountPath)
	role := strings.TrimSpace(req.Role)
	jwt := strings.TrimSpace(req.JWT)
	if mountPath == "" {
		return AuthToken{}, fmt.Errorf("auth mount path is required")
	}
	if role == "" {
		return AuthToken{}, fmt.Errorf("auth role is required")
	}
	if jwt == "" {
		return AuthToken{}, fmt.Errorf("jwt is required")
	}

	body := jwtLoginRequestBody{
		Role: role,
		JWT:  jwt,
	}
	var response authResponseBody
	if err := c.doUnauthenticated(
		ctx,
		authOperationJWTLogin,
		http.MethodPost,
		path.Join(strings.Trim(mountPath, "/"), authLoginPathSegment),
		body,
		&response,
	); err != nil {
		return AuthToken{}, err
	}
	return response.Auth.token(), nil
}

// LoginCert exchanges the configured TLS client certificate for an OpenBao client token.
func (c *AuthClient) LoginCert(ctx context.Context, req CertLoginRequest) (AuthToken, error) {
	mountPath := strings.TrimSpace(req.MountPath)
	name := strings.TrimSpace(req.Name)
	if mountPath == "" {
		return AuthToken{}, fmt.Errorf("auth mount path is required")
	}

	c.CloseIdleConnections()
	body := certLoginRequestBody{Name: name}
	var response authResponseBody
	if err := c.doUnauthenticated(
		ctx,
		authOperationCertLogin,
		http.MethodPost,
		path.Join(strings.Trim(mountPath, "/"), authLoginPathSegment),
		body,
		&response,
	); err != nil {
		return AuthToken{}, err
	}
	return response.Auth.token(), nil
}

// RenewSelfToken renews the supplied OpenBao token using auth/token/renew-self.
func (c *AuthClient) RenewSelfToken(ctx context.Context, token string, increment time.Duration) (AuthToken, error) {
	if token == "" {
		return AuthToken{}, fmt.Errorf("openbao token is required")
	}
	if increment <= 0 {
		return AuthToken{}, fmt.Errorf("token renewal increment must be positive")
	}

	body := renewSelfRequestBody{Increment: formatDurationSeconds(increment)}
	var response authResponseBody
	if err := c.doWithToken(
		ctx,
		authOperationRenewSelf,
		http.MethodPost,
		authTokenRenewSelfPath,
		body,
		&response,
		token,
	); err != nil {
		return AuthToken{}, err
	}
	return response.Auth.token(), nil
}

type jwtLoginRequestBody struct {
	Role string `json:"role"`
	JWT  string `json:"jwt"`
}

func (jwtLoginRequestBody) requestPayload() {}

type certLoginRequestBody struct {
	Name string `json:"name,omitempty"`
}

func (certLoginRequestBody) requestPayload() {}

type renewSelfRequestBody struct {
	Increment string `json:"increment"`
}

func (renewSelfRequestBody) requestPayload() {}

type authResponseBody struct {
	responseMetadata
	Auth authResponseData `json:"auth"`
}

func (*authResponseBody) responsePayload() {}

type authResponseData struct {
	ClientToken   string   `json:"client_token"`
	LeaseDuration int      `json:"lease_duration"`
	Renewable     bool     `json:"renewable"`
	Policies      []string `json:"policies"`
	TokenPolicies []string `json:"token_policies"`
}

func (d authResponseData) token() AuthToken {
	return AuthToken{
		ClientToken:   d.ClientToken,
		LeaseDuration: time.Duration(d.LeaseDuration) * time.Second,
		Renewable:     d.Renewable,
		Policies:      slices.Clone(d.Policies),
		TokenPolicies: slices.Clone(d.TokenPolicies),
	}
}

func formatDurationSeconds(value time.Duration) string {
	seconds := int64((value + time.Second - time.Nanosecond) / time.Second)
	if seconds <= 0 {
		seconds = 1
	}
	return fmt.Sprintf("%d%s", seconds, durationSecondsSuffix)
}
