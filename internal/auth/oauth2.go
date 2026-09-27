package auth

import (
	"context"
	"fmt"
	"strings"

	clocktime "github.com/dc-tec/openbao-kubernetes-kms/internal/clock"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/oauth2"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
)

// OAuth2LoginSource obtains a fresh JWT for each OpenBao login using client credentials.
type OAuth2LoginSource struct {
	cfg    ManagerConfig
	issuer *oauth2.Client
}

// NewOAuth2LoginSource creates a JWT source backed by a fixed OAuth token endpoint.
func NewOAuth2LoginSource(cfg ManagerConfig, clientConfig oauth2.Config) (*OAuth2LoginSource, error) {
	normalized, err := validateManagerConfig(cfg)
	if err != nil {
		return nil, err
	}
	if normalized.JWTFile != "" || normalized.ExpectedIssuer == "" || len(normalized.ExpectedAudience) == 0 {
		return nil, fmt.Errorf("%w: oauth2 requires issuer and audience expectations and no JWT file", ErrAuthConfig)
	}
	issuer, err := oauth2.NewClient(clientConfig)
	if err != nil {
		return nil, err
	}
	return &OAuth2LoginSource{cfg: normalized, issuer: issuer}, nil
}

// SourceInfo returns bounded metadata for the JWT auth method.
func (s *OAuth2LoginSource) SourceInfo() SourceInfo {
	return SourceInfo{AuthMethod: authMethodJWT}
}

// Login acquires, checks, and exchanges a JWT within the manager's shared deadline.
func (s *OAuth2LoginSource) Login(ctx context.Context, client OpenBaoAuthClient, clock Clock) (LoginResult, error) {
	clock = clockOrReal(clock)
	requestedAt := clock.Read()
	token, err := s.issuer.Token(ctx)
	if err != nil {
		return LoginResult{}, err
	}
	parts := strings.Split(token.AccessToken, ".")
	if len(parts) != compactJWTPartCount || parts[2] == "" || strings.ContainsAny(token.AccessToken, jwtWhitespaceChars) {
		return LoginResult{}, ErrJWTMalformed
	}
	claims, err := ParseClaims(token.AccessToken)
	if err != nil {
		return LoginResult{}, err
	}
	var lifetime clocktime.Lifetime
	// The relative endpoint lifetime and the signed absolute claim are separate bounds.
	if token.ExpiresIn > 0 {
		lifetime = clocktime.NewLifetime(requestedAt, token.ExpiresIn)
		if lifetime.Remaining(clock.Read()) <= s.cfg.MinJWTRemainingTTL {
			return LoginResult{}, ErrJWTNearExpiry
		}
	}
	if err := ValidateClaims(claims, JWTValidationOptions{
		MinRemainingTTL: s.cfg.MinJWTRemainingTTL, ClockSkewLeeway: s.cfg.ClockSkewLeeway,
		ExpectedIssuer: s.cfg.ExpectedIssuer, ExpectedAudience: s.cfg.ExpectedAudience,
		ExpectedSubject: s.cfg.ExpectedSubject, Clock: clock,
	}); err != nil {
		return LoginResult{}, err
	}
	result, err := client.LoginJWT(ctx, openbao.JWTLoginRequest{
		MountPath: s.cfg.MountPath, Role: s.cfg.Role, JWT: token.AccessToken,
	})
	if err != nil {
		return LoginResult{}, publicAuthError(err)
	}
	return LoginResult{AuthToken: result, JWT: JWT{
		Raw: token.AccessToken, Claims: claims, ReadAt: clock.Now(), EndpointLifetime: lifetime,
	}}, nil
}
