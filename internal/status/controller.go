package status

import (
	"context"
	"errors"
	"fmt"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
)

const (
	probeAssociatedDataValue = "openbao-kubernetes-kms/status-probe/v1"

	messageAuthRefreshFailed     = "auth refresh failed"
	messageContextRequired       = "context is required"
	messageDeepProbeFailed       = "Transit deep probe failed"
	messageRegistryStateSave     = "save registry state"
	messageTransitMetadataFailed = "Transit metadata read failed"
)

// AuthRefresher is the auth lifecycle surface needed by background probes.
type AuthRefresher interface {
	Refresh(context.Context) error
}

// TransitProbeClient is the Transit metadata and deep-probe surface needed by Status.
type TransitProbeClient interface {
	ReadKeyProfile(context.Context, string, string) (openbao.KeyProfile, error)
	ProbeEncryptDecrypt(context.Context, openbao.ProbeRequest) error
}

// ControllerOptions wires the status cache, rotation observer, and probe dependencies.
type ControllerOptions struct {
	Clock      Clock
	Store      *Store
	Observer   *Observer
	Auth       AuthRefresher
	Transit    TransitProbeClient
	StateStore StateStore
	MountPath  string
	KeyName    string
}

// Controller runs one-shot status probes used by the scheduler and tests.
type Controller struct {
	clock      Clock
	store      *Store
	observer   *Observer
	auth       AuthRefresher
	transit    TransitProbeClient
	stateStore StateStore
	mountPath  string
	keyName    string
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
		clock:      clockOrReal(opts.Clock),
		store:      opts.Store,
		observer:   opts.Observer,
		auth:       opts.Auth,
		transit:    opts.Transit,
		stateStore: opts.StateStore,
		mountPath:  opts.MountPath,
		keyName:    opts.KeyName,
	}
	if opts.StateStore != nil {
		if err := controller.loadState(); err != nil {
			return nil, err
		}
	}
	return controller, nil
}

// ProbeOnce refreshes auth, reads Transit metadata, advances rotation state, and publishes cache health.
func (c *Controller) ProbeOnce(ctx context.Context) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	now := c.clock.Now()
	if err := c.auth.Refresh(ctx); err != nil {
		c.store.PublishUnhealthy(now)
		return fmt.Errorf("%w: %s: %w", ErrProbeFailed, messageAuthRefreshFailed, err)
	}

	profile, err := c.transit.ReadKeyProfile(ctx, c.mountPath, c.keyName)
	if err != nil {
		c.store.PublishUnhealthy(now)
		return fmt.Errorf("%w: %s: %w", ErrProbeFailed, messageTransitMetadataFailed, err)
	}

	state, hasState := c.store.State()
	var result ObservationResult
	if hasState {
		result, err = c.observer.Observe(state, profile, now)
	} else {
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
	return nil
}

// DeepProbeOnce performs a non-secret Transit round trip for the active cached version.
func (c *Controller) DeepProbeOnce(ctx context.Context) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	now := c.clock.Now()
	active, ok := c.store.Active()
	if !ok {
		c.store.PublishUnhealthy(now)
		return ErrStateUnavailable
	}
	if err := c.transit.ProbeEncryptDecrypt(ctx, openbao.ProbeRequest{
		MountPath:      c.mountPath,
		KeyName:        c.keyName,
		KeyVersion:     active.TransitVersion,
		AssociatedData: []byte(probeAssociatedDataValue),
	}); err != nil {
		c.store.PublishUnhealthy(now)
		return fmt.Errorf("%w: %s: %w", ErrProbeFailed, messageDeepProbeFailed, err)
	}
	return nil
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
