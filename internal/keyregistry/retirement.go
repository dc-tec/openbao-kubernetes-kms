package keyregistry

import "fmt"

// RetireVersions records an operator decision to stop accepting historical keys
// below beforeVersion. It never removes the active key or runs during promotion.
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
		if SnapshotState(record.State) == StatePending {
			return StateFile{}, fmt.Errorf("cannot retire versions while rotation is pending")
		}
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
	return NewStateFileFromRecords(previous.ActiveKeyID, records, previous.Generation+1, previous.CurrentHash)
}

func validateRetainedSnapshots(previous StateFile, next StateFile) error {
	nextRecords := make(map[string]SnapshotStateRecord, len(next.Snapshots))
	for _, record := range next.Snapshots {
		nextRecords[record.KubernetesKeyID] = record
	}
	for _, record := range previous.Snapshots {
		nextRecord, ok := nextRecords[record.KubernetesKeyID]
		switch SnapshotState(record.State) {
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
