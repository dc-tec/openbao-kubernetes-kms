package status

import (
	"fmt"
	"sort"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
)

const (
	scopeValidationVersion       = 1
	scopeValidationCreatedAtUnix = 1
	initialTransitVersion        = 1
)

// SnapshotScope contains the identity-bearing inputs used for key_id derivation.
type SnapshotScope struct {
	ProviderName        string
	ClusterID           string
	OpenBaoInstanceID   string
	OpenBaoNamespace    string
	TransitMountID      string
	TransitKeyLineageID string
	AADMode             keyregistry.AADMode
}

// RotationPolicy controls observed Transit version promotion.
type RotationPolicy struct {
	ActivationDelay               time.Duration
	RequireStableObservationCount int
	RejectVersionRollback         bool
}

// ObservationResult describes one rotation state-machine transition.
type ObservationResult struct {
	State    keyregistry.StateFile
	Changed  bool
	Promoted bool
	Pending  bool
}

// Observer promotes new Transit versions only after stable observation and activation delay.
type Observer struct {
	scope  SnapshotScope
	policy RotationPolicy
}

// NewObserver validates and returns a rotation observer.
func NewObserver(scope SnapshotScope, policy RotationPolicy) (*Observer, error) {
	if policy.ActivationDelay < 0 {
		return nil, fmt.Errorf("%w: activation delay must not be negative", ErrConfigInvalid)
	}
	if policy.RequireStableObservationCount <= 0 {
		return nil, fmt.Errorf("%w: stable observation count must be positive", ErrConfigInvalid)
	}
	if scope.AADMode == "" {
		scope.AADMode = keyregistry.AADModeRequired
	}
	createdAt := time.Unix(scopeValidationCreatedAtUnix, 0).UTC()
	if _, err := scope.snapshot(scopeValidationVersion, createdAt, keyregistry.StateActive); err != nil {
		return nil, err
	}
	return &Observer{scope: scope, policy: policy}, nil
}

// RebuildState creates initial active state from live Transit metadata.
func (o *Observer) RebuildState(profile openbao.KeyProfile, now time.Time) (keyregistry.StateFile, error) {
	if err := validateProfile(profile); err != nil {
		return keyregistry.StateFile{}, err
	}
	versions, err := validatedVersionCreationTimes(profile)
	if err != nil {
		return keyregistry.StateFile{}, err
	}

	firstVersion := profile.MinAvailableVersion
	if firstVersion == 0 {
		firstVersion = 1
	}
	records := make([]keyregistry.SnapshotStateRecord, 0, profile.LatestVersion-firstVersion+1)
	activeKeyID := ""
	for version := firstVersion; version <= profile.LatestVersion; version++ {
		createdAt, ok := versions[version]
		if !ok {
			return keyregistry.StateFile{}, fmt.Errorf(
				"%w: Transit version creation time not found",
				ErrTransitMetadataInvalid,
			)
		}
		state := keyregistry.StateRetired
		promotedAt := time.Time{}
		if version == profile.LatestVersion {
			state = keyregistry.StateActive
			promotedAt = now
		}
		snapshot, snapshotErr := o.scope.snapshot(version, createdAt, state)
		if snapshotErr != nil {
			return keyregistry.StateFile{}, snapshotErr
		}
		if state == keyregistry.StateActive {
			activeKeyID = snapshot.KubernetesKeyID
		}
		records = append(records, recordFromSnapshot(snapshot, now, time.Time{}, 0, promotedAt))
	}

	state, err := keyregistry.NewStateFileFromRecords(activeKeyID, orderedRecords(activeKeyID, records), 1, "")
	if err != nil {
		return keyregistry.StateFile{}, err
	}
	if err := validateProfileForState(profile, state); err != nil {
		return keyregistry.StateFile{}, err
	}
	return state, nil
}

// Observe advances rotation state for one successful metadata observation.
func (o *Observer) Observe(
	state keyregistry.StateFile,
	profile openbao.KeyProfile,
	now time.Time,
) (ObservationResult, error) {
	if err := state.Validate(); err != nil {
		return ObservationResult{}, err
	}
	if err := validateProfile(profile); err != nil {
		return ObservationResult{}, err
	}
	if err := o.validateStateScope(state); err != nil {
		return ObservationResult{}, err
	}
	active, err := state.ActiveSnapshot()
	if err != nil {
		return ObservationResult{}, err
	}

	if profile.LatestVersion < active.TransitVersion {
		if o.policy.RejectVersionRollback {
			return ObservationResult{}, fmt.Errorf(
				"%w: observed Transit version is behind active version",
				ErrVersionRollback,
			)
		}
		return ObservationResult{State: state}, nil
	}
	if err := validateProfileForState(profile, state); err != nil {
		return ObservationResult{}, err
	}

	if profile.LatestVersion == active.TransitVersion {
		cleared, changed, clearErr := clearPendingRecords(state)
		if clearErr != nil {
			return ObservationResult{}, clearErr
		}
		return ObservationResult{State: cleared, Changed: changed}, nil
	}
	return o.observeNewerVersion(state, profile, now)
}

func (o *Observer) observeNewerVersion(
	state keyregistry.StateFile,
	profile openbao.KeyProfile,
	now time.Time,
) (ObservationResult, error) {
	candidate, err := o.snapshotForProfile(profile, profile.LatestVersion, keyregistry.StatePending)
	if err != nil {
		return ObservationResult{}, err
	}
	records, pendingRecord, err := upsertPendingRecord(
		state,
		recordFromSnapshot(candidate, now, time.Time{}, 1, time.Time{}),
		o.policy.RequireStableObservationCount,
		now,
	)
	if err != nil {
		return ObservationResult{}, err
	}
	if pendingReady(pendingRecord, o.policy.ActivationDelay, now) {
		promoted, promoteErr := promotePendingRecord(state, pendingRecord, now)
		if promoteErr != nil {
			return ObservationResult{}, promoteErr
		}
		return ObservationResult{State: promoted, Changed: true, Promoted: true}, nil
	}

	next, err := nextStateFromRecords(state, state.ActiveKeyID, records)
	if err != nil {
		return ObservationResult{}, err
	}
	return ObservationResult{State: next, Changed: true, Pending: true}, nil
}

func (s SnapshotScope) snapshot(
	version int,
	createdAt time.Time,
	state keyregistry.SnapshotState,
) (keyregistry.KeySnapshot, error) {
	snapshot := keyregistry.KeySnapshot{
		ProviderName:            s.ProviderName,
		ClusterID:               s.ClusterID,
		OpenBaoInstanceID:       s.OpenBaoInstanceID,
		OpenBaoNamespace:        s.OpenBaoNamespace,
		TransitMountID:          s.TransitMountID,
		TransitKeyLineageID:     s.TransitKeyLineageID,
		TransitVersion:          version,
		TransitVersionCreatedAt: createdAt.UTC(),
		State:                   state,
		AADMode:                 s.AADMode,
	}
	return snapshot.Normalize()
}

func (o *Observer) snapshotForProfile(
	profile openbao.KeyProfile,
	version int,
	state keyregistry.SnapshotState,
) (keyregistry.KeySnapshot, error) {
	createdAt, err := versionCreatedAt(profile, version)
	if err != nil {
		return keyregistry.KeySnapshot{}, err
	}
	return o.scope.snapshot(version, createdAt, state)
}

func (o *Observer) validateStateScope(state keyregistry.StateFile) error {
	for _, record := range state.Snapshots {
		snapshot, err := record.Snapshot()
		if err != nil {
			return err
		}
		switch {
		case snapshot.ProviderName != o.scope.ProviderName:
			return fmt.Errorf("%w: persisted state provider name differs from current configuration", ErrConfigInvalid)
		case snapshot.ClusterID != o.scope.ClusterID:
			return fmt.Errorf("%w: persisted state cluster ID differs from current configuration", ErrConfigInvalid)
		case snapshot.OpenBaoInstanceID != o.scope.OpenBaoInstanceID:
			return fmt.Errorf("%w: persisted state OpenBao instance ID differs from current configuration", ErrConfigInvalid)
		case snapshot.OpenBaoNamespace != o.scope.OpenBaoNamespace:
			return fmt.Errorf("%w: persisted state OpenBao namespace differs from current configuration", ErrConfigInvalid)
		case snapshot.TransitMountID != o.scope.TransitMountID:
			return fmt.Errorf("%w: persisted state Transit mount ID differs from current configuration", ErrConfigInvalid)
		case snapshot.TransitKeyLineageID != o.scope.TransitKeyLineageID:
			return fmt.Errorf("%w: persisted state Transit key lineage ID differs from current configuration", ErrConfigInvalid)
		case snapshot.AADMode != o.scope.AADMode:
			return fmt.Errorf("%w: persisted state AAD mode differs from current configuration", ErrConfigInvalid)
		}
	}
	return nil
}

func validateProfile(profile openbao.KeyProfile) error {
	if profile.LatestVersion <= 0 {
		return fmt.Errorf("%w: latest Transit version must be positive", ErrTransitMetadataInvalid)
	}
	if profile.MinAvailableVersion < 0 {
		return fmt.Errorf("%w: minimum available version must not be negative", ErrTransitMetadataInvalid)
	}
	if profile.SoftDeleted {
		return fmt.Errorf("%w: Transit key is soft-deleted", ErrTransitKeyUnusable)
	}
	if profile.MinAvailableVersion > profile.LatestVersion {
		return fmt.Errorf("%w: minimum available version exceeds latest version", ErrTransitKeyUnusable)
	}
	if len(openbao.AssessKeyProfile(profile)) > 0 {
		return fmt.Errorf("%w: Transit key profile has unsafe settings", ErrTransitKeyUnusable)
	}
	versions, err := validatedVersionCreationTimes(profile)
	if err != nil {
		return err
	}
	if _, ok := versions[profile.LatestVersion]; !ok {
		return fmt.Errorf("%w: Transit version creation time not found", ErrTransitMetadataInvalid)
	}
	return nil
}

func validateProfileForState(profile openbao.KeyProfile, state keyregistry.StateFile) error {
	for _, record := range state.Snapshots {
		snapshot, err := record.Snapshot()
		if err != nil {
			return err
		}
		switch snapshot.State {
		case keyregistry.StateActive:
			if err := validateActiveSnapshot(profile, snapshot); err != nil {
				return err
			}
		case keyregistry.StateRetired:
			if err := validateHistoricalSnapshot(profile, snapshot); err != nil {
				return err
			}
		case keyregistry.StatePending, keyregistry.StateRejected:
		default:
			return fmt.Errorf("%w: unsupported snapshot state", ErrTransitMetadataInvalid)
		}
	}
	return nil
}

func validateActiveSnapshot(profile openbao.KeyProfile, snapshot keyregistry.KeySnapshot) error {
	if profile.MinEncryptionVersion > snapshot.TransitVersion {
		return fmt.Errorf("%w: active Transit version cannot encrypt", ErrTransitKeyUnusable)
	}
	if err := validateDecryptableSnapshot(profile, snapshot, "active"); err != nil {
		return err
	}
	return nil
}

func validateHistoricalSnapshot(profile openbao.KeyProfile, snapshot keyregistry.KeySnapshot) error {
	return validateDecryptableSnapshot(profile, snapshot, "historical")
}

func validateDecryptableSnapshot(profile openbao.KeyProfile, snapshot keyregistry.KeySnapshot, label string) error {
	if profile.MinAvailableVersion > snapshot.TransitVersion {
		return fmt.Errorf("%w: %s Transit version is unavailable", ErrTransitKeyUnusable, label)
	}
	if profile.MinDecryptionVersion > snapshot.TransitVersion {
		return fmt.Errorf("%w: %s Transit version cannot decrypt", ErrTransitKeyUnusable, label)
	}
	createdAt, err := versionCreatedAt(profile, snapshot.TransitVersion)
	if err != nil {
		return err
	}
	if !createdAt.Equal(snapshot.TransitVersionCreatedAt.UTC()) {
		return fmt.Errorf("%w: %s Transit version creation time changed", ErrTransitMetadataInvalid, label)
	}
	return nil
}

func versionCreatedAt(profile openbao.KeyProfile, version int) (time.Time, error) {
	versions, err := validatedVersionCreationTimes(profile)
	if err != nil {
		return time.Time{}, err
	}
	createdAt, ok := versions[version]
	if !ok {
		return time.Time{}, fmt.Errorf("%w: Transit version creation time not found", ErrTransitMetadataInvalid)
	}
	return createdAt, nil
}

func validatedVersionCreationTimes(profile openbao.KeyProfile) (map[int]time.Time, error) {
	versions := make(map[int]time.Time, len(profile.VersionCreationTimes))
	for _, candidate := range profile.VersionCreationTimes {
		switch {
		case candidate.Version <= 0:
			return nil, fmt.Errorf("%w: Transit version must be positive", ErrTransitMetadataInvalid)
		case candidate.Version > profile.LatestVersion:
			return nil, fmt.Errorf("%w: Transit version exceeds latest version", ErrTransitMetadataInvalid)
		case candidate.CreatedAt.IsZero():
			return nil, fmt.Errorf("%w: Transit version creation time is missing", ErrTransitMetadataInvalid)
		}
		if _, ok := versions[candidate.Version]; ok {
			return nil, fmt.Errorf("%w: duplicate Transit version creation time", ErrTransitMetadataInvalid)
		}
		versions[candidate.Version] = candidate.CreatedAt.UTC()
	}
	return versions, nil
}

func upsertPendingRecord(
	state keyregistry.StateFile,
	candidate keyregistry.SnapshotStateRecord,
	stableThreshold int,
	now time.Time,
) ([]keyregistry.SnapshotStateRecord, keyregistry.SnapshotStateRecord, error) {
	records := make([]keyregistry.SnapshotStateRecord, 0, len(state.Snapshots)+1)
	pending := candidate
	found := false
	for _, record := range state.Snapshots {
		snapshot, err := record.Snapshot()
		if err != nil {
			return nil, keyregistry.SnapshotStateRecord{}, err
		}
		if snapshot.State == keyregistry.StatePending {
			if snapshot.KubernetesKeyID == candidate.KubernetesKeyID {
				pending = record
				found = true
			}
			continue
		}
		records = append(records, record)
	}

	if found {
		pending.StableObservationCount++
	} else if pending.StableObservationCount == 0 {
		pending.StableObservationCount = 1
	}
	if pending.ObservedAtUnix == 0 {
		pending.ObservedAtUnix = now.Unix()
	}
	if pending.StableObservationCount >= stableThreshold && pending.StableAtUnix == 0 {
		pending.StableAtUnix = now.Unix()
	}
	records = append(records, pending)
	return orderedRecords(state.ActiveKeyID, records), pending, nil
}

func pendingReady(record keyregistry.SnapshotStateRecord, activationDelay time.Duration, now time.Time) bool {
	if record.StableAtUnix == 0 {
		return false
	}
	stableAt := time.Unix(record.StableAtUnix, 0).UTC()
	return !now.Before(stableAt.Add(activationDelay))
}

func promotePendingRecord(
	state keyregistry.StateFile,
	pending keyregistry.SnapshotStateRecord,
	now time.Time,
) (keyregistry.StateFile, error) {
	pendingSnapshot, err := pending.Snapshot()
	if err != nil {
		return keyregistry.StateFile{}, err
	}
	pendingSnapshot.State = keyregistry.StateActive
	promotedActive, err := pendingSnapshot.Normalize()
	if err != nil {
		return keyregistry.StateFile{}, err
	}

	records := make([]keyregistry.SnapshotStateRecord, 0, len(state.Snapshots)+1)
	activeRetired := false
	for _, record := range state.Snapshots {
		snapshot, snapshotErr := record.Snapshot()
		if snapshotErr != nil {
			return keyregistry.StateFile{}, snapshotErr
		}
		if snapshot.KubernetesKeyID == pending.KubernetesKeyID {
			continue
		}
		if snapshot.KubernetesKeyID == state.ActiveKeyID {
			snapshot.State = keyregistry.StateRetired
			retired, normalizeErr := snapshot.Normalize()
			if normalizeErr != nil {
				return keyregistry.StateFile{}, normalizeErr
			}
			record = recordFromSnapshot(
				retired,
				recordTime(record.ObservedAtUnix),
				time.Time{},
				0,
				recordTime(record.PromotedAtUnix),
			)
			activeRetired = true
		}
		records = append(records, record)
	}
	if !activeRetired {
		return keyregistry.StateFile{}, fmt.Errorf("%w: active state record missing", ErrStateUnavailable)
	}

	activeRecord := recordFromSnapshot(
		promotedActive,
		recordTime(pending.ObservedAtUnix),
		recordTime(pending.StableAtUnix),
		pending.StableObservationCount,
		now,
	)
	records = append(records, activeRecord)
	return nextStateFromRecords(
		state,
		promotedActive.KubernetesKeyID,
		orderedRecords(promotedActive.KubernetesKeyID, records),
	)
}

func clearPendingRecords(state keyregistry.StateFile) (keyregistry.StateFile, bool, error) {
	records := make([]keyregistry.SnapshotStateRecord, 0, len(state.Snapshots))
	changed := false
	for _, record := range state.Snapshots {
		snapshot, err := record.Snapshot()
		if err != nil {
			return keyregistry.StateFile{}, false, err
		}
		if snapshot.State == keyregistry.StatePending {
			changed = true
			continue
		}
		records = append(records, record)
	}
	if !changed {
		return state, false, nil
	}
	next, err := nextStateFromRecords(state, state.ActiveKeyID, orderedRecords(state.ActiveKeyID, records))
	if err != nil {
		return keyregistry.StateFile{}, false, err
	}
	return next, true, nil
}

func nextStateFromRecords(
	previous keyregistry.StateFile,
	activeKeyID string,
	records []keyregistry.SnapshotStateRecord,
) (keyregistry.StateFile, error) {
	next, err := keyregistry.NewStateFileFromRecords(
		activeKeyID,
		orderedRecords(activeKeyID, records),
		previous.Generation+1,
		previous.CurrentHash,
	)
	if err != nil {
		return keyregistry.StateFile{}, err
	}
	if err := keyregistry.ValidateStateProgress(previous, next); err != nil {
		return keyregistry.StateFile{}, err
	}
	return next, nil
}

func recordFromSnapshot(
	snapshot keyregistry.KeySnapshot,
	observedAt time.Time,
	stableAt time.Time,
	stableCount int,
	promotedAt time.Time,
) keyregistry.SnapshotStateRecord {
	record := keyregistry.SnapshotStateRecordFromSnapshot(snapshot)
	if !observedAt.IsZero() {
		record.ObservedAtUnix = observedAt.UTC().Unix()
	}
	if stableCount > 0 {
		record.StableObservationCount = stableCount
	}
	if !stableAt.IsZero() {
		record.StableAtUnix = stableAt.UTC().Unix()
	}
	if !promotedAt.IsZero() {
		record.PromotedAtUnix = promotedAt.UTC().Unix()
	}
	return record
}

func recordTime(unix int64) time.Time {
	if unix == 0 {
		return time.Time{}
	}
	return time.Unix(unix, 0).UTC()
}

func orderedRecords(
	activeKeyID string,
	records []keyregistry.SnapshotStateRecord,
) []keyregistry.SnapshotStateRecord {
	ordered := make([]keyregistry.SnapshotStateRecord, len(records))
	copy(ordered, records)
	sort.SliceStable(ordered, func(i int, j int) bool {
		left := recordSortKey(activeKeyID, ordered[i])
		right := recordSortKey(activeKeyID, ordered[j])
		if left.priority != right.priority {
			return left.priority < right.priority
		}
		if left.version != right.version {
			return left.version > right.version
		}
		return left.keyID < right.keyID
	})
	return ordered
}

type recordSort struct {
	priority int
	version  int
	keyID    string
}

func recordSortKey(activeKeyID string, record keyregistry.SnapshotStateRecord) recordSort {
	var priority int
	if record.KubernetesKeyID == activeKeyID {
		priority = 0
	} else {
		switch keyregistry.SnapshotState(record.State) {
		case keyregistry.StatePending:
			priority = 1
		case keyregistry.StateRetired:
			priority = 2
		case keyregistry.StateRejected:
			priority = 3
		default:
			priority = 4
		}
	}
	return recordSort{
		priority: priority,
		version:  record.TransitVersion,
		keyID:    record.KubernetesKeyID,
	}
}
