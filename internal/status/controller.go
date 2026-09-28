package status

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	clocktime "github.com/dc-tec/openbao-kubernetes-kms/internal/clock"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/kmsv2"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
)

const (
	probeStatusOK                 = "ok"
	probeStatusCanceled           = "canceled"
	probeStatusTimeout            = "timeout"
	probeStatusCircuitBreakerOpen = "circuit_breaker_open"
	probeStatusError              = "error"

	probeAssociatedDataValue = "openbao-kubernetes-kms/status-probe/v1"

	messageContextRequired       = "context is required"
	messageCircuitBreakerOpen    = "probe skipped while circuit breaker is open"
	messageDeepProbeFailed       = "Transit deep probe failed"
	messageRegistryStateSave     = "save registry state"
	messageTransitUpsertAllowed  = "Transit disable_upsert is false"
	messageTransitUpsertRead     = "Transit disable_upsert read failed"
	messageTransitMetadataFailed = "Transit metadata read failed"
)

// ProbeKind identifies the bounded background probe type.
type ProbeKind string

const (
	// ProbeKindMetadata reads Transit metadata and advances rotation state.
	ProbeKindMetadata ProbeKind = "metadata"
	// ProbeKindDeep performs a non-secret Transit encrypt/decrypt probe.
	ProbeKindDeep ProbeKind = "deep"
)

// TransitProbeClient is the Transit metadata and deep-probe surface needed by Status.
type TransitProbeClient interface {
	ReadDisableUpsert(context.Context, string) (bool, error)
	ReadKeyProfile(context.Context, string, string) (openbao.KeyProfile, error)
	ProbeEncryptDecrypt(context.Context, openbao.ProbeRequest) (openbao.ProbeResult, error)
}

// ProbeObservation is one redacted background status probe observation.
type ProbeObservation struct {
	Kind       ProbeKind
	Status     string
	Duration   time.Duration
	Reason     HealthReason
	ErrorClass string
}

// ProbeObserver receives redacted background probe observations.
type ProbeObserver interface {
	ObserveStatusProbe(context.Context, ProbeObservation)
	ObserveKeyPromotion(context.Context, PromotionObservation)
}

// ControllerOptions wires the status cache, rotation observer, and probe dependencies.
type ControllerOptions struct {
	Clock         Clock
	Store         *Store
	Observer      *Observer
	Transit       TransitProbeClient
	StateStore    StateStore
	MountPath     string
	KeyName       string
	Breaker       CircuitBreakerOptions
	ProbeObserver ProbeObserver
	// DecryptRefreshInterval bounds metadata attempts caused by unknown key IDs.
	DecryptRefreshInterval time.Duration
	// LifecycleContext owns discovery work independently of individual KMS calls.
	LifecycleContext      context.Context
	DecryptRefreshTimeout time.Duration
}

// Controller runs one-shot status probes used by the scheduler and tests.
type Controller struct {
	clock           Clock
	store           *Store
	observer        *Observer
	transit         TransitProbeClient
	stateStore      StateStore
	mountPath       string
	keyName         string
	breakerMu       sync.Mutex
	metadataBreaker circuitBreaker
	deepBreaker     circuitBreaker
	probeObserver   ProbeObserver
	probeGate       chan struct{}
	refreshInterval time.Duration
	lifecycle       context.Context
	refreshTimeout  time.Duration
	nextRefresh     clocktime.Deadline
	refreshErr      error
	pendingCommit   *stateCommit
	activation      activationWait
}

// NewController builds a status probe controller and loads persisted registry state when available.
func NewController(opts ControllerOptions) (*Controller, error) {
	switch {
	case opts.LifecycleContext == nil:
		return nil, fmt.Errorf("%w: lifecycle context is required", ErrConfigInvalid)
	case opts.DecryptRefreshTimeout < 0:
		return nil, fmt.Errorf("%w: decrypt refresh timeout must not be negative", ErrConfigInvalid)
	case opts.Store == nil:
		return nil, fmt.Errorf("%w: status store is required", ErrConfigInvalid)
	case opts.Observer == nil:
		return nil, fmt.Errorf("%w: rotation observer is required", ErrConfigInvalid)
	case opts.Transit == nil:
		return nil, fmt.Errorf("%w: Transit probe client is required", ErrConfigInvalid)
	case opts.MountPath == "":
		return nil, fmt.Errorf("%w: Transit mount path is required", ErrConfigInvalid)
	case opts.KeyName == "":
		return nil, fmt.Errorf("%w: Transit key name is required", ErrConfigInvalid)
	case opts.DecryptRefreshInterval < 0:
		return nil, fmt.Errorf("%w: decrypt refresh interval must not be negative", ErrConfigInvalid)
	}
	if opts.DecryptRefreshInterval == 0 {
		opts.DecryptRefreshInterval = 30 * time.Second
	}
	if opts.DecryptRefreshTimeout == 0 {
		opts.DecryptRefreshTimeout = 5 * time.Second
	}

	controller := &Controller{
		clock:           clockOrReal(opts.Clock),
		store:           opts.Store,
		observer:        opts.Observer,
		transit:         opts.Transit,
		stateStore:      opts.StateStore,
		mountPath:       opts.MountPath,
		keyName:         opts.KeyName,
		metadataBreaker: newCircuitBreaker(opts.Breaker),
		deepBreaker:     newCircuitBreaker(opts.Breaker),
		probeObserver:   opts.ProbeObserver,
		probeGate:       make(chan struct{}, 1),
		refreshInterval: opts.DecryptRefreshInterval,
		lifecycle:       opts.LifecycleContext,
		refreshTimeout:  opts.DecryptRefreshTimeout,
	}
	controller.publishCircuitBreakerState()
	if opts.StateStore != nil {
		if err := controller.loadState(); err != nil {
			return nil, err
		}
	}
	return controller, nil
}

// ProbeOnce reads Transit metadata, advances rotation state, and publishes metadata health.
func (c *Controller) ProbeOnce(ctx context.Context) error {
	if err := c.acquireProbe(ctx); err != nil {
		return err
	}
	defer c.releaseProbe()
	return c.probeOnce(ctx, false)
}

// RefreshForDecrypt discovers validated keys on an unknown key_id. Concurrent
// requests share the next eligible attempt. Discovery never advances promotion.
func (c *Controller) RefreshForDecrypt(ctx context.Context, keyID string) error {
	if _, err := keyregistry.ParseKeyID(keyID); err != nil {
		return err
	}
	if err := c.acquireProbe(ctx); err != nil {
		return err
	}
	start, err := c.prepareDecryptRefresh(keyID)
	if !start {
		c.releaseProbe()
		return err
	}
	done := make(chan error, 1)
	go func() {
		probeCtx, cancel := context.WithTimeout(c.lifecycle, c.refreshTimeout)
		defer cancel()
		err := c.probeOnce(probeCtx, true)
		c.refreshErr = err
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			c.refreshErr = fmt.Errorf("%w: metadata discovery interrupted", ErrProbeFailed)
		}
		c.releaseProbe()
		if _, lookupErr := c.store.Lookup(keyID); lookupErr == nil {
			// Encryption can remain blocked while a validated pending key decrypts.
			err = nil
		}
		done <- err
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.lifecycle.Done():
		return c.lifecycle.Err()
	case err := <-done:
		return err
	}
}

// prepareDecryptRefresh runs under probeGate. The gate transfers to the shared
// attempt only when this method returns true.
func (c *Controller) prepareDecryptRefresh(keyID string) (bool, error) {
	if _, err := c.store.Lookup(keyID); err == nil {
		return false, nil
	}
	state, ok := c.store.State()
	if !ok {
		return false, ErrStateUnavailable
	}
	for _, record := range state.Snapshots {
		if record.KubernetesKeyID == keyID {
			// Removed and rejected identities cannot be rediscovered.
			return false, nil
		}
	}
	now := c.clock.Read()
	if c.nextRefresh.Pending(now) {
		return false, c.refreshErr
	}
	c.nextRefresh = clocktime.After(now, c.refreshInterval)
	return true, nil
}

func (c *Controller) acquireProbe(ctx context.Context) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	select {
	case c.probeGate <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Controller) releaseProbe() {
	<-c.probeGate
}

func (c *Controller) probeOnce(ctx context.Context, discover bool) (err error) {
	start := time.Now()
	defer func() {
		failure := failureForProbe(err)
		c.observeProbe(ctx, ProbeObservation{
			Kind:       ProbeKindMetadata,
			Status:     probeStatus(err),
			Duration:   time.Since(start),
			Reason:     failure.reason,
			ErrorClass: failure.errorClass,
		})
	}()

	if err := contextErr(ctx); err != nil {
		return err
	}
	now := c.clock.Read()
	if !c.allowProbe(ProbeKindMetadata, now) {
		return c.metadataFailed(now, ReasonCircuitBreakerOpen,
			fmt.Errorf("%w: %s", ErrCircuitBreakerOpen, messageCircuitBreakerOpen))
	}
	disableUpsert, err := c.transit.ReadDisableUpsert(ctx, c.mountPath)
	if err != nil {
		c.recordProbeFailure(ProbeKindMetadata, now)
		return c.metadataFailed(now, ReasonUpsertCheckFailed,
			fmt.Errorf("%w: %s: %w", ErrProbeFailed, messageTransitUpsertRead, err))
	}
	if !disableUpsert {
		return c.metadataFailed(now, ReasonUpsertAllowed,
			fmt.Errorf("%w: %s", ErrProbeFailed, messageTransitUpsertAllowed))
	}

	profile, err := c.transit.ReadKeyProfile(ctx, c.mountPath, c.keyName)
	if err != nil {
		c.recordProbeFailure(ProbeKindMetadata, now)
		return c.metadataFailed(now, ReasonMetadataReadFailed,
			fmt.Errorf("%w: %s: %w", ErrProbeFailed, messageTransitMetadataFailed, err))
	}
	return c.publishObservation(ctx, profile, now, discover)
}

func (c *Controller) publishObservation(
	ctx context.Context, profile openbao.KeyProfile, now clocktime.Reading, discover bool,
) error {
	state, hasState, err := c.stateForObservation()
	if err != nil {
		return c.stateSaveFailed(now, profile, err)
	}
	previous, _ := c.store.Active()
	var result ObservationResult
	if hasState {
		if discover {
			result, err = c.observer.Discover(state, profile, now.Wall)
		} else {
			result, err = c.observer.Observe(state, profile, now.Wall, c.promotionReady(state, profile, now))
		}
	} else {
		assessment := AssessAutoBootstrapState(profile)
		if !assessment.Allowed {
			return c.metadataFailed(now, ReasonStateUnavailable, fmt.Errorf(
				"%w: local registry state is absent and cannot be auto-bootstrapped: %s",
				ErrStateUnavailable,
				assessment.Reason,
			))
		}
		rebuilt, rebuildErr := c.observer.RebuildState(profile, now.Wall)
		err = rebuildErr
		result = ObservationResult{State: rebuilt, Changed: true}
	}
	if err != nil {
		return c.metadataFailed(now, profileFailureReason(err), err)
	}

	if result.Changed && c.stateStore != nil {
		if err := c.saveState(state, hasState, result.State); err != nil {
			return c.stateSaveFailed(now, profile, err)
		}
	}
	if err := c.store.publishMetadata(result.State, now, result.EncryptionBlocked); err != nil {
		return c.metadataFailed(now, ReasonStatePublishFailed, err)
	}
	c.pendingCommit = nil
	if !discover {
		c.confirmActivation(result.State, profile)
	}
	if result.ClockRegressed {
		c.observeClockRegression(ctx)
	}
	c.observePromotion(ctx, previous)
	c.recordProbeSuccess(ProbeKindMetadata)
	if result.EncryptionBlocked {
		return fmt.Errorf("%w: active Transit version cannot encrypt", ErrTransitKeyUnusable)
	}
	return nil
}

// DeepProbeOnce performs a non-secret Transit round trip for the active cached version.
func (c *Controller) DeepProbeOnce(ctx context.Context) (err error) {
	if err := c.acquireProbe(ctx); err != nil {
		return err
	}
	defer c.releaseProbe()
	start := time.Now()
	defer func() {
		failure := failureForProbe(err)
		c.observeProbe(ctx, ProbeObservation{
			Kind:       ProbeKindDeep,
			Status:     probeStatus(err),
			Duration:   time.Since(start),
			Reason:     failure.reason,
			ErrorClass: failure.errorClass,
		})
	}()

	if err := contextErr(ctx); err != nil {
		return err
	}
	now := c.clock.Read()
	if !c.allowProbe(ProbeKindDeep, now) {
		return c.deepFailed(ReasonCircuitBreakerOpen,
			fmt.Errorf("%w: %s", ErrCircuitBreakerOpen, messageCircuitBreakerOpen))
	}
	active, err := c.store.activeForDeepProbe()
	if err != nil {
		// A known encryption restriction is not a backend failure. Avoid
		// opening the breaker for the old key while promotion advances.
		return err
	}
	result, err := c.transit.ProbeEncryptDecrypt(ctx, openbao.ProbeRequest{
		MountPath:      c.mountPath,
		KeyName:        c.keyName,
		KeyVersion:     active.TransitVersion,
		AssociatedData: []byte(probeAssociatedDataValue),
	})
	if err != nil {
		c.recordProbeFailure(ProbeKindDeep, now)
		return c.deepFailed(ReasonDeepProbeFailed,
			fmt.Errorf("%w: %s: %w", ErrProbeFailed, messageDeepProbeFailed, err))
	}
	if len(result.Ciphertext) >= kmsv2.MaxKMSCiphertextBytes {
		c.recordProbeFailure(ProbeKindDeep, now)
		return c.deepFailed(ReasonDeepProbeInvalid, fmt.Errorf(
			"%w: %s: Transit ciphertext exceeds Kubernetes KMS v2 response limit",
			ErrProbeFailed,
			messageDeepProbeFailed,
		))
	}
	if result.KeyVersion != 0 && result.KeyVersion != active.TransitVersion {
		c.recordProbeFailure(ProbeKindDeep, now)
		return c.deepFailed(ReasonDeepProbeInvalid, fmt.Errorf(
			"%w: %s: Transit returned unexpected key version",
			ErrProbeFailed,
			messageDeepProbeFailed,
		))
	}
	c.store.publishDeepHealthy()
	c.recordProbeSuccess(ProbeKindDeep)
	return nil
}

func (c *Controller) allowProbe(kind ProbeKind, now clocktime.Reading) bool {
	c.breakerMu.Lock()
	allowed := c.breakerForKind(kind).allow(now)
	reading := c.clock.Read()
	snapshot := aggregateCircuitBreakerSnapshots(
		c.metadataBreaker.snapshot(reading), c.deepBreaker.snapshot(reading),
	)
	c.breakerMu.Unlock()

	c.store.UpdateCircuitBreaker(snapshot)
	return allowed
}

func (c *Controller) recordProbeFailure(kind ProbeKind, now clocktime.Reading) {
	c.breakerMu.Lock()
	c.breakerForKind(kind).recordFailure(now)
	reading := c.clock.Read()
	snapshot := aggregateCircuitBreakerSnapshots(
		c.metadataBreaker.snapshot(reading), c.deepBreaker.snapshot(reading),
	)
	c.breakerMu.Unlock()

	c.store.UpdateCircuitBreaker(snapshot)
}

func (c *Controller) recordProbeSuccess(kind ProbeKind) {
	c.breakerMu.Lock()
	c.breakerForKind(kind).recordSuccess()
	reading := c.clock.Read()
	snapshot := aggregateCircuitBreakerSnapshots(
		c.metadataBreaker.snapshot(reading), c.deepBreaker.snapshot(reading),
	)
	c.breakerMu.Unlock()

	c.store.UpdateCircuitBreaker(snapshot)
}

func (c *Controller) breakerForKind(kind ProbeKind) *circuitBreaker {
	if kind == ProbeKindDeep {
		return &c.deepBreaker
	}
	return &c.metadataBreaker
}

func (c *Controller) publishCircuitBreakerState() {
	c.store.UpdateCircuitBreaker(aggregateCircuitBreakerSnapshots(
		c.metadataBreaker.snapshot(c.clock.Read()),
		c.deepBreaker.snapshot(c.clock.Read()),
	))
}

func (c *Controller) deepProbeRequired() bool {
	return c.store.deepProbeRequired()
}

func (c *Controller) observeProbe(ctx context.Context, observation ProbeObservation) {
	if c.probeObserver == nil {
		return
	}
	c.probeObserver.ObserveStatusProbe(ctx, observation)
}

func (c *Controller) loadState() error {
	state, err := c.stateStore.Load()
	if errors.Is(err, keyregistry.ErrStateNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := c.observer.validateStateScope(state); err != nil {
		return err
	}
	return c.store.LoadState(state)
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("%w: %s", ErrConfigInvalid, messageContextRequired)
	}
	return ctx.Err()
}

func probeStatus(err error) string {
	if err == nil {
		return probeStatusOK
	}
	switch {
	case errors.Is(err, context.Canceled):
		return probeStatusCanceled
	case errors.Is(err, context.DeadlineExceeded):
		return probeStatusTimeout
	case errors.Is(err, ErrCircuitBreakerOpen):
		return probeStatusCircuitBreakerOpen
	}
	var openBaoErr *openbao.Error
	if errors.As(err, &openBaoErr) {
		if errors.Is(err, openbao.ErrAuthentication) {
			switch openBaoErr.Class {
			case openbao.ErrorClassUnauthenticated, openbao.ErrorClassPermissionDenied, openbao.ErrorClassInvalidRequest:
				return "auth_failed"
			}
		}
		return string(openBaoErr.Class)
	}
	if errors.Is(err, openbao.ErrAuthentication) {
		return "auth_failed"
	}
	return probeStatusError
}
