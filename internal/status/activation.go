package status

import (
	clocktime "github.com/dc-tec/openbao-kubernetes-kms/internal/clock"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
)

// activationWait belongs to this controller process. Only a validated, durable
// stable observation can start it; persisted wall timestamps never satisfy it.
type activationWait struct {
	keyID    string
	deadline clocktime.Deadline
}

func (c *Controller) promotionReady(
	state keyregistry.StateFile, profile openbao.KeyProfile, now clocktime.Reading,
) bool {
	if c.observer.policy.ActivationDelay == 0 {
		return true
	}
	pending, ok := c.stableCandidate(state, profile)
	return ok && c.activation.keyID == pending.KubernetesKeyID && !c.activation.deadline.Pending(now)
}

func (c *Controller) confirmActivation(state keyregistry.StateFile, profile openbao.KeyProfile) {
	pending, ok := c.stableCandidate(state, profile)
	if !ok {
		c.activation = activationWait{}
		return
	}
	if c.activation.keyID != pending.KubernetesKeyID {
		c.activation = activationWait{
			keyID:    pending.KubernetesKeyID,
			deadline: clocktime.After(c.clock.Read(), c.observer.policy.ActivationDelay),
		}
	}
}

func (c *Controller) stableCandidate(
	state keyregistry.StateFile, profile openbao.KeyProfile,
) (keyregistry.SnapshotStateRecord, bool) {
	pending, ok := pendingRecord(state)
	return pending, ok && pending.TransitVersion == profile.LatestVersion &&
		pending.StableAtUnix != 0 && pending.StableObservationCount >= c.observer.policy.RequireStableObservationCount
}
