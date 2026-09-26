package auth

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var errRefreshSuperseded = errors.New("token refresh superseded")

const tokenRecoveryInterval = 5 * time.Second

type refreshFlight struct {
	done chan struct{}
	kind refreshKind
	err  error
}

func (m *Manager) ensureToken(ctx context.Context, forceLogin bool) (currentToken, error) {
	if ctx == nil {
		return currentToken{}, fmt.Errorf("%w: context is required", ErrAuthConfig)
	}
	for {
		if err := ctx.Err(); err != nil {
			return currentToken{}, err
		}
		if err := m.lifecycle.Err(); err != nil {
			return currentToken{}, err
		}
		m.mu.Lock()
		now := m.clock.Now()
		if !forceLogin && m.current.value != "" && now.Before(m.current.expiresAt) {
			m.startEarlyRefreshLocked(now)
			token := m.current
			m.mu.Unlock()
			return token, nil
		}
		if m.flight == nil && m.retryBlockedLocked(now) {
			err := m.lastErr
			if err == nil {
				err = ErrTokenUnavailable
			}
			m.mu.Unlock()
			return currentToken{}, err
		}
		if m.flight == nil {
			m.startRefreshLocked(forceLogin)
		}
		flight := m.flight
		m.mu.Unlock()
		if err := m.waitForRefresh(ctx, flight); err != nil {
			if errors.Is(err, errRefreshSuperseded) {
				continue
			}
			return currentToken{}, err
		}
		// A concurrent login satisfies forced refresh. A renewal does not.
		if flight.kind == refreshKindLogin {
			forceLogin = false
		}
	}
}

func (m *Manager) startEarlyRefreshLocked(now time.Time) {
	if !now.Before(m.refreshAtLocked()) && !m.retryBlockedLocked(now) && m.flight == nil {
		m.startRefreshLocked(false)
	}
}

func (m *Manager) waitForRefresh(ctx context.Context, flight *refreshFlight) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-m.lifecycle.Done():
		return m.lifecycle.Err()
	case <-flight.done:
		return flight.err
	}
}

func (m *Manager) startRefreshLocked(forceLogin bool) {
	action := m.refreshActionLocked(forceLogin, m.clock.Now())
	flight := &refreshFlight{done: make(chan struct{}), kind: action.kind}
	m.flight = flight
	go m.runRefresh(action, flight)
}

func (m *Manager) runRefresh(action refreshAction, flight *refreshFlight) {
	ctx, cancel := context.WithTimeout(m.lifecycle, m.refreshTimeout)
	defer cancel()
	result := m.performRefresh(ctx, action)
	if err := ctx.Err(); err != nil {
		result.err = publicAuthError(err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	switch {
	case m.lifecycle.Err() != nil:
		flight.err = m.lifecycle.Err()
	case result.kind == refreshKindRenew && action.current.value != m.current.value:
		// A token rejected while renewal was in flight must never be restored.
		flight.err = errRefreshSuperseded
	default:
		flight.err = m.applyRefreshResultLocked(result)
	}
	flight.kind = result.kind
	m.flight = nil
	close(flight.done)
}

// RecoverToken replaces the credential used by a rejected request. A late
// rejection for an old credential joins recovery or uses the current token.
// OpenBao uses 403 for both revoked tokens and policy denials, so repeated
// recovery is limited even when login succeeds. An empty result means throttled.
func (m *Manager) RecoverToken(ctx context.Context, token string) (string, error) {
	if ctx == nil {
		return "", publicAuthError(ErrAuthConfig)
	}
	if err := ctx.Err(); err != nil {
		return "", publicAuthError(err)
	}
	m.mu.Lock()
	if token != "" && token == m.current.value {
		now := m.clock.Now()
		if now.Before(m.nextRecoveryAt) {
			m.mu.Unlock()
			return "", nil
		}
		m.current = currentToken{}
		m.lastErr = publicAuthError(ErrTokenUnavailable)
		m.nextRecoveryAt = now.Add(tokenRecoveryInterval)
	}
	m.mu.Unlock()
	return m.Token(ctx)
}

// authError preserves internal causes while exposing only a redacted message.
type authError struct{ cause error }

func (e *authError) Error() string {
	return fmt.Sprintf("%s: %s", ErrAuthFailed, safeErrorMessage(e.cause))
}
func (e *authError) Unwrap() error        { return e.cause }
func (e *authError) Is(target error) bool { return target == ErrAuthFailed }

func publicAuthError(err error) error {
	if err == nil {
		return nil
	}
	var wrapped *authError
	if errors.As(err, &wrapped) {
		return err
	}
	return &authError{cause: err}
}
