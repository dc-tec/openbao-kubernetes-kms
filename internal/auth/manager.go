package auth

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
)

const (
	tokenSourceMemory          = "memory"
	defaultRefreshRetryBackoff = time.Second
	authStatusOK               = "ok"
	authStatusError            = "error"
	authStatusJWTExpired       = "jwt_expired"
	authStatusJWTNearExpiry    = "jwt_near_expiry"
	authStatusJWTInvalid       = "jwt_invalid"
	authStatusAuthFailed       = "auth_failed"
	authStatusNoUsableSession  = "no_usable_session"
)

var (
	// ErrAuthConfig identifies invalid local authentication manager settings.
	ErrAuthConfig = errors.New("auth config invalid")
	// ErrAuthFailed identifies a failed OpenBao authentication operation.
	ErrAuthFailed = errors.New("auth failed")
	// ErrTokenUnavailable identifies a missing or expired in-memory OpenBao token.
	ErrTokenUnavailable = errors.New("openbao token unavailable")
)

var safeAuthErrorClasses = []error{
	ErrJWTRead,
	ErrJWTMalformed,
	ErrJWTExpired,
	ErrJWTNearExpiry,
	ErrJWTNotYetValid,
	ErrJWTIssuedInFuture,
	ErrAuthConfig,
	ErrAuthFailed,
	ErrTokenUnavailable,
}

// Status is a redacted authentication state classification for readiness and status caches.
type Status string

const (
	StatusUnknown       Status = "unknown"
	StatusAuthenticated Status = "authenticated"
	StatusUnhealthy     Status = "unhealthy"
)

// OpenBaoAuthClient is the OpenBao auth API surface used by Manager.
type OpenBaoAuthClient interface {
	LoginJWT(context.Context, openbao.JWTLoginRequest) (openbao.AuthToken, error)
	RenewSelfToken(context.Context, string, time.Duration) (openbao.AuthToken, error)
}

// ManagerConfig contains JWT auth settings after typed config loading.
type ManagerConfig struct {
	MountPath              string
	Role                   string
	JWTFile                string
	MinJWTRemainingTTL     time.Duration
	ClockSkewLeeway        time.Duration
	LoginBeforeTokenExpiry time.Duration
}

// ManagerOptions contains testable lifecycle behavior settings.
type ManagerOptions struct {
	Clock               Clock
	RenewalEnabled      bool
	RefreshRetryBackoff time.Duration
	Observer            Observer
}

// Observer receives redacted auth lifecycle observations.
type Observer interface {
	ObserveAuthLogin(context.Context, string)
	ObserveAuthRenewal(context.Context, string)
}

// State is the redacted auth state exposed to status and readiness code.
type State struct {
	Status           Status
	TokenRenewable   bool
	TokenExpiresAt   time.Time
	TokenTTL         time.Duration
	JWTExpiresAt     time.Time
	JWTTTL           time.Duration
	LastLoginAt      time.Time
	LastRenewalAt    time.Time
	LastError        string
	LastRenewalError string
	NextRetryAt      time.Time
	LastTokenSource  string
}

// Manager keeps the OpenBao token in memory and refreshes it through JWT login.
type Manager struct {
	mu             sync.Mutex
	cfg            ManagerConfig
	client         OpenBaoAuthClient
	clock          Clock
	renewalEnabled bool
	current        currentToken
	lastJWT        JWT
	lastLoginAt    time.Time
	lastRenewalAt  time.Time
	lastErr        error
	lastRenewalErr error
	nextRetryAt    time.Time
	retryBackoff   time.Duration
	refreshing     bool
	refreshDone    chan struct{}
	observer       Observer
}

type currentToken struct {
	value     string
	expiresAt time.Time
	renewable bool
}

// NewManager creates an in-memory token manager.
func NewManager(cfg ManagerConfig, client OpenBaoAuthClient, opts ManagerOptions) (*Manager, error) {
	if client == nil {
		return nil, fmt.Errorf("%w: OpenBao auth client is required", ErrAuthConfig)
	}
	normalized, err := validateManagerConfig(cfg)
	if err != nil {
		return nil, err
	}
	retryBackoff := opts.RefreshRetryBackoff
	if retryBackoff <= 0 {
		retryBackoff = defaultRefreshRetryBackoff
	}

	return &Manager{
		cfg:            normalized,
		client:         client,
		clock:          clockOrReal(opts.Clock),
		renewalEnabled: opts.RenewalEnabled,
		retryBackoff:   retryBackoff,
		observer:       opts.Observer,
	}, nil
}

// Token returns a valid in-memory OpenBao token, refreshing it if needed.
func (m *Manager) Token(ctx context.Context) (string, error) {
	token, err := m.ensureToken(ctx, false)
	if err != nil {
		return "", err
	}
	if token.value == "" {
		return "", ErrTokenUnavailable
	}
	return token.value, nil
}

// Refresh forces a JWT file read and OpenBao JWT login.
func (m *Manager) Refresh(ctx context.Context) error {
	_, err := m.ensureToken(ctx, true)
	return err
}

// State returns redacted auth state for status and readiness code.
func (m *Manager) State() State {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.clock.Now()
	state := State{
		Status:           StatusUnknown,
		TokenRenewable:   m.current.renewable,
		TokenExpiresAt:   m.current.expiresAt,
		TokenTTL:         ttlUntil(now, m.current.expiresAt),
		JWTExpiresAt:     m.lastJWT.Claims.ExpiresAt,
		JWTTTL:           ttlUntil(now, m.lastJWT.Claims.ExpiresAt),
		LastLoginAt:      m.lastLoginAt,
		LastRenewalAt:    m.lastRenewalAt,
		LastError:        safeErrorMessage(m.lastErr),
		LastRenewalError: safeErrorMessage(m.lastRenewalErr),
		NextRetryAt:      m.nextRetryAt,
		LastTokenSource:  tokenSourceMemory,
	}
	if m.current.value != "" && now.Before(m.current.expiresAt) && m.lastErr == nil {
		state.Status = StatusAuthenticated
		return state
	}
	if m.lastErr != nil {
		state.Status = StatusUnhealthy
	}
	return state
}

func (m *Manager) ensureToken(ctx context.Context, forceLogin bool) (currentToken, error) {
	for {
		m.mu.Lock()
		now := m.clock.Now()
		if !forceLogin && m.current.value != "" && now.Before(m.refreshAtLocked()) {
			token := m.current
			m.mu.Unlock()
			return token, nil
		}
		if !forceLogin && m.retryBlockedLocked(now) {
			token, err := m.tokenDuringRetryBackoffLocked(now)
			m.mu.Unlock()
			return token, err
		}
		if m.refreshing {
			done := m.refreshDone
			m.mu.Unlock()
			select {
			case <-done:
				continue
			case <-ctx.Done():
				return currentToken{}, ctx.Err()
			}
		}

		action := m.refreshActionLocked(forceLogin, now)
		done := make(chan struct{})
		m.refreshing = true
		m.refreshDone = done
		m.mu.Unlock()

		result := m.performRefresh(ctx, action)

		m.mu.Lock()
		token, err := m.applyRefreshResultLocked(result)
		m.refreshing = false
		m.refreshDone = nil
		close(done)
		m.mu.Unlock()
		return token, err
	}
}

func (m *Manager) refreshAtLocked() time.Time {
	if m.current.expiresAt.IsZero() {
		return time.Time{}
	}
	return m.current.expiresAt.Add(-m.cfg.LoginBeforeTokenExpiry)
}

type refreshKind int

const (
	refreshKindLogin refreshKind = iota
	refreshKindRenew
)

type refreshAction struct {
	kind    refreshKind
	cfg     ManagerConfig
	current currentToken
	clock   Clock
}

type refreshResult struct {
	token      currentToken
	jwt        JWT
	loginAt    time.Time
	renewalAt  time.Time
	renewalErr error
	err        error
}

func (m *Manager) refreshActionLocked(forceLogin bool, now time.Time) refreshAction {
	action := refreshAction{
		kind:    refreshKindLogin,
		cfg:     m.cfg,
		current: m.current,
		clock:   m.clock,
	}
	if !forceLogin &&
		m.current.value != "" &&
		m.current.renewable &&
		m.renewalEnabled &&
		now.Before(m.current.expiresAt) {
		action.kind = refreshKindRenew
	}
	return action
}

func (m *Manager) performRefresh(ctx context.Context, action refreshAction) refreshResult {
	if action.kind == refreshKindRenew {
		token, renewedAt, err := m.renew(ctx, action)
		if err == nil {
			return refreshResult{token: token, renewalAt: renewedAt}
		}
		loginResult := m.login(ctx, action)
		loginResult.renewalErr = err
		return loginResult
	}
	return m.login(ctx, action)
}

func (m *Manager) login(ctx context.Context, action refreshAction) refreshResult {
	jwt, err := ReadAndValidateJWT(action.cfg.JWTFile, JWTValidationOptions{
		MinRemainingTTL: action.cfg.MinJWTRemainingTTL,
		ClockSkewLeeway: action.cfg.ClockSkewLeeway,
		Clock:           action.clock,
	})
	if err != nil {
		m.observeLogin(ctx, err)
		return refreshResult{err: err}
	}

	authToken, err := m.client.LoginJWT(ctx, openbao.JWTLoginRequest{
		MountPath: action.cfg.MountPath,
		Role:      action.cfg.Role,
		JWT:       jwt.Raw,
	})
	if err != nil {
		publicErr := publicAuthError(err)
		m.observeLogin(ctx, publicErr)
		return refreshResult{err: publicErr}
	}
	now := action.clock.Now()
	token, err := currentTokenFromAuth(authToken, "", now, true, action.cfg.LoginBeforeTokenExpiry)
	if err != nil {
		m.observeLogin(ctx, err)
		return refreshResult{err: err}
	}
	m.observeLogin(ctx, nil)

	return refreshResult{
		token:   token,
		jwt:     jwt,
		loginAt: now,
	}
}

func (m *Manager) renew(ctx context.Context, action refreshAction) (currentToken, time.Time, error) {
	authToken, err := m.client.RenewSelfToken(ctx, action.current.value, action.cfg.LoginBeforeTokenExpiry)
	if err != nil {
		publicErr := publicAuthError(err)
		m.observeRenewal(ctx, publicErr)
		return currentToken{}, time.Time{}, publicErr
	}
	now := action.clock.Now()
	token, err := currentTokenFromAuth(authToken, action.current.value, now, false, action.cfg.LoginBeforeTokenExpiry)
	if err != nil {
		m.observeRenewal(ctx, err)
		return currentToken{}, time.Time{}, err
	}
	m.observeRenewal(ctx, nil)

	return token, now, nil
}

func (m *Manager) applyRefreshResultLocked(result refreshResult) (currentToken, error) {
	now := m.clock.Now()
	if result.renewalErr != nil {
		m.lastRenewalErr = result.renewalErr
	}
	if result.err != nil {
		m.lastErr = result.err
		m.nextRetryAt = now.Add(m.retryBackoff)
		if m.current.value != "" && now.Before(m.current.expiresAt) {
			return m.current, nil
		}
		return currentToken{}, result.err
	}

	m.current = result.token
	if result.jwt.Raw != "" {
		m.lastJWT = result.jwt
	}
	if !result.loginAt.IsZero() {
		m.lastLoginAt = result.loginAt
	}
	if !result.renewalAt.IsZero() {
		m.lastRenewalAt = result.renewalAt
		m.lastRenewalErr = nil
	}
	m.lastErr = nil
	m.nextRetryAt = time.Time{}
	return result.token, nil
}

func currentTokenFromAuth(
	token openbao.AuthToken,
	fallbackValue string,
	now time.Time,
	requireValue bool,
	minUsableLease time.Duration,
) (currentToken, error) {
	value := token.ClientToken
	if value == "" {
		value = fallbackValue
	}
	if requireValue && value == "" {
		return currentToken{}, fmt.Errorf("%w: login response did not include a client token", ErrAuthFailed)
	}
	if token.LeaseDuration <= 0 {
		return currentToken{}, fmt.Errorf("%w: token lease duration must be positive", ErrAuthFailed)
	}
	if minUsableLease > 0 && token.LeaseDuration <= minUsableLease {
		return currentToken{}, fmt.Errorf("%w: token lease duration must exceed login-before-expiry", ErrAuthFailed)
	}
	return currentToken{
		value:     value,
		expiresAt: now.Add(token.LeaseDuration),
		renewable: token.Renewable,
	}, nil
}

func (m *Manager) retryBlockedLocked(now time.Time) bool {
	return !m.nextRetryAt.IsZero() && now.Before(m.nextRetryAt)
}

func (m *Manager) tokenDuringRetryBackoffLocked(now time.Time) (currentToken, error) {
	if m.current.value != "" && now.Before(m.current.expiresAt) {
		return m.current, nil
	}
	if m.lastErr != nil {
		return currentToken{}, m.lastErr
	}
	return currentToken{}, ErrTokenUnavailable
}

func validateManagerConfig(cfg ManagerConfig) (ManagerConfig, error) {
	cfg.MountPath = strings.TrimSpace(cfg.MountPath)
	cfg.Role = strings.TrimSpace(cfg.Role)
	cfg.JWTFile = strings.TrimSpace(cfg.JWTFile)
	if strings.TrimSpace(cfg.MountPath) == "" {
		return ManagerConfig{}, fmt.Errorf("%w: auth mount path is required", ErrAuthConfig)
	}
	if cfg.MountPath != path.Clean(cfg.MountPath) || strings.HasPrefix(cfg.MountPath, "/") || cfg.MountPath == "." {
		return ManagerConfig{}, fmt.Errorf("%w: auth mount path must be a relative OpenBao path", ErrAuthConfig)
	}
	if containsUnsafeIdentifierChars(cfg.MountPath) {
		return ManagerConfig{}, fmt.Errorf("%w: auth mount path contains unsafe characters", ErrAuthConfig)
	}
	if strings.TrimSpace(cfg.Role) == "" {
		return ManagerConfig{}, fmt.Errorf("%w: auth role is required", ErrAuthConfig)
	}
	if containsUnsafeIdentifierChars(cfg.Role) {
		return ManagerConfig{}, fmt.Errorf("%w: auth role contains unsafe characters", ErrAuthConfig)
	}
	if strings.TrimSpace(cfg.JWTFile) == "" {
		return ManagerConfig{}, fmt.Errorf("%w: JWT file is required", ErrAuthConfig)
	}
	if cfg.MinJWTRemainingTTL <= 0 {
		return ManagerConfig{}, fmt.Errorf("%w: minimum JWT remaining TTL must be positive", ErrAuthConfig)
	}
	if cfg.ClockSkewLeeway < 0 {
		return ManagerConfig{}, fmt.Errorf("%w: clock skew leeway must not be negative", ErrAuthConfig)
	}
	if cfg.LoginBeforeTokenExpiry <= 0 {
		return ManagerConfig{}, fmt.Errorf("%w: login-before-expiry duration must be positive", ErrAuthConfig)
	}
	return cfg, nil
}

func ttlUntil(now time.Time, expiry time.Time) time.Duration {
	if expiry.IsZero() {
		return 0
	}
	return expiry.Sub(now)
}

func publicAuthError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrAuthFailed, safeErrorMessage(err))
}

func (m *Manager) observeLogin(ctx context.Context, err error) {
	if m.observer == nil {
		return
	}
	m.observer.ObserveAuthLogin(ctx, authStatus(err))
}

func (m *Manager) observeRenewal(ctx context.Context, err error) {
	if m.observer == nil {
		return
	}
	m.observer.ObserveAuthRenewal(ctx, authStatus(err))
}

func authStatus(err error) string {
	if err == nil {
		return authStatusOK
	}
	switch {
	case errors.Is(err, ErrJWTExpired):
		return authStatusJWTExpired
	case errors.Is(err, ErrJWTNearExpiry):
		return authStatusJWTNearExpiry
	case errors.Is(err, ErrJWTMalformed),
		errors.Is(err, ErrJWTNotYetValid),
		errors.Is(err, ErrJWTIssuedInFuture),
		errors.Is(err, ErrJWTRead):
		return authStatusJWTInvalid
	case errors.Is(err, ErrAuthFailed),
		errors.Is(err, ErrTokenUnavailable):
		if errors.Is(err, ErrTokenUnavailable) {
			return authStatusNoUsableSession
		}
		return authStatusAuthFailed
	default:
		return authStatusError
	}
}

func safeErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	var openBaoErr *openbao.Error
	if errors.As(err, &openBaoErr) {
		return openBaoErr.Error()
	}
	for _, known := range safeAuthErrorClasses {
		if errors.Is(err, known) {
			return err.Error()
		}
	}
	return "auth operation failed"
}

func containsUnsafeIdentifierChars(value string) bool {
	return strings.TrimSpace(value) != value || strings.ContainsAny(value, "\x00\r\n\t")
}
