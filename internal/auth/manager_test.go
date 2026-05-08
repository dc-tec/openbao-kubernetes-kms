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
)

const (
	testAuthMountPath      = "auth/k8s-workload-a-jwt"
	testAuthRole           = "openbao-kms-control-plane"
	testBaoToken1          = "bao-token-1"
	testBaoToken2          = "bao-token-2"
	testLoginBeforeExpiry  = 30 * time.Second
	testMinJWTRemainingTTL = time.Minute
)

func TestManagerLogsInAndReturnsToken(t *testing.T) {
	clock := &fakeClock{now: time.Unix(testCurrentUnix, 0).UTC()}
	jwtPath := writeJWTFile(t, loadJWTFixture(t, validJWTFixture))
	client := &fakeOpenBaoAuthClient{
		loginResponses: []openbao.AuthToken{{
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
	if len(client.logins) != 1 || client.logins[0].JWT != loadJWTFixture(t, validJWTFixture) {
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
	manager := newTestManager(t, jwtPath, &fakeOpenBaoAuthClient{}, clock, false)

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
	client := &fakeOpenBaoAuthClient{}
	manager := newTestManager(t, jwtPath, client, clock, false)

	_, err := manager.Token(context.Background())
	if !errors.Is(err, ErrJWTNearExpiry) {
		t.Fatalf("expected near-expiry JWT error, got %v", err)
	}
	if len(client.logins) != 0 {
		t.Fatal("near-expiry JWT should fail before OpenBao login")
	}
}

func TestManagerReReadsJWTBeforeReLogin(t *testing.T) {
	clock := &fakeClock{now: time.Unix(testCurrentUnix, 0).UTC()}
	jwtPath := writeJWTFile(t, loadJWTFixture(t, validJWTFixture))
	client := &fakeOpenBaoAuthClient{
		loginResponses: []openbao.AuthToken{
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
	if len(client.logins) != 2 {
		t.Fatalf("expected two logins, got %d", len(client.logins))
	}
	if client.logins[1].JWT != loadJWTFixture(t, rotatedJWTFixture) {
		t.Fatal("manager did not re-read JWT file before re-login")
	}
}

func TestManagerRenewsWhenEnabledAndRenewable(t *testing.T) {
	clock := &fakeClock{now: time.Unix(testCurrentUnix, 0).UTC()}
	jwtPath := writeJWTFile(t, loadJWTFixture(t, validJWTFixture))
	client := &fakeOpenBaoAuthClient{
		loginResponses: []openbao.AuthToken{{
			ClientToken:   testBaoToken1,
			LeaseDuration: time.Minute,
			Renewable:     true,
		}},
		renewResponses: []openbao.AuthToken{{
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
	if len(client.logins) != 1 || len(client.renewals) != 1 {
		t.Fatalf("expected one login and one renewal, got %d/%d", len(client.logins), len(client.renewals))
	}
	if client.renewals[0].increment != testLoginBeforeExpiry {
		t.Fatalf("unexpected renewal increment: %s", client.renewals[0].increment)
	}
}

func TestManagerRecordsRenewalFailureWhenReloginSucceeds(t *testing.T) {
	clock := &fakeClock{now: time.Unix(testCurrentUnix, 0).UTC()}
	jwtPath := writeJWTFile(t, loadJWTFixture(t, validJWTFixture))
	client := &fakeOpenBaoAuthClient{
		loginResponses: []openbao.AuthToken{
			{ClientToken: testBaoToken1, LeaseDuration: time.Minute, Renewable: true},
			{ClientToken: testBaoToken2, LeaseDuration: time.Minute, Renewable: true},
		},
		renewErr: errors.New("renew unavailable"),
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
	client := &fakeOpenBaoAuthClient{
		loginResponses: []openbao.AuthToken{{
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
	client := &fakeOpenBaoAuthClient{
		loginResponses: []openbao.AuthToken{{
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
	}, client, ManagerOptions{
		Clock:               clock,
		RefreshRetryBackoff: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	if _, err := manager.Token(context.Background()); err != nil {
		t.Fatalf("initial token: %v", err)
	}
	client.loginErr = errors.New("login endpoint unavailable")
	clock.advance(40 * time.Second)

	token, err := manager.Token(context.Background())
	if err != nil {
		t.Fatalf("current token should be usable during failed refresh: %v", err)
	}
	if token != testBaoToken1 {
		t.Fatalf("unexpected token during failed refresh")
	}
	if len(client.logins) != 2 {
		t.Fatalf("expected one failed refresh attempt, got %d logins", len(client.logins))
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
	if len(client.logins) != 2 {
		t.Fatalf("retry backoff should suppress another login, got %d logins", len(client.logins))
	}
}

func TestManagerCoalescesConcurrentInitialLogins(t *testing.T) {
	clock := &fakeClock{now: time.Unix(testCurrentUnix, 0).UTC()}
	jwtPath := writeJWTFile(t, loadJWTFixture(t, validJWTFixture))
	client := &blockingOpenBaoAuthClient{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
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

	<-client.started
	close(client.release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent token call failed: %v", err)
		}
	}
	if client.loginCount() != 1 {
		t.Fatalf("expected one coalesced login, got %d", client.loginCount())
	}
}

func TestManagerStateRedactsUnexpectedErrors(t *testing.T) {
	clock := &fakeClock{now: time.Unix(testCurrentUnix, 0).UTC()}
	jwtPath := writeJWTFile(t, loadJWTFixture(t, validJWTFixture))
	client := &fakeOpenBaoAuthClient{loginErr: errors.New("leaked token value")}
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

type fakeOpenBaoAuthClient struct {
	logins         []openbao.JWTLoginRequest
	renewals       []renewalCall
	loginResponses []openbao.AuthToken
	renewResponses []openbao.AuthToken
	loginErr       error
	renewErr       error
}

type blockingOpenBaoAuthClient struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
	mu      sync.Mutex
	logins  int
}

type renewalCall struct {
	token     string
	increment time.Duration
}

func (f *fakeOpenBaoAuthClient) LoginJWT(_ context.Context, req openbao.JWTLoginRequest) (openbao.AuthToken, error) {
	f.logins = append(f.logins, req)
	if f.loginErr != nil {
		return openbao.AuthToken{}, f.loginErr
	}
	if len(f.loginResponses) == 0 {
		return openbao.AuthToken{}, errors.New("missing fake login response")
	}
	response := f.loginResponses[0]
	f.loginResponses = f.loginResponses[1:]
	return response, nil
}

func (f *fakeOpenBaoAuthClient) RenewSelfToken(
	_ context.Context,
	token string,
	increment time.Duration,
) (openbao.AuthToken, error) {
	f.renewals = append(f.renewals, renewalCall{token: token, increment: increment})
	if f.renewErr != nil {
		return openbao.AuthToken{}, f.renewErr
	}
	if len(f.renewResponses) == 0 {
		return openbao.AuthToken{}, errors.New("missing fake renew response")
	}
	response := f.renewResponses[0]
	f.renewResponses = f.renewResponses[1:]
	return response, nil
}

func (b *blockingOpenBaoAuthClient) LoginJWT(
	ctx context.Context,
	_ openbao.JWTLoginRequest,
) (openbao.AuthToken, error) {
	b.mu.Lock()
	b.logins++
	b.once.Do(func() {
		close(b.started)
	})
	b.mu.Unlock()

	select {
	case <-b.release:
	case <-ctx.Done():
		return openbao.AuthToken{}, ctx.Err()
	}
	return openbao.AuthToken{
		ClientToken:   testBaoToken1,
		LeaseDuration: time.Minute,
	}, nil
}

func (b *blockingOpenBaoAuthClient) RenewSelfToken(
	_ context.Context,
	_ string,
	_ time.Duration,
) (openbao.AuthToken, error) {
	return openbao.AuthToken{}, errors.New("renew should not be called")
}

func (b *blockingOpenBaoAuthClient) loginCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.logins
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
	}, client, ManagerOptions{
		Clock:          clock,
		RenewalEnabled: renewalEnabled,
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
