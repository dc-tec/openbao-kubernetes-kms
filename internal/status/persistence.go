package status

import "github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"

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
