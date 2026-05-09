package fakes

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
)

// RenewalCall records one fake OpenBao token renewal request.
type RenewalCall struct {
	Token     string
	Increment time.Duration
}

// OpenBaoAuthClient is a scriptable fake for internal/auth manager tests.
type OpenBaoAuthClient struct {
	mu sync.Mutex

	LoginResponses []openbao.AuthToken
	RenewResponses []openbao.AuthToken
	LoginErr       error
	RenewErr       error

	logins   []openbao.JWTLoginRequest
	renewals []RenewalCall
}

// LoginJWT records a login request and returns the next scripted response.
func (f *OpenBaoAuthClient) LoginJWT(
	_ context.Context,
	req openbao.JWTLoginRequest,
) (openbao.AuthToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.logins = append(f.logins, req)
	if f.LoginErr != nil {
		return openbao.AuthToken{}, f.LoginErr
	}
	if len(f.LoginResponses) == 0 {
		return openbao.AuthToken{}, errors.New("missing fake login response")
	}
	response := f.LoginResponses[0]
	f.LoginResponses = f.LoginResponses[1:]
	return response, nil
}

// RenewSelfToken records a renewal request and returns the next scripted response.
func (f *OpenBaoAuthClient) RenewSelfToken(
	_ context.Context,
	token string,
	increment time.Duration,
) (openbao.AuthToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.renewals = append(f.renewals, RenewalCall{Token: token, Increment: increment})
	if f.RenewErr != nil {
		return openbao.AuthToken{}, f.RenewErr
	}
	if len(f.RenewResponses) == 0 {
		return openbao.AuthToken{}, errors.New("missing fake renew response")
	}
	response := f.RenewResponses[0]
	f.RenewResponses = f.RenewResponses[1:]
	return response, nil
}

// Logins returns recorded JWT login requests.
func (f *OpenBaoAuthClient) Logins() []openbao.JWTLoginRequest {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]openbao.JWTLoginRequest(nil), f.logins...)
}

// Renewals returns recorded token renewal requests.
func (f *OpenBaoAuthClient) Renewals() []RenewalCall {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]RenewalCall(nil), f.renewals...)
}

// TokenSource is a mutable fake OpenBao token source.
type TokenSource struct {
	mu         sync.Mutex
	TokenValue string
	Err        error
	calls      int
}

// Token returns the configured token or error.
func (f *TokenSource) Token(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls++
	if f.Err != nil {
		return "", f.Err
	}
	if f.TokenValue == "" {
		return "", errors.New("missing fake token")
	}
	return f.TokenValue, nil
}

// SetToken replaces the fake token and clears any configured error.
func (f *TokenSource) SetToken(token string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.TokenValue = token
	f.Err = nil
}

// SetError configures Token to return an error.
func (f *TokenSource) SetError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.Err = err
}

// Calls returns the number of Token calls.
func (f *TokenSource) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.calls
}

// BlockingOpenBaoAuthClient blocks the first login until released.
type BlockingOpenBaoAuthClient struct {
	started     chan struct{}
	release     chan struct{}
	startedOnce sync.Once
	releaseOnce sync.Once
	mu          sync.Mutex
	logins      int
	token       openbao.AuthToken
}

// NewBlockingOpenBaoAuthClient builds a fake auth client with a blocking login.
func NewBlockingOpenBaoAuthClient(token openbao.AuthToken) *BlockingOpenBaoAuthClient {
	return &BlockingOpenBaoAuthClient{
		started: make(chan struct{}),
		release: make(chan struct{}),
		token:   token,
	}
}

// Started is closed when the first login starts.
func (b *BlockingOpenBaoAuthClient) Started() <-chan struct{} {
	return b.started
}

// Release unblocks a pending fake login.
func (b *BlockingOpenBaoAuthClient) Release() {
	b.releaseOnce.Do(func() {
		close(b.release)
	})
}

// LoginJWT blocks until Release is called or the context is canceled.
func (b *BlockingOpenBaoAuthClient) LoginJWT(
	ctx context.Context,
	_ openbao.JWTLoginRequest,
) (openbao.AuthToken, error) {
	b.mu.Lock()
	b.logins++
	b.startedOnce.Do(func() {
		close(b.started)
	})
	b.mu.Unlock()

	select {
	case <-b.release:
	case <-ctx.Done():
		return openbao.AuthToken{}, ctx.Err()
	}
	return b.token, nil
}

// RenewSelfToken fails because the blocking fake is only for initial login tests.
func (b *BlockingOpenBaoAuthClient) RenewSelfToken(
	_ context.Context,
	_ string,
	_ time.Duration,
) (openbao.AuthToken, error) {
	return openbao.AuthToken{}, errors.New("renew should not be called")
}

// LoginCount returns the number of fake login requests.
func (b *BlockingOpenBaoAuthClient) LoginCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.logins
}
