package status

import (
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/aad"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/kmsv2"
)

// RotationState is the bounded rotation state exposed to diagnostics.
type RotationState string

const (
	// RotationStateUnknown means no registry state is loaded.
	RotationStateUnknown RotationState = "unknown"
	// RotationStateActive means no pending rotation is observed.
	RotationStateActive RotationState = "active"
	// RotationStatePending means a newer Transit version is observed but not active.
	RotationStatePending RotationState = "pending"
)

// Diagnostics is a redacted local snapshot for readiness, metrics, and node comparison.
type Diagnostics struct {
	Healthz                       string
	UpdatedAt                     time.Time
	CacheAge                      time.Duration
	Stale                         bool
	ActiveKeyIDHash               string
	ActiveTransitVersion          int
	StateGeneration               uint64
	StateHash                     string
	RotationState                 RotationState
	PendingKeyIDHash              string
	PendingTransitVersion         int
	PendingStableObservationCount int
	PendingStableAt               time.Time
	CircuitBreaker                CircuitBreakerSnapshot
}

func diagnosticsForState(
	state keyregistry.StateFile,
	hasState bool,
	active keyregistry.KeySnapshot,
	healthz string,
	updatedAt time.Time,
	now time.Time,
	maxStaleness time.Duration,
	breaker CircuitBreakerSnapshot,
) Diagnostics {
	diagnostics := Diagnostics{
		Healthz:        normalizedHealth(healthz),
		UpdatedAt:      updatedAt,
		RotationState:  RotationStateUnknown,
		CircuitBreaker: normalizedCircuitBreakerSnapshot(breaker),
	}
	if !updatedAt.IsZero() {
		diagnostics.CacheAge = now.Sub(updatedAt)
	}
	if !hasState {
		diagnostics.Healthz = kmsv2.HealthUnhealthy
		diagnostics.Stale = true
		return diagnostics
	}

	diagnostics.StateGeneration = state.Generation
	diagnostics.StateHash = state.CurrentHash
	diagnostics.ActiveKeyIDHash = aad.HashValue(active.KubernetesKeyID)
	diagnostics.ActiveTransitVersion = active.TransitVersion
	diagnostics.RotationState = RotationStateActive
	diagnostics.Stale = updatedAt.IsZero() || now.Sub(updatedAt) > maxStaleness
	if diagnostics.Healthz == kmsv2.HealthOK && diagnostics.Stale {
		diagnostics.Healthz = kmsv2.HealthUnhealthy
	}

	pending, ok := pendingRecord(state)
	if ok {
		diagnostics.RotationState = RotationStatePending
		diagnostics.PendingKeyIDHash = aad.HashValue(pending.KubernetesKeyID)
		diagnostics.PendingTransitVersion = pending.TransitVersion
		diagnostics.PendingStableObservationCount = pending.StableObservationCount
		if pending.StableAtUnix != 0 {
			diagnostics.PendingStableAt = time.Unix(pending.StableAtUnix, 0).UTC()
		}
	}
	return diagnostics
}

func normalizedCircuitBreakerSnapshot(snapshot CircuitBreakerSnapshot) CircuitBreakerSnapshot {
	if snapshot.State == "" {
		snapshot.State = CircuitBreakerClosed
	}
	return snapshot
}

func normalizedHealth(healthz string) string {
	if healthz == "" {
		return kmsv2.HealthUnhealthy
	}
	return healthz
}

func pendingRecord(state keyregistry.StateFile) (keyregistry.SnapshotStateRecord, bool) {
	for _, record := range state.Snapshots {
		if keyregistry.SnapshotState(record.State) == keyregistry.StatePending {
			return record, true
		}
	}
	return keyregistry.SnapshotStateRecord{}, false
}
