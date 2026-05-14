package status

import (
	"errors"
	"fmt"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
)

const messageStatePathRequired = "state path is required"

// StateStore persists non-secret registry and rotation state.
type StateStore interface {
	Load() (keyregistry.StateFile, error)
	Save(keyregistry.StateFile) error
}

// FileStateStore persists registry state through the keyregistry state-file implementation.
type FileStateStore struct {
	Path string
}

// Load reads and validates the configured registry state file.
func (s FileStateStore) Load() (keyregistry.StateFile, error) {
	if s.Path == "" {
		return keyregistry.StateFile{}, fmt.Errorf("%w: %s", ErrConfigInvalid, messageStatePathRequired)
	}
	checkpointPath := keyregistry.StateCheckpointPath(s.Path)
	checkpoint, checkpointErr := keyregistry.LoadStateCheckpoint(checkpointPath)
	checkpointLoaded := checkpointErr == nil
	if checkpointErr != nil && !errors.Is(checkpointErr, keyregistry.ErrStateNotFound) {
		return keyregistry.StateFile{}, checkpointErr
	}

	state, _, err := keyregistry.LoadStateFile(s.Path, keyregistry.StateLoadOptions{})
	if err != nil {
		if checkpointLoaded && errors.Is(err, keyregistry.ErrStateNotFound) {
			return keyregistry.StateFile{}, fmt.Errorf(
				"%w: state file missing with checkpoint present",
				keyregistry.ErrStateRollback,
			)
		}
		return state, err
	}
	if checkpointLoaded {
		if err := checkpoint.ValidateState(state); err != nil {
			return keyregistry.StateFile{}, err
		}
		if state.Generation > checkpoint.Generation {
			return state, s.saveCheckpoint(state)
		}
		return state, nil
	}
	return state, s.saveCheckpoint(state)
}

// Save atomically writes the configured registry state file.
func (s FileStateStore) Save(state keyregistry.StateFile) error {
	if s.Path == "" {
		return fmt.Errorf("%w: %s", ErrConfigInvalid, messageStatePathRequired)
	}
	if err := keyregistry.SaveStateFile(s.Path, state); err != nil {
		return err
	}
	return s.saveCheckpoint(state)
}

func (s FileStateStore) saveCheckpoint(state keyregistry.StateFile) error {
	checkpoint, err := keyregistry.NewStateCheckpoint(state)
	if err != nil {
		return err
	}
	return keyregistry.SaveStateCheckpoint(keyregistry.StateCheckpointPath(s.Path), checkpoint)
}
