package status

import (
	"context"
	"fmt"
	"sync"
	"time"

	clocktime "github.com/dc-tec/openbao-kubernetes-kms/internal/clock"
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
	mu                  sync.RWMutex
	clock               Clock
	maxStaleness        time.Duration
	healthz             string
	updatedAt           clocktime.Reading
	freshness           clocktime.Lifetime
	metadataOK          bool
	encryptionBlocked   bool
	deepProbeOK         bool
	deepProbed          bool
	metadataFailure     probeFailure
	deepFailure         probeFailure
	persistenceDegraded bool
	state               keyregistry.StateFile
	registry            keyregistry.Registry
	active              keyregistry.KeySnapshot
	hasState            bool
	breaker             CircuitBreakerSnapshot
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

// PublishHealthy atomically publishes state after all required checks succeed.
func (s *Store) PublishHealthy(state keyregistry.StateFile, updatedAt clocktime.Reading) error {
	active, registry, err := runtimeRegistry(state)
	if err != nil {
		return err
	}
	if updatedAt.Wall.IsZero() {
		updatedAt = s.clock.Read()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.state = state
	s.registry = registry
	s.active = active
	s.hasState = true
	s.metadataOK = true
	s.metadataFailure = probeFailure{}
	s.persistenceDegraded = false
	s.encryptionBlocked = false
	s.deepProbeOK = true
	s.deepProbed = true
	s.deepFailure = probeFailure{}
	s.updateHealthLocked()
	s.updatedAt = updatedAt
	s.freshness = clocktime.NewLifetime(updatedAt, s.maxStaleness)
	return nil
}

func (s *Store) publishMetadata(
	state keyregistry.StateFile, updatedAt clocktime.Reading, encryptionBlocked bool,
) error {
	active, registry, err := runtimeRegistry(state)
	if err != nil {
		return err
	}
	if updatedAt.Wall.IsZero() {
		updatedAt = s.clock.Read()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	activeChanged := !s.hasState || s.active.KubernetesKeyID != active.KubernetesKeyID
	s.state = state
	s.registry = registry
	s.active = active
	s.hasState = true
	s.metadataOK = !encryptionBlocked
	s.encryptionBlocked = encryptionBlocked
	s.metadataFailure = probeFailure{}
	s.persistenceDegraded = false
	if encryptionBlocked {
		s.metadataFailure = probeFailure{reason: ReasonEncryptionBlocked}
	}
	if activeChanged {
		s.deepFailure = probeFailure{}
		s.deepProbeOK = false
		s.deepProbed = false
	}
	s.updateHealthLocked()
	s.updatedAt = updatedAt
	s.freshness = clocktime.NewLifetime(updatedAt, s.maxStaleness)
	return nil
}

func (s *Store) publishMetadataUnhealthy(updatedAt clocktime.Reading, failure probeFailure) {
	if updatedAt.Wall.IsZero() {
		updatedAt = s.clock.Read()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.metadataOK = false
	s.metadataFailure = failure
	s.updateHealthLocked()
	s.updatedAt = updatedAt
	s.freshness = clocktime.NewLifetime(updatedAt, s.maxStaleness)
}

func (s *Store) publishDeferredObservation(expectedHash string, updatedAt clocktime.Reading) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.hasState || s.state.CurrentHash != expectedHash || !s.deepProbed || !s.deepProbeOK {
		return false
	}
	s.metadataOK = true
	s.metadataFailure = probeFailure{}
	s.encryptionBlocked = false
	s.persistenceDegraded = true
	s.updatedAt = updatedAt
	s.freshness = clocktime.NewLifetime(updatedAt, s.maxStaleness)
	s.updateHealthLocked()
	return true
}

func (s *Store) publishDeepHealthy() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.deepProbeOK = true
	s.deepProbed = true
	s.deepFailure = probeFailure{}
	s.updateHealthLocked()
}

func (s *Store) publishDeepUnhealthy(failure probeFailure) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.deepProbeOK = false
	s.deepProbed = true
	s.deepFailure = failure
	s.updateHealthLocked()
}

func (s *Store) deepProbeRequired() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.hasState && s.metadataOK && (!s.deepProbed || !s.deepProbeOK)
}

func (s *Store) activeForDeepProbe() (keyregistry.KeySnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.hasState {
		return keyregistry.KeySnapshot{}, ErrStateUnavailable
	}
	if s.encryptionBlocked {
		return keyregistry.KeySnapshot{}, ErrTransitKeyUnusable
	}
	return s.active, nil
}

// Current returns the cached KMS Status view without calling OpenBao.
func (s *Store) Current(ctx context.Context) (kmsv2.CachedStatus, error) {
	if err := contextErr(ctx); err != nil {
		return kmsv2.CachedStatus{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.hasState {
		return kmsv2.CachedStatus{Healthz: kmsv2.HealthUnhealthy}, nil
	}

	healthz := s.healthz
	healthz = normalizedHealth(healthz)
	if healthz == kmsv2.HealthOK && s.staleLocked(s.clock.Read()) {
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

	return s.DiagnosticsSnapshot(), nil
}

// DiagnosticsSnapshot returns a redacted local view without requiring a request context.
func (s *Store) DiagnosticsSnapshot() Diagnostics {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.clock.Read()
	stale := s.staleLocked(now)
	diagnostics := diagnosticsForState(
		s.state,
		s.hasState,
		s.active,
		s.healthz,
		s.updatedAt.Wall,
		s.freshness.Age(now),
		stale,
		s.breaker,
	)
	diagnostics.Reasons = s.readinessReasonsLocked(diagnostics.Stale)
	diagnostics.MetadataErrorClass = s.metadataFailure.errorClass
	diagnostics.DeepErrorClass = s.deepFailure.errorClass
	diagnostics.PersistenceDegraded = s.persistenceDegraded
	return diagnostics
}

func (s *Store) staleLocked(now clocktime.Reading) bool {
	if s.updatedAt.Wall.IsZero() {
		return true
	}
	return s.freshness.Remaining(now) < 0
}

func (s *Store) updateHealthLocked() {
	s.healthz = kmsv2.HealthUnhealthy
	if s.metadataOK && s.deepProbeOK {
		s.healthz = kmsv2.HealthOK
	}
}

func runtimeRegistry(state keyregistry.StateFile) (keyregistry.KeySnapshot, keyregistry.Registry, error) {
	if err := state.Validate(); err != nil {
		return keyregistry.KeySnapshot{}, keyregistry.Registry{}, err
	}
	active, err := state.ActiveSnapshot()
	if err != nil {
		return keyregistry.KeySnapshot{}, keyregistry.Registry{}, err
	}

	registry, err := state.Registry()
	if err != nil {
		return keyregistry.KeySnapshot{}, keyregistry.Registry{}, err
	}
	return active, registry, nil
}
