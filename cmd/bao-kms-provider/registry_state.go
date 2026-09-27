package main

import (
	"errors"
	"fmt"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/config"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/status"
)

const (
	stateCheckpointStatusAbsent  = "absent"
	stateCheckpointStatusMissing = "missing"
	stateCheckpointStatusCurrent = "current"
	stateCheckpointStatusBehind  = "behind"
)

type registryStateLoadResult struct {
	State                keyregistry.StateFile
	StateLoaded          bool
	CheckpointLoaded     bool
	CheckpointStatus     string
	CheckpointGeneration uint64
	CheckpointHash       string
}

func loadConfiguredRegistryState(cfg config.Config) (registryStateLoadResult, error) {
	loaded, err := loadRegistryStateWithCheckpoint(cfg.State.Path)
	if err != nil {
		return loaded, err
	}
	fingerprint, err := config.IdentityFingerprint(cfg)
	if err != nil {
		return registryStateLoadResult{}, err
	}
	if loaded.State.IdentityFingerprint != fingerprint {
		return registryStateLoadResult{}, fmt.Errorf(
			"%w: persisted configuration identity differs from current configuration", status.ErrConfigInvalid,
		)
	}
	return loaded, nil
}

func loadRegistryStateWithCheckpoint(statePath string) (registryStateLoadResult, error) {
	result := registryStateLoadResult{CheckpointStatus: stateCheckpointStatusAbsent}
	checkpoint, checkpointErr := keyregistry.LoadStateCheckpoint(keyregistry.StateCheckpointPath(statePath))
	checkpointLoaded := checkpointErr == nil
	if checkpointErr != nil && !errors.Is(checkpointErr, keyregistry.ErrStateNotFound) {
		return registryStateLoadResult{}, checkpointErr
	}
	if checkpointLoaded {
		result.CheckpointLoaded = true
		result.CheckpointGeneration = checkpoint.Generation
		result.CheckpointHash = checkpoint.CurrentHash
	}

	state, _, err := keyregistry.LoadStateFile(statePath, keyregistry.StateLoadOptions{})
	if err != nil {
		if errors.Is(err, keyregistry.ErrStateNotFound) {
			if checkpointLoaded {
				return registryStateLoadResult{}, fmt.Errorf(
					"%w: state file missing with checkpoint present",
					keyregistry.ErrStateRollback,
				)
			}
			return result, err
		}
		return registryStateLoadResult{}, err
	}

	result.State = state
	result.StateLoaded = true
	if !checkpointLoaded {
		result.CheckpointStatus = stateCheckpointStatusMissing
		return result, nil
	}
	if err := checkpoint.ValidateState(state); err != nil {
		return registryStateLoadResult{}, err
	}
	if state.Generation > checkpoint.Generation {
		result.CheckpointStatus = stateCheckpointStatusBehind
		return result, nil
	}
	result.CheckpointStatus = stateCheckpointStatusCurrent
	return result, nil
}
