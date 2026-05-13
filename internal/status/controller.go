package status

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

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

	messageAuthRefreshFailed     = "auth refresh failed"
	messageContextRequired       = "context is required"
	messageCircuitBreakerOpen    = "probe skipped while circuit breaker is open"
	messageDeepProbeFailed       = "Transit deep probe failed"
	messageRegistryStateSave     = "save registry state"
	messageTransitMetadataFailed = "Transit metadata read failed"
)

// ProbeKind identifies the bounded background probe type.
type ProbeKind string

const (
	// ProbeKindMetadata refreshes auth, Transit metadata, and rotation state.
	ProbeKindMetadata ProbeKind = "metadata"
	// ProbeKindDeep performs a non-secret Transit encrypt/decrypt probe.
	ProbeKindDeep ProbeKind = "deep"
)

// AuthRefresher is the auth lifecycle surface needed by background probes.
type AuthRefresher interface {
	Refresh(context.Context) error
}

// TransitProbeClient is the Transit metadata and deep-probe surface needed by Status.
type TransitProbeClient interface {
	ReadKeyProfile(context.Context, string, string) (openbao.KeyProfile, error)
	ProbeEncryptDecrypt(context.Context, openbao.ProbeRequest) (openbao.ProbeResult, error)
}

// ProbeObservation is one redacted background status probe observation.
type ProbeObservation struct {
	Kind     ProbeKind
	Status   string
	Duration time.Duration
}

// ProbeObserver receives redacted background probe observations.
type ProbeObserver interface {
	ObserveStatusProbe(context.Context, ProbeObservation)
}

// ControllerOptions wires the status cache, rotation observer, and probe dependencies.
type ControllerOptions struct {
	Clock         Clock
	Store         *Store
	Observer      *Observer
	Auth          AuthRefresher
	Transit       TransitProbeClient
	StateStore    StateStore
	MountPath     string
	KeyName       string
	Breaker       CircuitBreakerOptions
	ProbeObserver ProbeObserver
}

// Controller runs one-shot status probes used by the scheduler and tests.
type Controller struct {
	clock         Clock
	store         *Store
	observer      *Observer
	auth          AuthRefresher
	transit       TransitProbeClient
	stateStore    StateStore
	mountPath     string
	keyName       string
	breakerMu     sync.Mutex
	breaker       circuitBreaker
	probeObserver ProbeObserver
}

// NewController builds a status probe controller and loads persisted registry state when available.
func NewController(opts ControllerOptions) (*Controller, error) {
	switch {
	case opts.Store == nil:
		return nil, fmt.Errorf("%w: status store is required", ErrConfigInvalid)
	case opts.Observer == nil:
		return nil, fmt.Errorf("%w: rotation observer is required", ErrConfigInvalid)
	case opts.Auth == nil:
		return nil, fmt.Errorf("%w: auth refresher is required", ErrConfigInvalid)
	case opts.Transit == nil:
		return nil, fmt.Errorf("%w: Transit probe client is required", ErrConfigInvalid)
	case opts.MountPath == "":
		return nil, fmt.Errorf("%w: Transit mount path is required", ErrConfigInvalid)
	case opts.KeyName == "":
		return nil, fmt.Errorf("%w: Transit key name is required", ErrConfigInvalid)
	}

	controller := &Controller{
		clock:         clockOrReal(opts.Clock),
		store:         opts.Store,
		observer:      opts.Observer,
		auth:          opts.Auth,
		transit:       opts.Transit,
		stateStore:    opts.StateStore,
		mountPath:     opts.MountPath,
		keyName:       opts.KeyName,
		breaker:       newCircuitBreaker(opts.Breaker),
		probeObserver: opts.ProbeObserver,
	}
	controller.store.UpdateCircuitBreaker(controller.breaker.snapshot())
	if opts.StateStore != nil {
		if err := controller.loadState(); err != nil {
			return nil, err
		}
	}
	return controller, nil
}

// ProbeOnce refreshes auth, reads Transit metadata, advances rotation state, and publishes cache health.
func (c *Controller) ProbeOnce(ctx context.Context) (err error) {
	start := time.Now()
	defer func() {
		c.observeProbe(ctx, ProbeObservation{
			Kind:     ProbeKindMetadata,
			Status:   probeStatus(err),
			Duration: time.Since(start),
		})
	}()

	if err := contextErr(ctx); err != nil {
		return err
	}
	now := c.clock.Now()
	if !c.allowProbe(now) {
		c.store.PublishUnhealthy(now)
		c.store.UpdateCircuitBreaker(c.breaker.snapshot())
		return fmt.Errorf("%w: %s", ErrCircuitBreakerOpen, messageCircuitBreakerOpen)
	}
	if err := c.auth.Refresh(ctx); err != nil {
		c.store.PublishUnhealthy(now)
		c.recordProbeFailure(now)
		return fmt.Errorf("%w: %s: %w", ErrProbeFailed, messageAuthRefreshFailed, err)
	}

	profile, err := c.transit.ReadKeyProfile(ctx, c.mountPath, c.keyName)
	if err != nil {
		c.store.PublishUnhealthy(now)
		c.recordProbeFailure(now)
		return fmt.Errorf("%w: %s: %w", ErrProbeFailed, messageTransitMetadataFailed, err)
	}

	state, hasState := c.store.State()
	var result ObservationResult
	if hasState {
		result, err = c.observer.Observe(state, profile, now)
	} else {
		if !CanAutoBootstrapState(profile) {
			c.store.PublishUnhealthy(now)
			return fmt.Errorf(
				"%w: local registry state is absent for non-initial Transit metadata",
				ErrStateUnavailable,
			)
		}
		rebuilt, rebuildErr := c.observer.RebuildState(profile, now)
		err = rebuildErr
		result = ObservationResult{State: rebuilt, Changed: true}
	}
	if err != nil {
		c.store.PublishUnhealthy(now)
		return err
	}

	if result.Changed && c.stateStore != nil {
		if err := c.stateStore.Save(result.State); err != nil {
			c.store.PublishUnhealthy(now)
			return fmt.Errorf("%w: %s: %w", ErrProbeFailed, messageRegistryStateSave, err)
		}
	}
	if err := c.store.PublishHealthy(result.State, now); err != nil {
		c.store.PublishUnhealthy(now)
		return err
	}
	c.recordProbeSuccess()
	return nil
}

// CanAutoBootstrapState reports whether absent local state may be synthesized from Transit metadata.
func CanAutoBootstrapState(profile openbao.KeyProfile) bool {
	return profile.LatestVersion == initialTransitVersion &&
		profile.MinAvailableVersion <= initialTransitVersion &&
		profile.MinDecryptionVersion <= initialTransitVersion
}

// DeepProbeOnce performs a non-secret Transit round trip for the active cached version.
func (c *Controller) DeepProbeOnce(ctx context.Context) (err error) {
	start := time.Now()
	defer func() {
		c.observeProbe(ctx, ProbeObservation{
			Kind:     ProbeKindDeep,
			Status:   probeStatus(err),
			Duration: time.Since(start),
		})
	}()

	if err := contextErr(ctx); err != nil {
		return err
	}
	now := c.clock.Now()
	if !c.allowProbe(now) {
		c.store.PublishUnhealthy(now)
		c.store.UpdateCircuitBreaker(c.breaker.snapshot())
		return fmt.Errorf("%w: %s", ErrCircuitBreakerOpen, messageCircuitBreakerOpen)
	}
	active, ok := c.store.Active()
	if !ok {
		c.store.PublishUnhealthy(now)
		return ErrStateUnavailable
	}
	result, err := c.transit.ProbeEncryptDecrypt(ctx, openbao.ProbeRequest{
		MountPath:      c.mountPath,
		KeyName:        c.keyName,
		KeyVersion:     active.TransitVersion,
		AssociatedData: []byte(probeAssociatedDataValue),
	})
	if err != nil {
		c.store.PublishUnhealthy(now)
		c.recordProbeFailure(now)
		return fmt.Errorf("%w: %s: %w", ErrProbeFailed, messageDeepProbeFailed, err)
	}
	if len(result.Ciphertext) >= kmsv2.MaxKMSCiphertextBytes {
		c.store.PublishUnhealthy(now)
		c.recordProbeFailure(now)
		return fmt.Errorf(
			"%w: %s: Transit ciphertext exceeds Kubernetes KMS v2 response limit",
			ErrProbeFailed,
			messageDeepProbeFailed,
		)
	}
	if result.KeyVersion != 0 && result.KeyVersion != active.TransitVersion {
		c.store.PublishUnhealthy(now)
		c.recordProbeFailure(now)
		return fmt.Errorf(
			"%w: %s: Transit returned unexpected key version",
			ErrProbeFailed,
			messageDeepProbeFailed,
		)
	}
	c.recordProbeSuccess()
	return nil
}

func (c *Controller) allowProbe(now time.Time) bool {
	c.breakerMu.Lock()
	defer c.breakerMu.Unlock()

	allowed := c.breaker.allow(now)
	c.store.UpdateCircuitBreaker(c.breaker.snapshot())
	return allowed
}

func (c *Controller) recordProbeFailure(now time.Time) {
	c.breakerMu.Lock()
	defer c.breakerMu.Unlock()

	c.breaker.recordFailure(now)
	c.store.UpdateCircuitBreaker(c.breaker.snapshot())
}

func (c *Controller) recordProbeSuccess() {
	c.breakerMu.Lock()
	defer c.breakerMu.Unlock()

	c.breaker.recordSuccess()
	c.store.UpdateCircuitBreaker(c.breaker.snapshot())
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
		return string(openBaoErr.Class)
	}
	return probeStatusError
}
