package status

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/kmsv2"
)

// StoreOptions controls cache staleness and optional restart state.
type StoreOptions struct {
	Clock           Clock
	MaxStaleness    time.Duration
	InitialState    keyregistry.StateFile
	HasInitialState bool
}

// Store is the runtime bridge between background probes and KMS v2 request handlers.
type Store struct {
	mu           sync.RWMutex
	clock        Clock
	maxStaleness time.Duration
	healthz      string
	updatedAt    time.Time
	state        keyregistry.StateFile
	registry     keyregistry.Registry
	active       keyregistry.KeySnapshot
	hasState     bool
	breaker      CircuitBreakerSnapshot
}

// NewStore creates an initially unhealthy status cache.
func NewStore(opts StoreOptions) (*Store, error) {
	if opts.MaxStaleness <= 0 {
		return nil, fmt.Errorf("%w: max staleness must be positive", ErrConfigInvalid)
	}
	store := &Store{
		clock:        clockOrReal(opts.Clock),
		maxStaleness: opts.MaxStaleness,
		healthz:      kmsv2.HealthUnhealthy,
	}
	if opts.HasInitialState {
		if err := store.LoadState(opts.InitialState); err != nil {
			return nil, err
		}
	}
	return store, nil
}

// LoadState replaces the registry snapshot set without marking Status healthy.
func (s *Store) LoadState(state keyregistry.StateFile) error {
	active, registry, err := runtimeRegistry(state)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.state = state
	s.registry = registry
	s.active = active
	s.hasState = true
	if s.healthz == "" {
		s.healthz = kmsv2.HealthUnhealthy
	}
	return nil
}

// PublishHealthy atomically publishes a successful probe result.
func (s *Store) PublishHealthy(state keyregistry.StateFile, updatedAt time.Time) error {
	active, registry, err := runtimeRegistry(state)
	if err != nil {
		return err
	}
	if updatedAt.IsZero() {
		updatedAt = s.clock.Now()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.state = state
	s.registry = registry
	s.active = active
	s.hasState = true
	s.healthz = kmsv2.HealthOK
	s.updatedAt = updatedAt.UTC()
	return nil
}

// PublishUnhealthy records a failed probe while retaining the last registry snapshot set.
func (s *Store) PublishUnhealthy(updatedAt time.Time) {
	if updatedAt.IsZero() {
		updatedAt = s.clock.Now()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.healthz = kmsv2.HealthUnhealthy
	s.updatedAt = updatedAt.UTC()
}

// Current returns the cached KMS Status view without calling OpenBao.
func (s *Store) Current(ctx context.Context) (kmsv2.CachedStatus, error) {
	if err := contextErr(ctx); err != nil {
		return kmsv2.CachedStatus{}, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.hasState {
		return kmsv2.CachedStatus{Healthz: kmsv2.HealthUnhealthy}, nil
	}

	healthz := s.healthz
	healthz = normalizedHealth(healthz)
	if healthz == kmsv2.HealthOK && s.staleLocked(s.clock.Now()) {
		healthz = kmsv2.HealthUnhealthy
	}

	keyID := s.active.KubernetesKeyID
	if healthz != kmsv2.HealthOK {
		keyID = ""
	}
	return kmsv2.CachedStatus{
		Healthz: healthz,
		KeyID:   keyID,
		Active:  s.active,
	}, nil
}

// Lookup resolves decryptable active and historical key IDs before Transit decrypt.
func (s *Store) Lookup(keyID string) (keyregistry.KeySnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.hasState {
		if _, err := keyregistry.ParseKeyID(keyID); err != nil {
			return keyregistry.KeySnapshot{}, err
		}
		return keyregistry.KeySnapshot{}, keyregistry.ErrUnknownKeyID
	}
	return s.registry.Lookup(keyID)
}

// State returns the current persisted snapshot set.
func (s *Store) State() (keyregistry.StateFile, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.state, s.hasState
}

// Active returns the active snapshot when registry state is loaded.
func (s *Store) Active() (keyregistry.KeySnapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.active, s.hasState
}

// UpdateCircuitBreaker publishes the current circuit breaker state to diagnostics.
func (s *Store) UpdateCircuitBreaker(snapshot CircuitBreakerSnapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.breaker = snapshot
}

// Diagnostics returns a redacted local view for readiness, metrics, and node comparison.
func (s *Store) Diagnostics(ctx context.Context) (Diagnostics, error) {
	if err := contextErr(ctx); err != nil {
		return Diagnostics{}, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	return diagnosticsForState(
		s.state,
		s.hasState,
		s.active,
		s.healthz,
		s.updatedAt,
		s.clock.Now(),
		s.maxStaleness,
		s.breaker,
	), nil
}

func (s *Store) staleLocked(now time.Time) bool {
	if s.updatedAt.IsZero() {
		return true
	}
	return now.Sub(s.updatedAt) > s.maxStaleness
}

func runtimeRegistry(state keyregistry.StateFile) (keyregistry.KeySnapshot, keyregistry.Registry, error) {
	if err := state.Validate(); err != nil {
		return keyregistry.KeySnapshot{}, keyregistry.Registry{}, err
	}
	active, err := state.ActiveSnapshot()
	if err != nil {
		return keyregistry.KeySnapshot{}, keyregistry.Registry{}, err
	}

	historical := make([]keyregistry.KeySnapshot, 0, len(state.Snapshots)-1)
	for _, record := range state.Snapshots {
		snapshot, snapshotErr := record.Snapshot()
		if snapshotErr != nil {
			return keyregistry.KeySnapshot{}, keyregistry.Registry{}, snapshotErr
		}
		if snapshot.KubernetesKeyID == state.ActiveKeyID {
			continue
		}
		switch snapshot.State {
		case keyregistry.StateRetired, keyregistry.StateDisasterRecovery:
			historical = append(historical, snapshot)
		case keyregistry.StatePending, keyregistry.StateRejected:
		default:
			return keyregistry.KeySnapshot{}, keyregistry.Registry{}, fmt.Errorf(
				"snapshot state %q is not registry-decryptable",
				snapshot.State,
			)
		}
	}

	registry, err := keyregistry.NewRegistry(active, historical)
	if err != nil {
		return keyregistry.KeySnapshot{}, keyregistry.Registry{}, err
	}
	return active, registry, nil
}
