// Package auth owns JWT file handling and OpenBao token lifecycle state.
package auth

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	maxJWTFileBytes = 1 << 20
	jwtFileModeMask = os.FileMode(0o037)

	compactJWTPartCount = 3
	jwtHeaderPart       = 0
	jwtPayloadPart      = 1
	jwtWhitespaceChars  = " \t\r\n"
)

var (
	// ErrJWTRead identifies an unreadable or unsafe JWT identity file.
	ErrJWTRead = errors.New("jwt read failed")
	// ErrJWTMalformed identifies a JWT that cannot be parsed as compact JWT claims.
	ErrJWTMalformed = errors.New("jwt malformed")
	// ErrJWTExpired identifies a JWT whose exp claim is not in the future.
	ErrJWTExpired = errors.New("jwt expired")
	// ErrJWTNearExpiry identifies a JWT that is too close to expiry for login.
	ErrJWTNearExpiry = errors.New("jwt near expiry")
	// ErrJWTNotYetValid identifies a JWT with a future nbf claim.
	ErrJWTNotYetValid = errors.New("jwt not yet valid")
	// ErrJWTIssuedInFuture identifies a JWT with a future iat claim.
	ErrJWTIssuedInFuture = errors.New("jwt issued in future")
)

// Clock abstracts time for token lifecycle tests.
type Clock interface {
	Now() time.Time
}

// RealClock uses the host wall clock.
type RealClock struct{}

// Now returns the current UTC time.
func (RealClock) Now() time.Time {
	return time.Now().UTC()
}

// JWT is one identity token read from disk with locally parsed claims.
type JWT struct {
	Raw    string
	Claims Claims
	ReadAt time.Time
}

// Claims contains locally checkable registered JWT claims.
type Claims struct {
	Issuer       string
	Subject      string
	Audience     []string
	ExpiresAt    time.Time
	NotBefore    time.Time
	IssuedAt     time.Time
	HasNotBefore bool
	HasIssuedAt  bool
}

// JWTValidationOptions controls local JWT expiry checks before OpenBao login.
type JWTValidationOptions struct {
	MinRemainingTTL time.Duration
	ClockSkewLeeway time.Duration
	Clock           Clock
}

type registeredClaims struct {
	Issuer    string        `json:"iss"`
	Subject   string        `json:"sub"`
	Audience  audienceClaim `json:"aud"`
	ExpiresAt numericDate   `json:"exp"`
	NotBefore numericDate   `json:"nbf"`
	IssuedAt  numericDate   `json:"iat"`
}

type audienceClaim struct {
	Values []string
}

type numericDate struct {
	Time time.Time
	Set  bool
}

// ReadJWTFile reads a compact JWT from a local identity file.
func ReadJWTFile(path string) (string, error) {
	cleanPath := strings.TrimSpace(path)
	if cleanPath == "" {
		return "", fmt.Errorf("%w: file path is required", ErrJWTRead)
	}
	if !filepath.IsAbs(cleanPath) {
		return "", fmt.Errorf("%w: identity file path must be absolute", ErrJWTRead)
	}
	if err := validateJWTFileInfo(cleanPath); err != nil {
		return "", err
	}

	// #nosec G304 -- JWT file path comes from validated local provider configuration.
	file, err := os.Open(cleanPath)
	if err != nil {
		return "", fmt.Errorf("%w: identity file is unreadable", ErrJWTRead)
	}
	defer func() {
		_ = file.Close()
	}()
	if err := validateOpenedJWTFile(cleanPath, file); err != nil {
		return "", err
	}

	content, err := io.ReadAll(io.LimitReader(file, maxJWTFileBytes+1))
	if err != nil {
		return "", fmt.Errorf("%w: identity file is unreadable", ErrJWTRead)
	}
	if len(content) > maxJWTFileBytes {
		return "", fmt.Errorf("%w: identity file is too large", ErrJWTRead)
	}
	token := strings.TrimSpace(string(content))
	if token == "" {
		return "", fmt.Errorf("%w: identity file is empty", ErrJWTRead)
	}
	if strings.ContainsAny(token, jwtWhitespaceChars) {
		return "", fmt.Errorf("%w: compact token contains whitespace", ErrJWTMalformed)
	}
	return token, nil
}

// ReadJWT reads one JWT file and parses its claims without verifying the signature.
func ReadJWT(path string, clock Clock) (JWT, error) {
	token, err := ReadJWTFile(path)
	if err != nil {
		return JWT{}, err
	}
	claims, err := ParseClaims(token)
	if err != nil {
		return JWT{}, err
	}
	return JWT{
		Raw:    token,
		Claims: claims,
		ReadAt: clockOrReal(clock).Now(),
	}, nil
}

// ReadAndValidateJWT reads one JWT file and enforces local expiry policy.
func ReadAndValidateJWT(path string, opts JWTValidationOptions) (JWT, error) {
	token, err := ReadJWT(path, opts.Clock)
	if err != nil {
		return JWT{}, err
	}
	if err := ValidateClaims(token.Claims, opts); err != nil {
		return JWT{}, err
	}
	return token, nil
}

// ParseClaims parses registered claims from a compact JWT without verifying its signature.
func ParseClaims(token string) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != compactJWTPartCount || parts[jwtHeaderPart] == "" || parts[jwtPayloadPart] == "" {
		return Claims{}, fmt.Errorf("%w: compact token must contain header, payload, and signature", ErrJWTMalformed)
	}
	payload, err := decodeJWTPart(parts[jwtPayloadPart])
	if err != nil {
		return Claims{}, fmt.Errorf("%w: payload is not valid base64url", ErrJWTMalformed)
	}

	var decoded registeredClaims
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return Claims{}, fmt.Errorf("%w: payload claims are invalid", ErrJWTMalformed)
	}
	if !decoded.ExpiresAt.Set {
		return Claims{}, fmt.Errorf("%w: exp claim is required", ErrJWTMalformed)
	}

	return Claims{
		Issuer:       decoded.Issuer,
		Subject:      decoded.Subject,
		Audience:     slices.Clone(decoded.Audience.Values),
		ExpiresAt:    decoded.ExpiresAt.Time,
		NotBefore:    decoded.NotBefore.Time,
		IssuedAt:     decoded.IssuedAt.Time,
		HasNotBefore: decoded.NotBefore.Set,
		HasIssuedAt:  decoded.IssuedAt.Set,
	}, nil
}

// ValidateClaims enforces local JWT validity windows before OpenBao login.
func ValidateClaims(claims Claims, opts JWTValidationOptions) error {
	now := clockOrReal(opts.Clock).Now()
	leeway := opts.ClockSkewLeeway
	if leeway < 0 {
		leeway = 0
	}
	latestAcceptedNow := now.Add(leeway)
	if claims.HasNotBefore && latestAcceptedNow.Before(claims.NotBefore) {
		return fmt.Errorf("%w: nbf claim is in the future", ErrJWTNotYetValid)
	}
	if claims.HasIssuedAt && latestAcceptedNow.Before(claims.IssuedAt) {
		return fmt.Errorf("%w: iat claim is in the future", ErrJWTIssuedInFuture)
	}
	remaining := claims.ExpiresAt.Sub(now)
	if now.Add(-leeway).After(claims.ExpiresAt) || now.Add(-leeway).Equal(claims.ExpiresAt) {
		return fmt.Errorf("%w: exp claim is not in the future", ErrJWTExpired)
	}
	if opts.MinRemainingTTL > 0 && remaining <= opts.MinRemainingTTL {
		return fmt.Errorf("%w: remaining TTL is below minimum", ErrJWTNearExpiry)
	}
	return nil
}

// RemainingTTL returns the JWT expiry duration relative to the supplied clock.
func (c Claims) RemainingTTL(clock Clock) time.Duration {
	return c.ExpiresAt.Sub(clockOrReal(clock).Now())
}

func (a *audienceClaim) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("null")) {
		return nil
	}
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		a.Values = []string{single}
		return nil
	}
	var multiple []string
	if err := json.Unmarshal(data, &multiple); err == nil {
		a.Values = slices.Clone(multiple)
		return nil
	}
	return fmt.Errorf("aud claim must be a string or string array")
}

func (n *numericDate) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("null")) {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var number json.Number
	if err := decoder.Decode(&number); err != nil {
		return fmt.Errorf("numeric date must be a number: %w", err)
	}
	seconds, err := number.Int64()
	if err != nil {
		return fmt.Errorf("numeric date must be an integer: %w", err)
	}
	if seconds < 0 {
		return fmt.Errorf("numeric date must be non-negative")
	}
	n.Time = time.Unix(seconds, 0).UTC()
	n.Set = true
	return nil
}

func decodeJWTPart(segment string) ([]byte, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(segment)
	if err == nil {
		return decoded, nil
	}
	return base64.URLEncoding.DecodeString(segment)
}

func validateJWTFileInfo(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("%w: identity file is unreadable", ErrJWTRead)
	}
	return validateJWTFileMode(info)
}

func validateOpenedJWTFile(path string, file *os.File) error {
	openedInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("%w: identity file is unreadable", ErrJWTRead)
	}
	if err := validateJWTFileMode(openedInfo); err != nil {
		return err
	}
	currentInfo, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("%w: identity file is unreadable", ErrJWTRead)
	}
	if err := validateJWTFileMode(currentInfo); err != nil {
		return err
	}
	if !os.SameFile(openedInfo, currentInfo) {
		return fmt.Errorf("%w: identity file changed while opening", ErrJWTRead)
	}
	return nil
}

func validateJWTFileMode(info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: identity file must not be a symlink", ErrJWTRead)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: identity file must be a regular file", ErrJWTRead)
	}
	if info.Mode().Perm()&jwtFileModeMask != 0 {
		return fmt.Errorf("%w: identity file permissions are too broad", ErrJWTRead)
	}
	return nil
}

func clockOrReal(clock Clock) Clock {
	if clock == nil {
		return RealClock{}
	}
	return clock
}
