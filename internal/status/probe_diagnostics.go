package status

import (
	"context"
	"errors"
	"slices"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/aad"
	clocktime "github.com/dc-tec/openbao-kubernetes-kms/internal/clock"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
)

// HealthReason identifies a bounded readiness or probe failure condition.
type HealthReason string

const (
	ReasonStateUnavailable   HealthReason = "state_unavailable"
	ReasonStatusStale        HealthReason = "status_stale"
	ReasonMetadataUnverified HealthReason = "metadata_unverified"
	ReasonUpsertCheckFailed  HealthReason = "upsert_check_failed"
	ReasonUpsertAllowed      HealthReason = "upsert_allowed"
	ReasonMetadataReadFailed HealthReason = "metadata_read_failed"
	ReasonProfileInvalid     HealthReason = "profile_invalid"
	ReasonVersionRollback    HealthReason = "version_rollback"
	ReasonStateSaveFailed    HealthReason = "state_save_failed"
	ReasonStatePublishFailed HealthReason = "state_publish_failed"
	ReasonEncryptionBlocked  HealthReason = "encryption_blocked"
	ReasonDeepProbePending   HealthReason = "deep_probe_pending"
	ReasonDeepProbeFailed    HealthReason = "deep_probe_failed"
	ReasonDeepProbeInvalid   HealthReason = "deep_probe_invalid"
	ReasonCircuitBreakerOpen HealthReason = "circuit_breaker_open"
	ReasonProbeFailed        HealthReason = "probe_failed"
)

type probeFailure struct {
	reason     HealthReason
	errorClass string
}

type probeError struct {
	reason HealthReason
	cause  error
}

func (e *probeError) Error() string { return e.cause.Error() }
func (e *probeError) Unwrap() error { return e.cause }

func failureForProbe(err error) probeFailure {
	if err == nil {
		return probeFailure{}
	}
	reason := ReasonProbeFailed
	var failure *probeError
	switch {
	case errors.As(err, &failure):
		reason = failure.reason
	case errors.Is(err, ErrStateUnavailable):
		reason = ReasonStateUnavailable
	case errors.Is(err, ErrTransitKeyUnusable):
		reason = ReasonEncryptionBlocked
	case errors.Is(err, ErrVersionRollback):
		reason = ReasonVersionRollback
	}
	class := probeStatus(err)
	if class == probeStatusError {
		class = string(reason)
	}
	return probeFailure{reason: reason, errorClass: class}
}

func (c *Controller) metadataFailed(now clocktime.Reading, reason HealthReason, err error) error {
	failure := &probeError{reason: reason, cause: err}
	c.store.publishMetadataUnhealthy(now, failureForProbe(failure))
	return failure
}

func (c *Controller) deepFailed(reason HealthReason, err error) error {
	failure := &probeError{reason: reason, cause: err}
	c.store.publishDeepUnhealthy(failureForProbe(failure))
	return failure
}

func (s *Store) readinessReasonsLocked(stale bool) []HealthReason {
	reasons := make([]HealthReason, 0, 4)
	if !s.hasState {
		reasons = append(reasons, ReasonStateUnavailable)
	}
	if stale {
		reasons = append(reasons, ReasonStatusStale)
	}
	if !s.metadataOK {
		reason := s.metadataFailure.reason
		if reason == "" {
			reason = ReasonMetadataUnverified
		}
		if !slices.Contains(reasons, reason) {
			reasons = append(reasons, reason)
		}
	}
	if s.hasState && !s.deepProbeOK {
		reason := s.deepFailure.reason
		if reason == "" {
			reason = ReasonDeepProbePending
		}
		reasons = append(reasons, reason)
	}
	return reasons
}

// PromotionObservation describes an active-key change after state persistence and publication.
type PromotionObservation struct {
	PreviousKeyIDHash      string
	KeyIDHash              string
	PreviousTransitVersion int
	TransitVersion         int
}

func (c *Controller) observePromotion(ctx context.Context, previous keyregistry.KeySnapshot) {
	active, ok := c.store.Active()
	if !ok || previous.KubernetesKeyID == "" || previous.KubernetesKeyID == active.KubernetesKeyID ||
		c.probeObserver == nil {
		return
	}
	c.probeObserver.ObserveKeyPromotion(ctx, PromotionObservation{
		PreviousKeyIDHash:      aad.HashValue(previous.KubernetesKeyID),
		KeyIDHash:              aad.HashValue(active.KubernetesKeyID),
		PreviousTransitVersion: previous.TransitVersion,
		TransitVersion:         active.TransitVersion,
	})
}

func profileFailureReason(err error) HealthReason {
	if errors.Is(err, ErrVersionRollback) {
		return ReasonVersionRollback
	}
	return ReasonProfileInvalid
}
