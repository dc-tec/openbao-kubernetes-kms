package status

import (
	"errors"
	"fmt"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
)

// Recover reconciles a failed two-file save without accepting unrelated disk
// state. The caller must retain the state writer lock throughout the attempt and
// recovery. Rewriting the exact candidate repeats the durability barriers even
// when a previous rename succeeded but its directory sync failed.
func (s FileStateStore) Recover(previous *keyregistry.StateFile, attempted keyregistry.StateFile) error {
	if err := validateRecoveryTransition(previous, attempted); err != nil {
		return err
	}
	if err := s.checkRecoveryFiles(previous, attempted); err != nil {
		return err
	}
	return s.Save(attempted)
}

func validateRecoveryTransition(previous *keyregistry.StateFile, attempted keyregistry.StateFile) error {
	if err := attempted.Validate(); err != nil {
		return err
	}
	if previous != nil {
		return keyregistry.ValidateStateProgress(*previous, attempted)
	}
	if attempted.Generation != 1 || attempted.PreviousHash != "" {
		return fmt.Errorf("%w: recovery bootstrap must start at generation one", keyregistry.ErrStateRollback)
	}
	return nil
}

func (s FileStateStore) checkRecoveryFiles(previous *keyregistry.StateFile, attempted keyregistry.StateFile) error {
	if s.Path == "" {
		return fmt.Errorf("%w: %s", ErrConfigInvalid, messageStatePathRequired)
	}
	state, _, stateErr := keyregistry.LoadStateFile(s.Path, keyregistry.StateLoadOptions{})
	checkpoint, checkpointErr := keyregistry.LoadStateCheckpoint(keyregistry.StateCheckpointPath(s.Path))
	if err := recoveryReadError(stateErr, previous == nil); err != nil {
		return err
	}
	if err := recoveryReadError(checkpointErr, previous == nil); err != nil {
		return err
	}
	if stateErr == nil && !recoveryStateMatches(state, previous, attempted) {
		return fmt.Errorf("%w: state differs from the confirmed and attempted states", keyregistry.ErrStateRollback)
	}
	if checkpointErr != nil {
		return nil // Only a bootstrap may have no checkpoint.
	}
	if stateErr != nil {
		return fmt.Errorf("%w: state file missing with checkpoint present", keyregistry.ErrStateRollback)
	}
	if err := checkpoint.ValidateState(state); err != nil {
		return err
	}
	if checkpoint.CurrentHash == attempted.CurrentHash && checkpoint.Generation == attempted.Generation {
		return nil
	}
	if previous != nil && checkpoint.CurrentHash == previous.CurrentHash && checkpoint.Generation == previous.Generation {
		return nil
	}
	return fmt.Errorf("%w: checkpoint differs from the confirmed and attempted states", keyregistry.ErrStateRollback)
}

func recoveryReadError(err error, allowMissing bool) error {
	if errors.Is(err, keyregistry.ErrStateNotFound) {
		if allowMissing {
			return nil
		}
		return fmt.Errorf("%w: confirmed persistence file is missing", keyregistry.ErrStateRollback)
	}
	return err
}

func recoveryStateMatches(
	state keyregistry.StateFile, previous *keyregistry.StateFile, attempted keyregistry.StateFile,
) bool {
	return state.CurrentHash == attempted.CurrentHash || previous != nil && state.CurrentHash == previous.CurrentHash
}
