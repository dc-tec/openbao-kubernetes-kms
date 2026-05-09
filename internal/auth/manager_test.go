package auth

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
	"github.com/dc-tec/openbao-kubernetes-kms/test/fakes"
)

const (
	testAuthMountPath      = "auth/k8s-workload-a-jwt"
	testAuthRole           = "openbao-kms-control-plane"
	testBaoToken1          = "bao-token-1"
	testBaoToken2          = "bao-token-2"
	testLoginBeforeExpiry  = 30 * time.Second
	testRenewalIncrement   = 2 * time.Minute
	testMinJWTRemainingTTL = time.Minute
)

func TestManagerLogsInAndReturnsToken(t *testing.T) {
	clock := &fakeClock{now: time.Unix(testCurrentUnix, 0).UTC()}
	jwtPath := writeJWTFile(t, loadJWTFixture(t, validJWTFixture))
	client := &fakes.OpenBaoAuthClient{
		LoginResponses: []openbao.AuthToken{{
			ClientToken:   testBaoToken1,
			LeaseDuration: time.Minute,
			Renewable:     true,
		}},
	}
	manager := newTestManager(t, jwtPath, client, clock, false)

	token, err := manager.Token(context.Background())
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	if token != testBaoToken1 {
		t.Fatalf("unexpected token")
	}
	logins := client.Logins()
	if len(logins) != 1 || logins[0].JWT != loadJWTFixture(t, validJWTFixture) {
		t.Fatalf("expected login with JWT fixture")
	}
	state := manager.State()
	if state.Status != StatusAuthenticated || state.LastTokenSource != tokenSourceMemory {
		t.Fatalf("unexpected state: %#v", state)
	}
}

func TestManagerStateBeforeLoginHasZeroTTLs(t *testing.T) {
	clock := &fakeClock{now: time.Unix(testCurrentUnix, 0).UTC()}
	jwtPath := writeJWTFile(t, loadJWTFixture(t, validJWTFixture))
	manager := newTestManager(t, jwtPath, &fakes.OpenBaoAuthClient{}, clock, false)

	state := manager.State()
	if state.Status != StatusUnknown {
		t.Fatalf("unexpected state: %#v", state)
	}
	if state.TokenTTL != 0 || state.JWTTTL != 0 {
		t.Fatalf("expected zero TTLs before login, got token=%s jwt=%s", state.TokenTTL, state.JWTTTL)
	}
}

func TestManagerRejectsNearExpiryJWTBeforeLogin(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1778281950, 0).UTC()}
	jwtPath := writeJWTFile(t, loadJWTFixture(t, nearExpiryJWTFixture))
	client := &fakes.OpenBaoAuthClient{}
	manager := newTestManager(t, jwtPath, client, clock, false)

	_, err := manager.Token(context.Background())
	if !errors.Is(err, ErrJWTNearExpiry) {
		t.Fatalf("expected near-expiry JWT error, got %v", err)
	}
	if len(client.Logins()) != 0 {
		t.Fatal("near-expiry JWT should fail before OpenBao login")
	}
}

func TestManagerReReadsJWTBeforeReLogin(t *testing.T) {
	clock := &fakeClock{now: time.Unix(testCurrentUnix, 0).UTC()}
	jwtPath := writeJWTFile(t, loadJWTFixture(t, validJWTFixture))
	client := &fakes.OpenBaoAuthClient{
		LoginResponses: []openbao.AuthToken{
			{ClientToken: testBaoToken1, LeaseDuration: time.Minute},
			{ClientToken: testBaoToken2, LeaseDuration: time.Minute},
		},
	}
	manager := newTestManager(t, jwtPath, client, clock, false)

	first, err := manager.Token(context.Background())
	if err != nil {
		t.Fatalf("first token: %v", err)
	}
	if first != testBaoToken1 {
		t.Fatalf("unexpected first token")
	}

	if err := os.WriteFile(jwtPath, []byte(loadJWTFixture(t, rotatedJWTFixture)), 0o600); err != nil {
		t.Fatalf("rotate jwt fixture: %v", err)
	}
	clock.advance(35 * time.Second)
	second, err := manager.Token(context.Background())
	if err != nil {
		t.Fatalf("second token: %v", err)
	}
	if second != testBaoToken2 {
		t.Fatalf("unexpected second token")
	}
	logins := client.Logins()
	if len(logins) != 2 {
		t.Fatalf("expected two logins, got %d", len(logins))
	}
	if logins[1].JWT != loadJWTFixture(t, rotatedJWTFixture) {
		t.Fatal("manager did not re-read JWT file before re-login")
	}
}

func TestManagerRenewsWhenEnabledAndRenewable(t *testing.T) {
	clock := &fakeClock{now: time.Unix(testCurrentUnix, 0).UTC()}
	jwtPath := writeJWTFile(t, loadJWTFixture(t, validJWTFixture))
	client := &fakes.OpenBaoAuthClient{
		LoginResponses: []openbao.AuthToken{{
			ClientToken:   testBaoToken1,
			LeaseDuration: time.Minute,
			Renewable:     true,
		}},
		RenewResponses: []openbao.AuthToken{{
			LeaseDuration: 2 * time.Minute,
			Renewable:     true,
		}},
	}
	manager := newTestManager(t, jwtPath, client, clock, true)

	if _, err := manager.Token(context.Background()); err != nil {
		t.Fatalf("initial token: %v", err)
	}
	clock.advance(40 * time.Second)
	renewed, err := manager.Token(context.Background())
	if err != nil {
		t.Fatalf("renew token: %v", err)
	}
	if renewed != testBaoToken1 {
		t.Fatalf("renewal should retain token when response omits a token")
	}
	logins := client.Logins()
	renewals := client.Renewals()
	if len(logins) != 1 || len(renewals) != 1 {
		t.Fatalf("expected one login and one renewal, got %d/%d", len(logins), len(renewals))
	}
	if renewals[0].Increment != testRenewalIncrement {
		t.Fatalf("unexpected renewal increment: %s", renewals[0].Increment)
	}
}

func TestManagerRecordsRenewalFailureWhenReloginSucceeds(t *testing.T) {
	clock := &fakeClock{now: time.Unix(testCurrentUnix, 0).UTC()}
	jwtPath := writeJWTFile(t, loadJWTFixture(t, validJWTFixture))
	client := &fakes.OpenBaoAuthClient{
		LoginResponses: []openbao.AuthToken{
			{ClientToken: testBaoToken1, LeaseDuration: time.Minute, Renewable: true},
			{ClientToken: testBaoToken2, LeaseDuration: time.Minute, Renewable: true},
		},
		RenewErr: errors.New("renew unavailable"),
	}
	manager := newTestManager(t, jwtPath, client, clock, true)

	if _, err := manager.Token(context.Background()); err != nil {
		t.Fatalf("initial token: %v", err)
	}
	clock.advance(40 * time.Second)
	token, err := manager.Token(context.Background())
	if err != nil {
		t.Fatalf("re-login after renewal failure: %v", err)
	}
	if token != testBaoToken2 {
		t.Fatalf("unexpected fallback login token")
	}
	state := manager.State()
	if state.Status != StatusAuthenticated || state.LastError != "" || state.LastRenewalError == "" {
		t.Fatalf("unexpected state after fallback login: %#v", state)
	}
}

func TestManagerRejectsUnusableShortTokenLease(t *testing.T) {
	clock := &fakeClock{now: time.Unix(testCurrentUnix, 0).UTC()}
	jwtPath := writeJWTFile(t, loadJWTFixture(t, validJWTFixture))
	client := &fakes.OpenBaoAuthClient{
		LoginResponses: []openbao.AuthToken{{
			ClientToken:   testBaoToken1,
			LeaseDuration: testLoginBeforeExpiry,
		}},
	}
	manager := newTestManager(t, jwtPath, client, clock, false)

	_, err := manager.Token(context.Background())
	if !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("expected auth failure for short lease, got %v", err)
	}
}

func TestManagerUsesCurrentTokenDuringRefreshBackoff(t *testing.T) {
	clock := &fakeClock{now: time.Unix(testCurrentUnix, 0).UTC()}
	jwtPath := writeJWTFile(t, loadJWTFixture(t, validJWTFixture))
	client := &fakes.OpenBaoAuthClient{
		LoginResponses: []openbao.AuthToken{{
			ClientToken:   testBaoToken1,
			LeaseDuration: time.Minute,
		}},
	}
	manager, err := NewManager(ManagerConfig{
		MountPath:              testAuthMountPath,
		Role:                   testAuthRole,
		JWTFile:                jwtPath,
		MinJWTRemainingTTL:     testMinJWTRemainingTTL,
		LoginBeforeTokenExpiry: testLoginBeforeExpiry,
		TokenRenewalIncrement:  testRenewalIncrement,
	}, client, ManagerOptions{
		Clock:               clock,
		RefreshRetryBackoff: 10 * time.Second,
		RefreshRetryJitter:  noRetryJitter,
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	if _, err := manager.Token(context.Background()); err != nil {
		t.Fatalf("initial token: %v", err)
	}
	client.LoginErr = errors.New("login endpoint unavailable")
	clock.advance(40 * time.Second)

	token, err := manager.Token(context.Background())
	if err != nil {
		t.Fatalf("current token should be usable during failed refresh: %v", err)
	}
	if token != testBaoToken1 {
		t.Fatalf("unexpected token during failed refresh")
	}
	logins := client.Logins()
	if len(logins) != 2 {
		t.Fatalf("expected one failed refresh attempt, got %d logins", len(logins))
	}
	state := manager.State()
	if state.Status != StatusUnhealthy || state.LastError == "" || state.NextRetryAt.IsZero() {
		t.Fatalf("expected unhealthy state with retry deadline, got %#v", state)
	}

	token, err = manager.Token(context.Background())
	if err != nil {
		t.Fatalf("current token should be returned during retry backoff: %v", err)
	}
	if token != testBaoToken1 {
		t.Fatalf("unexpected token during retry backoff")
	}
	logins = client.Logins()
	if len(logins) != 2 {
		t.Fatalf("retry backoff should suppress another login, got %d logins", len(logins))
	}
}

func TestManagerUsesExponentialRefreshBackoff(t *testing.T) {
	clock := &fakeClock{now: time.Unix(testCurrentUnix, 0).UTC()}
	jwtPath := writeJWTFile(t, loadJWTFixture(t, validJWTFixture))
	client := &fakes.OpenBaoAuthClient{LoginErr: errors.New("login endpoint unavailable")}
	manager, err := NewManager(ManagerConfig{
		MountPath:              testAuthMountPath,
		Role:                   testAuthRole,
		JWTFile:                jwtPath,
		MinJWTRemainingTTL:     testMinJWTRemainingTTL,
		LoginBeforeTokenExpiry: testLoginBeforeExpiry,
		TokenRenewalIncrement:  testRenewalIncrement,
	}, client, ManagerOptions{
		Clock:                  clock,
		RefreshRetryBackoff:    time.Second,
		MaxRefreshRetryBackoff: 4 * time.Second,
		RefreshRetryJitter:     noRetryJitter,
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	expectedBackoffs := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 4 * time.Second}
	for attempt, expected := range expectedBackoffs {
		_, err := manager.Token(context.Background())
		if !errors.Is(err, ErrAuthFailed) {
			t.Fatalf("attempt %d: expected auth failure, got %v", attempt+1, err)
		}
		state := manager.State()
		if state.ConsecutiveFailures != attempt+1 {
			t.Fatalf("attempt %d: expected %d failures, got %#v", attempt+1, attempt+1, state)
		}
		if got := state.NextRetryAt.Sub(clock.now); got != expected {
			t.Fatalf("attempt %d: expected retry backoff %s, got %s", attempt+1, expected, got)
		}
		clock.advance(expected)
	}
	if len(client.Logins()) != len(expectedBackoffs) {
		t.Fatalf("expected %d login attempts, got %d", len(expectedBackoffs), len(client.Logins()))
	}
}

func TestManagerCoalescesConcurrentInitialLogins(t *testing.T) {
	clock := &fakeClock{now: time.Unix(testCurrentUnix, 0).UTC()}
	jwtPath := writeJWTFile(t, loadJWTFixture(t, validJWTFixture))
	client := fakes.NewBlockingOpenBaoAuthClient(openbao.AuthToken{
		ClientToken:   testBaoToken1,
		LeaseDuration: time.Minute,
	})
	manager := newTestManager(t, jwtPath, client, clock, false)

	const callers = 8
	var wg sync.WaitGroup
	errs := make(chan error, callers)
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			token, err := manager.Token(context.Background())
			if err != nil {
				errs <- err
				return
			}
			if token != testBaoToken1 {
				errs <- errors.New("unexpected token")
			}
		}()
	}

	<-client.Started()
	client.Release()
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent token call failed: %v", err)
		}
	}
	if client.LoginCount() != 1 {
		t.Fatalf("expected one coalesced login, got %d", client.LoginCount())
	}
}

func TestManagerAppliesRefreshAfterWaiterCancellation(t *testing.T) {
	clock := &fakeClock{now: time.Unix(testCurrentUnix, 0).UTC()}
	jwtPath := writeJWTFile(t, loadJWTFixture(t, validJWTFixture))
	client := fakes.NewBlockingOpenBaoAuthClient(openbao.AuthToken{
		ClientToken:   testBaoToken1,
		LeaseDuration: time.Minute,
	})
	manager := newTestManager(t, jwtPath, client, clock, false)

	leaderDone := make(chan error, 1)
	go func() {
		token, err := manager.Token(context.Background())
		if err != nil {
			leaderDone <- err
			return
		}
		if token != testBaoToken1 {
			leaderDone <- errors.New("unexpected leader token")
			return
		}
		leaderDone <- nil
	}()

	<-client.Started()
	waiterCtx, cancelWaiter := context.WithCancel(context.Background())
	waiterDone := make(chan error, 1)
	go func() {
		_, err := manager.Token(waiterCtx)
		waiterDone <- err
	}()
	cancelWaiter()

	if err := <-waiterDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected waiter cancellation, got %v", err)
	}
	client.Release()
	if err := <-leaderDone; err != nil {
		t.Fatalf("leader token: %v", err)
	}

	token, err := manager.Token(context.Background())
	if err != nil {
		t.Fatalf("token after cancelled waiter: %v", err)
	}
	if token != testBaoToken1 {
		t.Fatalf("unexpected token after cancelled waiter")
	}
	if client.LoginCount() != 1 {
		t.Fatalf("cancelled waiter should not trigger another login, got %d", client.LoginCount())
	}
}

func TestManagerStateRedactsUnexpectedErrors(t *testing.T) {
	clock := &fakeClock{now: time.Unix(testCurrentUnix, 0).UTC()}
	jwtPath := writeJWTFile(t, loadJWTFixture(t, validJWTFixture))
	client := &fakes.OpenBaoAuthClient{LoginErr: errors.New("leaked token value")}
	manager := newTestManager(t, jwtPath, client, clock, false)

	_, err := manager.Token(context.Background())
	if !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("expected auth failure, got %v", err)
	}
	state := manager.State()
	if state.Status != StatusUnhealthy {
		t.Fatalf("unexpected state: %#v", state)
	}
	if strings.Contains(state.LastError, "leaked") || strings.Contains(state.LastError, "token value") {
		t.Fatalf("state error leaked sensitive content: %q", state.LastError)
	}
}

func newTestManager(
	t *testing.T,
	jwtPath string,
	client OpenBaoAuthClient,
	clock *fakeClock,
	renewalEnabled bool,
) *Manager {
	t.Helper()

	manager, err := NewManager(ManagerConfig{
		MountPath:              testAuthMountPath,
		Role:                   testAuthRole,
		JWTFile:                jwtPath,
		MinJWTRemainingTTL:     testMinJWTRemainingTTL,
		LoginBeforeTokenExpiry: testLoginBeforeExpiry,
		TokenRenewalIncrement:  testRenewalIncrement,
	}, client, ManagerOptions{
		Clock:              clock,
		RenewalEnabled:     renewalEnabled,
		RefreshRetryJitter: noRetryJitter,
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	return manager
}

func writeJWTFile(t *testing.T, token string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "identity.jwt")
	if err := os.WriteFile(path, []byte(token), 0o600); err != nil {
		t.Fatalf("write jwt: %v", err)
	}
	return path
}

func noRetryJitter(backoff time.Duration) time.Duration {
	return backoff
}
