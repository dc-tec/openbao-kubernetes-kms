package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
)

const (
	tokenSourceMemory          = "memory"
	defaultRefreshRetryBackoff = time.Second
	defaultMaxRefreshBackoff   = time.Minute
	authStatusOK               = "ok"
	authStatusError            = "error"
	authStatusJWTExpired       = "jwt_expired"
	authStatusJWTNearExpiry    = "jwt_near_expiry"
	authStatusJWTInvalid       = "jwt_invalid"
	authStatusCertExpired      = "cert_expired"
	authStatusCertNearExpiry   = "cert_near_expiry"
	authStatusCertInvalid      = "cert_invalid"
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
	ErrJWTIssuerMismatch,
	ErrJWTAudienceMismatch,
	ErrJWTSubjectMismatch,
	ErrCertificateRead,
	ErrCertificateMalformed,
	ErrCertificateExpired,
	ErrCertificateNearExpiry,
	ErrCertificateNotYetValid,
	ErrCertificateWeakSignature,
	ErrCertificateUsage,
	ErrCertificateIdentityMismatch,
	ErrCertificateSignerMismatch,
	ErrCertificateSignerProbe,
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

// LifecycleConfig contains token lifecycle settings shared by auth methods.
type LifecycleConfig struct {
	LoginBeforeTokenExpiry time.Duration
	TokenRenewalIncrement  time.Duration
}

// ManagerConfig contains JWT auth settings after typed config loading.
type ManagerConfig struct {
	MountPath              string
	Role                   string
	JWTFile                string
	MinJWTRemainingTTL     time.Duration
	ClockSkewLeeway        time.Duration
	LoginBeforeTokenExpiry time.Duration
	TokenRenewalIncrement  time.Duration
	ExpectedIssuer         string
	ExpectedAudience       []string
	ExpectedSubject        string
}

// LoginResult contains one successful auth-method login.
type LoginResult struct {
	AuthToken openbao.AuthToken
	JWT       JWT
}

// LoginSource performs one auth-method-specific OpenBao login.
type LoginSource interface {
	Login(context.Context, OpenBaoAuthClient, Clock) (LoginResult, error)
}

// JWTLoginSource performs JWT file validation and OpenBao JWT login.
type JWTLoginSource struct {
	cfg ManagerConfig
}

// ManagerOptions contains testable lifecycle behavior settings.
type ManagerOptions struct {
	Clock                  Clock
	RenewalEnabled         bool
	RefreshRetryBackoff    time.Duration
	MaxRefreshRetryBackoff time.Duration
	RefreshRetryJitter     func(time.Duration) time.Duration
	Observer               Observer
}

// Observer receives redacted auth lifecycle observations.
type Observer interface {
	ObserveAuthLogin(context.Context, string)
	ObserveAuthRenewal(context.Context, string)
}

// State is the redacted auth state exposed to status and readiness code.
type State struct {
	Status              Status
	TokenRenewable      bool
	TokenExpiresAt      time.Time
	TokenTTL            time.Duration
	JWTExpiresAt        time.Time
	JWTTTL              time.Duration
	LastLoginAt         time.Time
	LastRenewalAt       time.Time
	LastError           string
	LastRenewalError    string
	NextRetryAt         time.Time
	ConsecutiveFailures int
	LastTokenSource     string
}

// Manager keeps the OpenBao token in memory and refreshes it through a login source.
type Manager struct {
	mu                  sync.Mutex
	cfg                 LifecycleConfig
	source              LoginSource
	client              OpenBaoAuthClient
	clock               Clock
	renewalEnabled      bool
	current             currentToken
	lastJWT             JWT
	lastLoginAt         time.Time
	lastRenewalAt       time.Time
	lastErr             error
	lastRenewalErr      error
	nextRetryAt         time.Time
	baseRetryBackoff    time.Duration
	maxRetryBackoff     time.Duration
	retryJitter         func(time.Duration) time.Duration
	consecutiveFailures int
	refreshing          bool
	refreshDone         chan struct{}
	observer            Observer
}

type currentToken struct {
	value     string
	expiresAt time.Time
	renewable bool
}

// NewManager creates an in-memory token manager.
func NewManager(cfg ManagerConfig, client OpenBaoAuthClient, opts ManagerOptions) (*Manager, error) {
	source, err := NewJWTLoginSource(cfg)
	if err != nil {
		return nil, err
	}
	return NewManagerWithSource(lifecycleConfigFromManager(cfg), source, client, opts)
}

// NewManagerWithSource creates an in-memory token manager for one login source.
func NewManagerWithSource(
	cfg LifecycleConfig,
	source LoginSource,
	client OpenBaoAuthClient,
	opts ManagerOptions,
) (*Manager, error) {
	if client == nil {
		return nil, fmt.Errorf("%w: OpenBao auth client is required", ErrAuthConfig)
	}
	if source == nil {
		return nil, fmt.Errorf("%w: login source is required", ErrAuthConfig)
	}
	normalized, err := validateLifecycleConfig(cfg)
	if err != nil {
		return nil, err
	}
	retryBackoff := opts.RefreshRetryBackoff
	if retryBackoff <= 0 {
		retryBackoff = defaultRefreshRetryBackoff
	}
	maxRetryBackoff := opts.MaxRefreshRetryBackoff
	if maxRetryBackoff <= 0 {
		maxRetryBackoff = defaultMaxRefreshBackoff
	}
	if maxRetryBackoff < retryBackoff {
		maxRetryBackoff = retryBackoff
	}
	retryJitter := opts.RefreshRetryJitter
	if retryJitter == nil {
		retryJitter = jitterRetryBackoff
	}

	return &Manager{
		cfg:              normalized,
		source:           source,
		client:           client,
		clock:            clockOrReal(opts.Clock),
		renewalEnabled:   opts.RenewalEnabled,
		baseRetryBackoff: retryBackoff,
		maxRetryBackoff:  maxRetryBackoff,
		retryJitter:      retryJitter,
		observer:         opts.Observer,
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

// Refresh forces an auth-method login.
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
		Status:              StatusUnknown,
		TokenRenewable:      m.current.renewable,
		TokenExpiresAt:      m.current.expiresAt,
		TokenTTL:            ttlUntil(now, m.current.expiresAt),
		JWTExpiresAt:        m.lastJWT.Claims.ExpiresAt,
		JWTTTL:              ttlUntil(now, m.lastJWT.Claims.ExpiresAt),
		LastLoginAt:         m.lastLoginAt,
		LastRenewalAt:       m.lastRenewalAt,
		LastError:           safeErrorMessage(m.lastErr),
		LastRenewalError:    safeErrorMessage(m.lastRenewalErr),
		NextRetryAt:         m.nextRetryAt,
		ConsecutiveFailures: m.consecutiveFailures,
		LastTokenSource:     tokenSourceMemory,
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
		token, err := m.applyRefreshResultLocked(result, forceLogin)
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
	cfg     LifecycleConfig
	source  LoginSource
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
		source:  m.source,
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
	login, err := action.source.Login(ctx, m.client, action.clock)
	if err != nil {
		m.observeLogin(ctx, err)
		return refreshResult{err: err}
	}

	now := action.clock.Now()
	token, err := currentTokenFromAuth(login.AuthToken, "", now, true, action.cfg.LoginBeforeTokenExpiry)
	if err != nil {
		m.observeLogin(ctx, err)
		return refreshResult{err: err}
	}
	m.observeLogin(ctx, nil)

	return refreshResult{
		token:   token,
		jwt:     login.JWT,
		loginAt: now,
	}
}

func (m *Manager) renew(ctx context.Context, action refreshAction) (currentToken, time.Time, error) {
	authToken, err := m.client.RenewSelfToken(ctx, action.current.value, action.cfg.TokenRenewalIncrement)
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

func (m *Manager) applyRefreshResultLocked(result refreshResult, forceLogin bool) (currentToken, error) {
	now := m.clock.Now()
	if result.renewalErr != nil {
		m.lastRenewalErr = result.renewalErr
	}
	if result.err != nil {
		m.lastErr = result.err
		m.consecutiveFailures++
		m.nextRetryAt = now.Add(m.nextRetryBackoffLocked())
		if forceLogin {
			return m.current, result.err
		}
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
	m.consecutiveFailures = 0
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
		// auth/token/renew-self normally omits client_token; retain the existing token.
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

func (m *Manager) nextRetryBackoffLocked() time.Duration {
	backoff := exponentialBackoff(m.baseRetryBackoff, m.maxRetryBackoff, m.consecutiveFailures)
	jittered := m.retryJitter(backoff)
	if jittered <= 0 {
		return backoff
	}
	if jittered > m.maxRetryBackoff {
		return m.maxRetryBackoff
	}
	return jittered
}

func exponentialBackoff(base time.Duration, max time.Duration, failures int) time.Duration {
	if failures <= 1 {
		return base
	}
	backoff := base
	for attempt := 1; attempt < failures; attempt++ {
		if backoff >= max || backoff > max/2 {
			return max
		}
		backoff *= 2
	}
	return backoff
}

func jitterRetryBackoff(backoff time.Duration) time.Duration {
	quarter := backoff / 4
	if quarter <= 0 {
		return backoff
	}
	width := int64(quarter*2 + 1)
	offset, err := rand.Int(rand.Reader, big.NewInt(width))
	if err != nil {
		return backoff
	}
	return backoff + time.Duration(offset.Int64()) - quarter
}

// NewJWTLoginSource creates a JWT login source with validated local settings.
func NewJWTLoginSource(cfg ManagerConfig) (*JWTLoginSource, error) {
	normalized, err := validateManagerConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &JWTLoginSource{cfg: normalized}, nil
}

// Login validates the current JWT file and exchanges it for an OpenBao token.
func (s *JWTLoginSource) Login(
	ctx context.Context,
	client OpenBaoAuthClient,
	clock Clock,
) (LoginResult, error) {
	jwt, err := ReadAndValidateJWT(s.cfg.JWTFile, JWTValidationOptions{
		MinRemainingTTL:  s.cfg.MinJWTRemainingTTL,
		ClockSkewLeeway:  s.cfg.ClockSkewLeeway,
		ExpectedIssuer:   s.cfg.ExpectedIssuer,
		ExpectedAudience: s.cfg.ExpectedAudience,
		ExpectedSubject:  s.cfg.ExpectedSubject,
		Clock:            clock,
	})
	if err != nil {
		return LoginResult{}, err
	}

	authToken, err := client.LoginJWT(ctx, openbao.JWTLoginRequest{
		MountPath: s.cfg.MountPath,
		Role:      s.cfg.Role,
		JWT:       jwt.Raw,
	})
	if err != nil {
		return LoginResult{}, publicAuthError(err)
	}
	return LoginResult{AuthToken: authToken, JWT: jwt}, nil
}

func validateManagerConfig(cfg ManagerConfig) (ManagerConfig, error) {
	cfg.MountPath = strings.TrimSpace(cfg.MountPath)
	cfg.Role = strings.TrimSpace(cfg.Role)
	cfg.JWTFile = strings.TrimSpace(cfg.JWTFile)
	cfg.ExpectedIssuer = strings.TrimSpace(cfg.ExpectedIssuer)
	cfg.ExpectedSubject = strings.TrimSpace(cfg.ExpectedSubject)
	expectedAudience, err := normalizeExpectedAudience(cfg.ExpectedAudience)
	if err != nil {
		return ManagerConfig{}, err
	}
	cfg.ExpectedAudience = expectedAudience
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
	lifecycle, err := validateLifecycleConfig(lifecycleConfigFromManager(cfg))
	if err != nil {
		return ManagerConfig{}, err
	}
	cfg.LoginBeforeTokenExpiry = lifecycle.LoginBeforeTokenExpiry
	cfg.TokenRenewalIncrement = lifecycle.TokenRenewalIncrement
	return cfg, nil
}

func lifecycleConfigFromManager(cfg ManagerConfig) LifecycleConfig {
	return LifecycleConfig{
		LoginBeforeTokenExpiry: cfg.LoginBeforeTokenExpiry,
		TokenRenewalIncrement:  cfg.TokenRenewalIncrement,
	}
}

func validateLifecycleConfig(cfg LifecycleConfig) (LifecycleConfig, error) {
	if cfg.LoginBeforeTokenExpiry <= 0 {
		return LifecycleConfig{}, fmt.Errorf("%w: login-before-expiry duration must be positive", ErrAuthConfig)
	}
	if cfg.TokenRenewalIncrement <= 0 {
		return LifecycleConfig{}, fmt.Errorf("%w: token renewal increment must be positive", ErrAuthConfig)
	}
	return cfg, nil
}

func normalizeExpectedAudience(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	expectedAudience := make([]string, 0, len(values))
	for _, audience := range values {
		trimmed := strings.TrimSpace(audience)
		if trimmed == "" {
			return nil, fmt.Errorf("%w: expected JWT audience must not be empty", ErrAuthConfig)
		}
		expectedAudience = append(expectedAudience, trimmed)
	}
	return expectedAudience, nil
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
		errors.Is(err, ErrJWTIssuerMismatch),
		errors.Is(err, ErrJWTAudienceMismatch),
		errors.Is(err, ErrJWTSubjectMismatch),
		errors.Is(err, ErrJWTRead):
		return authStatusJWTInvalid
	case errors.Is(err, ErrCertificateExpired):
		return authStatusCertExpired
	case errors.Is(err, ErrCertificateNearExpiry):
		return authStatusCertNearExpiry
	case errors.Is(err, ErrCertificateMalformed),
		errors.Is(err, ErrCertificateNotYetValid),
		errors.Is(err, ErrCertificateWeakSignature),
		errors.Is(err, ErrCertificateUsage),
		errors.Is(err, ErrCertificateIdentityMismatch),
		errors.Is(err, ErrCertificateSignerMismatch),
		errors.Is(err, ErrCertificateSignerProbe),
		errors.Is(err, ErrCertificateRead):
		return authStatusCertInvalid
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
	switch {
	case errors.Is(err, ErrJWTIssuerMismatch):
		return ErrJWTIssuerMismatch.Error()
	case errors.Is(err, ErrJWTAudienceMismatch):
		return ErrJWTAudienceMismatch.Error()
	case errors.Is(err, ErrJWTSubjectMismatch):
		return ErrJWTSubjectMismatch.Error()
	case errors.Is(err, ErrCertificateIdentityMismatch):
		return ErrCertificateIdentityMismatch.Error()
	case errors.Is(err, ErrCertificateSignerMismatch):
		return ErrCertificateSignerMismatch.Error()
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
