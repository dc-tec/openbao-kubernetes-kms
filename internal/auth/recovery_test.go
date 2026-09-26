package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
	"github.com/dc-tec/openbao-kubernetes-kms/test/fakes"
)

// finishRefresh observes the attempt started by Token without forcing another login.
func finishRefresh(t *testing.T, manager *Manager) {
	t.Helper()
	manager.mu.Lock()
	flight := manager.flight
	manager.mu.Unlock()
	if flight == nil {
		return
	}
	select {
	case <-flight.done:
	case <-time.After(2 * time.Second):
		t.Fatal("refresh did not finish")
	}
}

type recoveryLoginSource struct {
	login func(context.Context) (LoginResult, error)
}

func (s recoveryLoginSource) Login(ctx context.Context, _ OpenBaoAuthClient, _ Clock) (LoginResult, error) {
	return s.login(ctx)
}
func (recoveryLoginSource) SourceInfo() SourceInfo { return SourceInfo{AuthMethod: authMethodJWT} }

func recoveryManager(t *testing.T, source recoveryLoginSource, opts ManagerOptions) *Manager {
	t.Helper()
	if opts.LifecycleContext == nil {
		opts.LifecycleContext = t.Context()
	}
	opts.RefreshRetryJitter = noRetryJitter
	manager, err := NewManagerWithSource(LifecycleConfig{
		LoginBeforeTokenExpiry: 30 * time.Second,
		TokenRenewalIncrement:  time.Minute,
	}, source, &fakes.OpenBaoAuthClient{}, opts)
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

func recoveryLogin(token string) LoginResult {
	return LoginResult{AuthToken: openbao.AuthToken{ClientToken: token, LeaseDuration: time.Minute}}
}

func TestEarlyRefreshReturnsValidTokenWithoutWaiting(t *testing.T) {
	clock := &fakeClock{now: time.Unix(testCurrentUnix, 0)}
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	manager := recoveryManager(t, recoveryLoginSource{login: func(ctx context.Context) (LoginResult, error) {
		if calls.Add(1) == 1 {
			return recoveryLogin(testBaoToken1), nil
		}
		close(started)
		select {
		case <-ctx.Done():
			return LoginResult{}, ctx.Err()
		case <-release:
			return recoveryLogin(testBaoToken2), nil
		}
	}}, ManagerOptions{Clock: clock})
	if _, err := manager.Token(t.Context()); err != nil {
		t.Fatal(err)
	}
	clock.advance(40 * time.Second)
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	if token, err := manager.Token(ctx); err != nil || token != testBaoToken1 {
		t.Fatalf("valid token blocked by early refresh: %v", err)
	}
	<-started
	for range 8 {
		if token, err := manager.Token(ctx); err != nil || token != testBaoToken1 {
			t.Fatalf("valid token blocked by shared refresh: %v", err)
		}
	}
	clock.advance(21 * time.Second)
	expiredCtx, stop := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer stop()
	if _, err := manager.Token(expiredCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expired token returned: %v", err)
	}
	close(release)
	finishRefresh(t, manager)
	if token, err := manager.Token(t.Context()); err != nil || token != testBaoToken2 {
		t.Fatalf("replacement unavailable: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatal("refresh was not coalesced")
	}
}

func TestInitialLoginSurvivesInitiatorCancellation(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	manager := recoveryManager(t, recoveryLoginSource{login: func(ctx context.Context) (LoginResult, error) {
		close(started)
		select {
		case <-ctx.Done():
			return LoginResult{}, ctx.Err()
		case <-release:
			return recoveryLogin(testBaoToken1), nil
		}
	}}, ManagerOptions{})
	ctx, cancel := context.WithCancel(t.Context())
	leader := make(chan error, 1)
	go func() { _, err := manager.Token(ctx); leader <- err }()
	<-started
	cancel()
	if err := <-leader; !errors.Is(err, context.Canceled) {
		t.Fatalf("initiator cancellation lost: %v", err)
	}
	close(release)
	if token, err := manager.Token(t.Context()); err != nil || token != testBaoToken1 {
		t.Fatalf("shared login canceled: %v", err)
	}
	if state := manager.State(); state.ConsecutiveFailures != 0 || state.LastError != "" {
		t.Fatalf("cancellation counted as auth failure: %#v", state)
	}
}

func TestRefreshUsesManagerDeadlineAndLifecycle(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(fmt.Sprint(shutdown), func(t *testing.T) {
			lifecycle, cancel := context.WithCancel(t.Context())
			defer cancel()
			started := make(chan struct{})
			manager := recoveryManager(t, recoveryLoginSource{login: func(ctx context.Context) (LoginResult, error) {
				if _, ok := ctx.Deadline(); !ok {
					return LoginResult{}, errors.New("missing manager deadline")
				}
				close(started)
				<-ctx.Done()
				return LoginResult{}, ctx.Err()
			}}, ManagerOptions{LifecycleContext: lifecycle, RefreshTimeout: 50 * time.Millisecond})
			done := make(chan error, 1)
			go func() { _, err := manager.Token(t.Context()); done <- err }()
			<-started
			expected := context.DeadlineExceeded
			if shutdown {
				cancel()
				expected = context.Canceled
			}
			select {
			case err := <-done:
				if !errors.Is(err, expected) || !errors.Is(err, ErrAuthFailed) {
					t.Fatalf("wrong refresh error: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("shared refresh exceeded deadline")
			}
			finishRefresh(t, manager)
			failures := manager.State().ConsecutiveFailures
			if shutdown && failures != 0 || !shutdown && failures != 1 {
				t.Fatalf("unexpected failure count %d", failures)
			}
		})
	}
}

func TestTokenRecoveryCoalescesRejectsStaleFailuresAndThrottles(t *testing.T) {
	clock := &fakeClock{now: time.Unix(testCurrentUnix, 0)}
	var calls atomic.Int32
	manager := recoveryManager(t, recoveryLoginSource{login: func(context.Context) (LoginResult, error) {
		return recoveryLogin(fmt.Sprintf("test-token-%d", calls.Add(1))), nil
	}}, ManagerOptions{Clock: clock})
	old, err := manager.Token(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			token, err := manager.RecoverToken(t.Context(), old)
			if err != nil || token == old || token == "" {
				t.Errorf("recovery failed: %v", err)
			}
		})
	}
	wg.Wait()
	current, err := manager.Token(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected one recovery login, got %d total", calls.Load())
	}
	if token, err := manager.RecoverToken(t.Context(), old); err != nil || token != current {
		t.Fatalf("late rejection invalidated replacement: %v", err)
	}
	if token, err := manager.RecoverToken(t.Context(), current); err != nil || token != "" {
		t.Fatalf("repeated recovery was not throttled: %v", err)
	}
	clock.advance(tokenRecoveryInterval)
	if token, err := manager.RecoverToken(t.Context(), current); err != nil || token == "" || token == current {
		t.Fatalf("recovery did not resume: %v", err)
	}
}

func TestRejectedTokenIsNotFallbackAfterFailedLogin(t *testing.T) {
	clock := &fakeClock{now: time.Unix(testCurrentUnix, 0)}
	var calls atomic.Int32
	cause := &openbao.Error{Class: openbao.ErrorClassPermissionDenied, StatusCode: 403, Operation: "jwt login"}
	manager := recoveryManager(t, recoveryLoginSource{login: func(context.Context) (LoginResult, error) {
		if calls.Add(1) == 1 {
			return recoveryLogin(testBaoToken1), nil
		}
		return LoginResult{}, cause
	}}, ManagerOptions{Clock: clock})
	token, err := manager.Token(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		_, err = manager.RecoverToken(t.Context(), token)
		var apiErr *openbao.Error
		if !errors.Is(err, ErrAuthFailed) || !errors.As(err, &apiErr) || apiErr != cause {
			t.Fatalf("lost auth cause: %v", err)
		}
		if value, err := manager.Token(t.Context()); err == nil || value != "" {
			t.Fatal("returned rejected token during login backoff")
		}
	}
	if calls.Load() != 2 {
		t.Fatal("failed recovery bypassed login backoff")
	}
}

func TestPublicAuthErrorPreservesCauseWithoutDisclosure(t *testing.T) {
	cause := errors.New("secret token payload")
	err := publicAuthError(cause)
	if !errors.Is(err, cause) || !errors.Is(err, ErrAuthFailed) {
		t.Fatal("error chain lost")
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatal("auth error disclosed cause")
	}
}

type recoveryRenewClient struct {
	fakes.OpenBaoAuthClient
	renew func(context.Context, string) (openbao.AuthToken, error)
}

func (c *recoveryRenewClient) RenewSelfToken(
	ctx context.Context, token string, _ time.Duration,
) (openbao.AuthToken, error) {
	return c.renew(ctx, token)
}

func TestLateRenewalCannotRestoreRejectedToken(t *testing.T) {
	clock := &fakeClock{now: time.Unix(testCurrentUnix, 0)}
	started, release := make(chan struct{}), make(chan struct{})
	var logins atomic.Int32
	manager := recoveryManager(t, recoveryLoginSource{login: func(context.Context) (LoginResult, error) {
		result := recoveryLogin(fmt.Sprintf("test-token-%d", logins.Add(1)))
		result.AuthToken.Renewable = true
		return result, nil
	}}, ManagerOptions{Clock: clock, RenewalEnabled: true})
	manager.client = &recoveryRenewClient{renew: func(ctx context.Context, token string) (openbao.AuthToken, error) {
		close(started)
		select {
		case <-ctx.Done():
			return openbao.AuthToken{}, ctx.Err()
		case <-release:
			return openbao.AuthToken{ClientToken: token, LeaseDuration: time.Hour, Renewable: true}, nil
		}
	}}
	old, err := manager.Token(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	clock.advance(40 * time.Second)
	if _, err := manager.Token(t.Context()); err != nil {
		t.Fatal(err)
	}
	<-started
	done := make(chan error, 1)
	go func() {
		replacement, err := manager.RecoverToken(t.Context(), old)
		if err == nil && (replacement == old || replacement == "") {
			err = errors.New("rejected token restored by renewal")
		}
		done <- err
	}()
	deadline := time.Now().Add(time.Second)
	for manager.State().TokenTTL != 0 {
		if time.Now().After(deadline) {
			t.Fatal("recovery did not invalidate token")
		}
		time.Sleep(time.Millisecond)
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("recovery did not finish")
	}
	if logins.Load() != 2 {
		t.Fatal("renewal prevented fresh login")
	}
}
