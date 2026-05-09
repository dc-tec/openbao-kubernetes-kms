package status

import (
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
	state, _, err := keyregistry.LoadStateFile(s.Path, keyregistry.StateLoadOptions{})
	return state, err
}

// Save atomically writes the configured registry state file.
func (s FileStateStore) Save(state keyregistry.StateFile) error {
	if s.Path == "" {
		return fmt.Errorf("%w: %s", ErrConfigInvalid, messageStatePathRequired)
	}
	return keyregistry.SaveStateFile(s.Path, state)
}
