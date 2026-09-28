package keyregistry

import "fmt"

// RetireVersions records an operator decision to stop accepting historical keys
// below beforeVersion. It never removes the active or pending keys.
// Callers must obtain independent migration and backup evidence before applying
// this transition. Transit metadata alone cannot establish that evidence.
func RetireVersions(previous StateFile, beforeVersion int) (StateFile, error) {
	if err := previous.Validate(); err != nil {
		return StateFile{}, err
	}
	active, err := previous.ActiveSnapshot()
	if err != nil {
		return StateFile{}, err
	}
	if beforeVersion <= 1 || beforeVersion > active.TransitVersion {
		return StateFile{}, fmt.Errorf("retirement boundary must be greater than 1 and at most active Transit version %d",
			active.TransitVersion)
	}
	records := append([]SnapshotStateRecord(nil), previous.Snapshots...)
	changed := false
	for i, record := range records {
		if SnapshotState(record.State) == StateRetired && record.TransitVersion < beforeVersion {
			records[i].State = string(StateRemoved)
			changed = true
		}
	}
	if !changed {
		return previous, nil
	}
	// This constructor is the only supported transition that intentionally drops
	// decrypt eligibility. Normal rotation uses ValidateStateProgress instead.
	return NewStateFileFromRecords(
		previous.ActiveKeyID, records, previous.Generation+1, previous.CurrentHash, previous.IdentityFingerprint,
	)
}

// RestoreVersions records an operator decision to restore removed historical
// versions to decrypt lookup. It preserves their identity and the active key.
// Callers must validate the resulting state against current Transit metadata,
// hold the provider's state lock, and confirm the reviewed state hash.
// Ordinary rotation and discovery must never perform this transition.
func RestoreVersions(previous StateFile, versions []int) (StateFile, error) {
	if err := previous.Validate(); err != nil {
		return StateFile{}, err
	}
	active, err := previous.ActiveSnapshot()
	if err != nil {
		return StateFile{}, err
	}
	selected := make(map[int]bool, len(versions))
	for _, version := range versions {
		if version <= 0 || version >= active.TransitVersion || selected[version] {
			return StateFile{}, fmt.Errorf("restore versions must be distinct positive historical versions below active")
		}
		selected[version] = true
	}
	if len(selected) == 0 {
		return StateFile{}, fmt.Errorf("select at least one removed version to restore")
	}
	records := append([]SnapshotStateRecord(nil), previous.Snapshots...)
	for i, record := range records {
		if !selected[record.TransitVersion] {
			continue
		}
		if SnapshotState(record.State) != StateRemoved {
			return StateFile{}, fmt.Errorf("restore requires a removed version: %d", record.TransitVersion)
		}
		records[i].State = string(StateRetired)
		delete(selected, record.TransitVersion)
	}
	if len(selected) != 0 {
		return StateFile{}, fmt.Errorf("selected version is absent from the local registry")
	}
	return NewStateFileFromRecords(
		previous.ActiveKeyID, records, previous.Generation+1, previous.CurrentHash, previous.IdentityFingerprint,
	)
}

func validateRetainedSnapshots(previous StateFile, next StateFile) error {
	nextRecords := make(map[string]SnapshotStateRecord, len(next.Snapshots))
	for _, record := range next.Snapshots {
		nextRecords[record.KubernetesKeyID] = record
	}
	for _, record := range previous.Snapshots {
		nextRecord, ok := nextRecords[record.KubernetesKeyID]
		switch SnapshotState(record.State) {
		case StatePending:
			if !ok || (SnapshotState(nextRecord.State) != StatePending &&
				SnapshotState(nextRecord.State) != StateActive && SnapshotState(nextRecord.State) != StateRetired) {
				return fmt.Errorf("%w: decryptable pending snapshot lost", ErrStateRollback)
			}
		case StateActive, StateRetired:
			if !ok || (SnapshotState(nextRecord.State) != StateActive && SnapshotState(nextRecord.State) != StateRetired) {
				return fmt.Errorf("%w: decryptable snapshot lost; use the operator retirement transition", ErrStateRollback)
			}
		case StateRemoved:
			if !ok || nextRecord != record {
				return fmt.Errorf("%w: removed snapshot record changed", ErrStateRollback)
			}
		}
	}
	return nil
}
