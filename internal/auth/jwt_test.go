package auth

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	authFixtureDir = "../../test/testdata/auth"

	validJWTFixture        = "valid.jwt"
	rotatedJWTFixture      = "valid-rotated.jwt"
	expiredJWTFixture      = "expired.jwt"
	nearExpiryJWTFixture   = "near-expiry.jwt"
	malformedJWTFixture    = "malformed.jwt"
	testIssuer             = "https://issuer.example.internal"
	testSubject            = "system:openbao-kms:workload-a"
	testAudienceProvider   = "bao-kms-provider"
	testAudienceOpenBao    = "openbao"
	testValidJWTExpiryUnix = 4102444800
	testNotBeforeUnix      = 1700000000
	testCurrentUnix        = 1778270000
)

func TestReadJWTFileTrimsTrailingWhitespace(t *testing.T) {
	token := loadJWTFixture(t, validJWTFixture)
	path := filepath.Join(t.TempDir(), "identity.jwt")
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		t.Fatalf("write jwt: %v", err)
	}

	got, err := ReadJWTFile(path)
	if err != nil {
		t.Fatalf("read jwt: %v", err)
	}
	if got != token {
		t.Fatalf("unexpected token read")
	}
}

func TestParseClaimsReadsRegisteredFields(t *testing.T) {
	claims, err := ParseClaims(loadJWTFixture(t, validJWTFixture))
	if err != nil {
		t.Fatalf("parse claims: %v", err)
	}
	if claims.Issuer != testIssuer {
		t.Fatalf("unexpected issuer: %q", claims.Issuer)
	}
	if claims.Subject != testSubject {
		t.Fatalf("unexpected subject: %q", claims.Subject)
	}
	if !slices.Equal(claims.Audience, []string{testAudienceProvider, testAudienceOpenBao}) {
		t.Fatalf("unexpected audience: %#v", claims.Audience)
	}
	if claims.ExpiresAt.Unix() != testValidJWTExpiryUnix {
		t.Fatalf("unexpected expiry: %s", claims.ExpiresAt)
	}
	if !claims.HasNotBefore || claims.NotBefore.Unix() != testNotBeforeUnix {
		t.Fatalf("unexpected not-before claim: %#v", claims)
	}
}

func TestParseClaimsRejectsMalformedJWT(t *testing.T) {
	_, err := ParseClaims(loadJWTFixture(t, malformedJWTFixture))
	if !errors.Is(err, ErrJWTMalformed) {
		t.Fatalf("expected malformed jwt, got %v", err)
	}
}

func TestReadJWTFileRejectsUnsafeFiles(t *testing.T) {
	tempDir := t.TempDir()
	token := loadJWTFixture(t, validJWTFixture)
	worldReadablePath := filepath.Join(tempDir, "world-readable.jwt")
	// #nosec G306 -- this test intentionally creates an unsafe JWT file mode.
	if err := os.WriteFile(worldReadablePath, []byte(token), 0o644); err != nil {
		t.Fatalf("write world-readable JWT: %v", err)
	}
	if _, err := ReadJWTFile(worldReadablePath); !errors.Is(err, ErrJWTRead) {
		t.Fatalf("expected unsafe permission error, got %v", err)
	}

	targetPath := filepath.Join(tempDir, "target.jwt")
	if err := os.WriteFile(targetPath, []byte(token), 0o600); err != nil {
		t.Fatalf("write target JWT: %v", err)
	}
	linkPath := filepath.Join(tempDir, "identity.jwt")
	if err := os.Symlink(targetPath, linkPath); err != nil {
		t.Fatalf("create JWT symlink: %v", err)
	}
	if _, err := ReadJWTFile(linkPath); !errors.Is(err, ErrJWTRead) {
		t.Fatalf("expected symlink error, got %v", err)
	}
}

func TestValidateClaimsRejectsExpiredAndNearExpiryJWTs(t *testing.T) {
	tests := []struct {
		name      string
		fixture   string
		now       time.Time
		minTTL    time.Duration
		wantError error
	}{
		{
			name:      "expired",
			fixture:   expiredJWTFixture,
			now:       time.Unix(testCurrentUnix, 0).UTC(),
			minTTL:    time.Minute,
			wantError: ErrJWTExpired,
		},
		{
			name:      "near expiry",
			fixture:   nearExpiryJWTFixture,
			now:       time.Unix(1778281900, 0).UTC(),
			minTTL:    2 * time.Minute,
			wantError: ErrJWTNearExpiry,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims, err := ParseClaims(loadJWTFixture(t, tt.fixture))
			if err != nil {
				t.Fatalf("parse claims: %v", err)
			}
			err = ValidateClaims(claims, JWTValidationOptions{
				MinRemainingTTL: tt.minTTL,
				Clock:           &fakeClock{now: tt.now},
			})
			if !errors.Is(err, tt.wantError) {
				t.Fatalf("expected %v, got %v", tt.wantError, err)
			}
		})
	}
}

func TestValidateClaimsRejectsFutureNotBefore(t *testing.T) {
	err := ValidateClaims(Claims{
		ExpiresAt:    time.Unix(testCurrentUnix+3600, 0).UTC(),
		NotBefore:    time.Unix(testCurrentUnix+60, 0).UTC(),
		HasNotBefore: true,
	}, JWTValidationOptions{
		MinRemainingTTL: time.Minute,
		Clock:           &fakeClock{now: time.Unix(testCurrentUnix, 0).UTC()},
	})
	if !errors.Is(err, ErrJWTNotYetValid) {
		t.Fatalf("expected not-yet-valid JWT error, got %v", err)
	}
}

func TestValidateClaimsAppliesClockSkewLeeway(t *testing.T) {
	now := time.Unix(testCurrentUnix, 0).UTC()
	err := ValidateClaims(Claims{
		ExpiresAt:    now.Add(time.Hour),
		NotBefore:    now.Add(20 * time.Second),
		IssuedAt:     now.Add(20 * time.Second),
		HasNotBefore: true,
		HasIssuedAt:  true,
	}, JWTValidationOptions{
		MinRemainingTTL: time.Minute,
		ClockSkewLeeway: 30 * time.Second,
		Clock:           &fakeClock{now: now},
	})
	if err != nil {
		t.Fatalf("expected claims within leeway to pass: %v", err)
	}
}

func TestValidateClaimsRejectsIssuedAtBeyondClockSkewLeeway(t *testing.T) {
	now := time.Unix(testCurrentUnix, 0).UTC()
	err := ValidateClaims(Claims{
		ExpiresAt:   now.Add(time.Hour),
		IssuedAt:    now.Add(time.Minute),
		HasIssuedAt: true,
	}, JWTValidationOptions{
		MinRemainingTTL: time.Minute,
		ClockSkewLeeway: 30 * time.Second,
		Clock:           &fakeClock{now: now},
	})
	if !errors.Is(err, ErrJWTIssuedInFuture) {
		t.Fatalf("expected future iat error, got %v", err)
	}
}

func TestReadAndValidateJWTRejectsUnreadableAndEmptyFiles(t *testing.T) {
	_, err := ReadAndValidateJWT(filepath.Join(t.TempDir(), "missing.jwt"), JWTValidationOptions{
		MinRemainingTTL: time.Minute,
		Clock:           &fakeClock{now: time.Unix(testCurrentUnix, 0).UTC()},
	})
	if !errors.Is(err, ErrJWTRead) {
		t.Fatalf("expected jwt read error, got %v", err)
	}

	path := filepath.Join(t.TempDir(), "empty.jwt")
	if err := os.WriteFile(path, []byte("\n"), 0o600); err != nil {
		t.Fatalf("write empty jwt: %v", err)
	}
	_, err = ReadAndValidateJWT(path, JWTValidationOptions{
		MinRemainingTTL: time.Minute,
		Clock:           &fakeClock{now: time.Unix(testCurrentUnix, 0).UTC()},
	})
	if !errors.Is(err, ErrJWTRead) {
		t.Fatalf("expected jwt read error, got %v", err)
	}
}

func loadJWTFixture(t *testing.T, name string) string {
	t.Helper()

	// #nosec G304 -- fixture names are supplied by tests in this package.
	content, err := os.ReadFile(filepath.Join(authFixtureDir, name))
	if err != nil {
		t.Fatalf("read jwt fixture: %v", err)
	}
	return strings.TrimSpace(string(content))
}
