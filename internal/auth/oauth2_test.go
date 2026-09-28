package auth

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/oauth2"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
	"github.com/dc-tec/openbao-kubernetes-kms/test/fakes"
)

type oauthSourceFixture struct {
	beforeResponse func()
	expiresIn      atomic.Int64
	token          atomic.Value
	unavailable    atomic.Bool
	requests       atomic.Int32
	cfg            ManagerConfig
	endpoint       oauth2.Config
	clock          *fakeClock
}

func newOAuthSourceFixture(t *testing.T) *oauthSourceFixture {
	t.Helper()
	f := &oauthSourceFixture{clock: &fakeClock{now: time.Unix(testCurrentUnix, 0)}}
	raw := loadJWTFixture(t, validJWTFixture)
	f.token.Store(raw)
	f.expiresIn.Store(300)
	claims, err := ParseClaims(raw)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		f.requests.Add(1)
		if f.beforeResponse != nil {
			f.beforeResponse()
		}
		if f.unavailable.Load() {
			http.Error(w, "issuer response must stay private", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		// #nosec G117 -- this test issuer returns synthetic JWT fixtures through the token protocol.
		_ = json.NewEncoder(w).Encode(struct {
			AccessToken string `json:"access_token"`
			TokenType   string `json:"token_type"`
			ExpiresIn   int64  `json:"expires_in"`
		}{f.token.Load().(string), "Bearer", f.expiresIn.Load()})
	}))
	t.Cleanup(server.Close)
	dir := t.TempDir()
	ca := filepath.Join(dir, "issuer-ca.pem")
	secret := filepath.Join(dir, "client-secret")
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := os.WriteFile(ca, caPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secret, []byte("test-client-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.endpoint = oauth2.Config{
		TokenURL: server.URL, ClientID: "client", ClientSecretFile: secret,
		AuthMethod: oauth2.ClientSecretBasic, CACertFile: ca, Timeout: time.Second,
	}
	f.cfg = ManagerConfig{
		MountPath: testAuthMountPath, Role: testAuthRole, MinJWTRemainingTTL: testMinJWTRemainingTTL,
		LoginBeforeTokenExpiry: testLoginBeforeExpiry, TokenRenewalIncrement: testRenewalIncrement,
		ExpectedIssuer: claims.Issuer, ExpectedAudience: claims.Audience, ExpectedSubject: claims.Subject,
	}
	return f
}

func TestOAuth2SourceChecksJWTBeforeOpenBaoLogin(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*oauthSourceFixture)
		want   error
	}{
		{"opaque", func(f *oauthSourceFixture) { f.token.Store("opaque-access-token") }, ErrJWTMalformed},
		{"unsigned", func(f *oauthSourceFixture) {
			raw := f.token.Load().(string)
			f.token.Store(raw[:strings.LastIndex(raw, ".")+1])
		}, ErrJWTMalformed},
		{"short response lifetime", func(f *oauthSourceFixture) { f.expiresIn.Store(30) }, ErrJWTNearExpiry},
		{"expired", func(f *oauthSourceFixture) { f.clock.advance(100 * 365 * 24 * time.Hour) }, ErrJWTExpired},
		{"issuer", func(f *oauthSourceFixture) { f.cfg.ExpectedIssuer = "https://wrong.example" }, ErrJWTIssuerMismatch},
		{"audience", func(f *oauthSourceFixture) { f.cfg.ExpectedAudience = []string{"wrong"} }, ErrJWTAudienceMismatch},
		{"subject", func(f *oauthSourceFixture) { f.cfg.ExpectedSubject = "wrong" }, ErrJWTSubjectMismatch},
		{"outage", func(f *oauthSourceFixture) { f.unavailable.Store(true) }, oauth2.ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newOAuthSourceFixture(t)
			tc.change(f)
			source, err := NewOAuth2LoginSource(f.cfg, f.endpoint)
			if err != nil {
				t.Fatal(err)
			}
			bao := &fakes.OpenBaoAuthClient{}
			_, err = source.Login(t.Context(), bao, f.clock)
			if !errors.Is(err, tc.want) {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(bao.Logins()) != 0 {
				t.Fatal("invalid issuer credential reached OpenBao")
			}
		})
	}
}

func TestOAuth2ManagerAcquiresOnLoginAndRecoversAfterOutage(t *testing.T) {
	f := newOAuthSourceFixture(t)
	source, err := NewOAuth2LoginSource(f.cfg, f.endpoint)
	if err != nil {
		t.Fatal(err)
	}
	bao := &fakes.OpenBaoAuthClient{LoginResponses: []openbao.AuthToken{
		{ClientToken: testBaoToken1, LeaseDuration: time.Minute},
		{ClientToken: testBaoToken2, LeaseDuration: time.Minute},
	}}
	manager, err := NewManagerWithSource(lifecycleConfigFromManager(f.cfg), source, bao, ManagerOptions{
		LifecycleContext: t.Context(), Clock: f.clock, RefreshTimeout: time.Second, RefreshRetryJitter: noRetryJitter,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertSharedOAuthStartup(t, f, manager)
	f.unavailable.Store(true)
	if err := manager.Refresh(t.Context()); !errors.Is(err, oauth2.ErrUnavailable) {
		t.Fatalf("outage not classified: %v", err)
	}
	if token, err := manager.Token(t.Context()); err != nil || token != testBaoToken1 {
		t.Fatal("valid OpenBao token lost during issuer outage")
	}
	if strings.Contains(manager.State().LastError, "issuer response") {
		t.Fatal("remote error leaked")
	}
	f.clock.advance(2 * time.Minute)
	if _, err := manager.Token(t.Context()); err == nil {
		t.Fatal("expired OpenBao token used during issuer outage")
	}
	f.unavailable.Store(false)
	// The fake OpenBao client does not verify signatures; preserve identity claims.
	parts := strings.Split(f.token.Load().(string), ".")
	f.token.Store(strings.Join(parts[:2], ".") + ".c2lnbmF0dXJlMg")
	f.clock.advance(time.Minute)
	token, err := manager.Token(t.Context())
	if err != nil || token != testBaoToken2 {
		t.Fatalf("issuer recovery failed: %v", err)
	}
	logins := bao.Logins()
	if len(logins) != 2 || logins[1].JWT != f.token.Load().(string) {
		t.Fatal("recovery did not acquire a fresh JWT")
	}
}

func TestOAuth2SourceUsesSharedDeadline(t *testing.T) {
	f := newOAuthSourceFixture(t)
	source, err := NewOAuth2LoginSource(f.cfg, f.endpoint)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	bao := &fakes.OpenBaoAuthClient{}
	if _, err := source.Login(ctx, bao, f.clock); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation not propagated: %v", err)
	}
	if len(bao.Logins()) != 0 {
		t.Fatal("OpenBao login attempted after cancellation")
	}
}

func assertSharedOAuthStartup(t *testing.T, f *oauthSourceFixture, manager *Manager) {
	t.Helper()

	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			if token, err := manager.Token(t.Context()); err != nil || token != testBaoToken1 {
				t.Error("shared startup login failed")
			}
		})
	}
	wg.Wait()
	if f.requests.Load() != 1 {
		t.Fatal("concurrent startup requests did not share token acquisition")
	}
}

func TestOAuthRelativeLifetimeSurvivesWallRollback(t *testing.T) {
	f := newOAuthSourceFixture(t)
	f.expiresIn.Store(90)
	f.beforeResponse = func() { f.clock.advance(40 * time.Second); f.clock.jumpWall(-time.Hour) }
	source, err := NewOAuth2LoginSource(f.cfg, f.endpoint)
	if err != nil {
		t.Fatal(err)
	}
	bao := &fakes.OpenBaoAuthClient{}
	if _, err := source.Login(t.Context(), bao, f.clock); !errors.Is(err, ErrJWTNearExpiry) {
		t.Fatalf("relative expiry was extended by wall rollback: %v", err)
	}
	if len(bao.Logins()) != 0 {
		t.Fatal("near-expiry OAuth credential reached OpenBao")
	}
}

func TestOAuthKeepsSignedExpiryAndReportsRelativeLifetime(t *testing.T) {
	f := newOAuthSourceFixture(t)
	f.expiresIn.Store(90)
	source, err := NewOAuth2LoginSource(f.cfg, f.endpoint)
	if err != nil {
		t.Fatal(err)
	}
	bao := &fakes.OpenBaoAuthClient{LoginResponses: []openbao.AuthToken{
		{ClientToken: testBaoToken1, LeaseDuration: time.Minute},
	}}
	result, err := source.Login(t.Context(), bao, f.clock)
	if err != nil {
		t.Fatal(err)
	}
	original, err := ParseClaims(f.token.Load().(string))
	if err != nil {
		t.Fatal(err)
	}
	if !result.JWT.Claims.ExpiresAt.Equal(original.ExpiresAt) {
		t.Fatal("signed claim overwritten by relative lifetime")
	}
	f.clock.jumpWall(-time.Hour)
	f.clock.advance(40 * time.Second)
	if got := result.JWT.EndpointLifetime.Remaining(f.clock.Read()); got != 50*time.Second {
		t.Fatalf("unexpected relative lifetime after clock correction: %s", got)
	}
}
