package status

import (
	"fmt"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
)

// stateCommit survives failed saves until the exact candidate is durable and a
// metadata probe publishes it. Access is serialized by the controller probe gate.
type stateCommit struct {
	previous *keyregistry.StateFile
	next     keyregistry.StateFile
}

func (c *Controller) stateForObservation() (keyregistry.StateFile, bool, error) {
	if c.pendingCommit != nil {
		commit := c.pendingCommit
		if err := c.stateStore.Recover(commit.previous, commit.next); err != nil {
			return keyregistry.StateFile{}, false, err
		}
		return commit.next, true, nil
	}
	state, ok := c.store.State()
	return state, ok, nil
}

func (c *Controller) saveState(previous keyregistry.StateFile, hasPrevious bool, next keyregistry.StateFile) error {
	commit := &stateCommit{next: next}
	if hasPrevious {
		commit.previous = &previous
	}
	// Remember the exact bytes even when Save fails after replacing a file.
	c.pendingCommit = commit
	return c.stateStore.Save(next)
}

func (c *Controller) stateSaveFailed(now time.Time, profile openbao.KeyProfile, cause error) error {
	err := fmt.Errorf("%w: %s: %w", ErrProbeFailed, messageRegistryStateSave, cause)
	if c.deferObservation(now, profile) {
		// Keep reporting the failed save through probe logs and counters, even
		// when the independently validated published state remains usable.
		c.recordProbeSuccess(ProbeKindMetadata)
		return &probeError{reason: ReasonStateSaveFailed, cause: err}
	}
	return c.metadataFailed(now, ReasonStateSaveFailed, err)
}

func (c *Controller) deferObservation(now time.Time, profile openbao.KeyProfile) bool {
	commit := c.pendingCommit
	if commit == nil || commit.previous == nil || !observationOnly(*commit.previous, commit.next) {
		return false
	}
	// Discovery revalidates all retained keys and refuses an unseen newer key.
	// It cannot advance observations or promote while persistence is deferred.
	validated, err := c.observer.Discover(*commit.previous, profile, now)
	if err != nil || validated.Changed || validated.EncryptionBlocked {
		return false
	}
	if err := c.stateStore.Confirm(*commit.previous); err != nil {
		return false
	}
	return c.store.publishDeferredObservation(commit.previous.CurrentHash, now)
}

func observationOnly(previous, next keyregistry.StateFile) bool {
	if err := keyregistry.ValidateStateProgress(previous, next); err != nil {
		return false
	}
	if previous.ActiveKeyID != next.ActiveKeyID || len(previous.Snapshots) != len(next.Snapshots) {
		return false
	}
	changed := false
	for i, before := range previous.Snapshots {
		after := next.Snapshots[i]
		if before == after {
			continue
		}
		if before.State != string(keyregistry.StatePending) || after.State != before.State ||
			after.StableObservationCount < before.StableObservationCount ||
			(before.StableAtUnix != 0 && after.StableAtUnix != before.StableAtUnix) {
			return false
		}
		after.StableObservationCount = before.StableObservationCount
		after.StableAtUnix = before.StableAtUnix
		if after != before {
			return false
		}
		changed = true
	}
	return changed
}
