package auth

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func (f *fakeClock) jumpWall(delta time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(delta)
}

func TestTokenExpiryCannotBeReversedByClockCorrection(t *testing.T) {
	for _, forward := range []bool{false, true} {
		t.Run(map[bool]string{false: "backward", true: "forward_then_corrected"}[forward], func(t *testing.T) {
			clock := &fakeClock{now: time.Unix(testCurrentUnix, 0)}
			var logins atomic.Int32
			manager := recoveryManager(t, recoveryLoginSource{login: func(context.Context) (LoginResult, error) {
				if logins.Add(1) == 1 {
					return recoveryLogin(testBaoToken1), nil
				}
				return LoginResult{}, ErrAuthFailed
			}}, ManagerOptions{Clock: clock})
			if _, err := manager.Token(t.Context()); err != nil {
				t.Fatal(err)
			}
			if forward {
				clock.jumpWall(time.Hour)
				if manager.State().TokenTTL > 0 {
					t.Fatal("forward correction did not expire token")
				}
				clock.jumpWall(-time.Hour)
			} else {
				clock.jumpWall(-time.Hour)
				clock.advance(time.Minute)
			}
			if manager.State().TokenTTL > 0 {
				t.Fatal("clock correction extended token lifetime")
			}
			if _, err := manager.Token(t.Context()); !errors.Is(err, ErrAuthFailed) {
				t.Fatalf("expired token escaped failed reauthentication: %v", err)
			}
			if logins.Load() != 2 {
				t.Fatal("expected one reauthentication attempt")
			}
		})
	}
}

func TestAuthRetryCooldownUsesElapsedTime(t *testing.T) {
	clock := &fakeClock{now: time.Unix(testCurrentUnix, 0)}
	var logins atomic.Int32
	manager := recoveryManager(t, recoveryLoginSource{login: func(context.Context) (LoginResult, error) {
		logins.Add(1)
		return LoginResult{}, ErrAuthFailed
	}}, ManagerOptions{Clock: clock, RefreshRetryBackoff: time.Second, MaxRefreshRetryBackoff: time.Second})
	_, _ = manager.Token(t.Context())
	clock.jumpWall(time.Hour)
	_, _ = manager.Token(t.Context())
	if logins.Load() != 1 {
		t.Fatal("forward jump bypassed retry cooldown")
	}
	clock.jumpWall(-2 * time.Hour)
	clock.advance(time.Second)
	_, _ = manager.Token(t.Context())
	if logins.Load() != 2 {
		t.Fatal("backward jump prolonged retry cooldown")
	}
}
