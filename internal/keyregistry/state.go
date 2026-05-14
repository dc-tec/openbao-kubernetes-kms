package keyregistry

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	stateFileVersion       = "keyregistry.openbao-kms/v1alpha1"
	stateCheckpointVersion = "keyregistry.openbao-kms/checkpoint/v1alpha1"
	stateHashPrefix        = "krs1."
	stateFileMode          = os.FileMode(0o640)

	stateFileDisallowedMode = os.FileMode(0o137)
)

var (
	// ErrStateNotFound identifies a missing local registry state file.
	ErrStateNotFound = errors.New("key registry state not found")
	// ErrStateCorrupt identifies registry state that cannot be decoded or fails integrity checks.
	ErrStateCorrupt = errors.New("key registry state corrupt")
	// ErrStatePermission identifies unsafe registry state file or parent permissions.
	ErrStatePermission = errors.New("key registry state permissions unsafe")
	// ErrStateRollback identifies replayed or rollback registry state.
	ErrStateRollback = errors.New("key registry state rollback detected")
)

// StateFile contains the non-secret local registry state persisted across restarts.
type StateFile struct {
	SchemaVersion string                `json:"schemaVersion"`
	Generation    uint64                `json:"generation"`
	PreviousHash  string                `json:"previousHash,omitempty"`
	CurrentHash   string                `json:"currentHash"`
	ActiveKeyID   string                `json:"activeKeyId"`
	Snapshots     []SnapshotStateRecord `json:"snapshots"`
}

// StateCheckpoint is the small replay anchor saved next to the registry state file.
type StateCheckpoint struct {
	SchemaVersion string `json:"schemaVersion"`
	Generation    uint64 `json:"generation"`
	CurrentHash   string `json:"currentHash"`
}

// SnapshotStateRecord is the JSON representation of one observed or promoted snapshot.
type SnapshotStateRecord struct {
	ProviderName                string `json:"providerName"`
	ClusterID                   string `json:"clusterId"`
	OpenBaoInstanceID           string `json:"openbaoInstanceId"`
	OpenBaoNamespace            string `json:"openbaoNamespace,omitempty"`
	TransitMountID              string `json:"transitMountId"`
	TransitKeyLineageID         string `json:"transitKeyLineageId"`
	TransitVersion              int    `json:"transitVersion"`
	TransitVersionCreatedAtUnix int64  `json:"transitVersionCreatedAtUnix"`
	KubernetesKeyID             string `json:"kubernetesKeyId"`
	State                       string `json:"state"`
	AADMode                     string `json:"aadMode"`
	ObservedAtUnix              int64  `json:"observedAtUnix,omitempty"`
	StableObservationCount      int    `json:"stableObservationCount,omitempty"`
	StableAtUnix                int64  `json:"stableAtUnix,omitempty"`
	PromotedAtUnix              int64  `json:"promotedAtUnix,omitempty"`
}

// StateLoadOptions controls restart and replay validation when loading registry state.
type StateLoadOptions struct {
	MinimumGeneration uint64
	ExpectedHash      string
}

// NewStateFile builds a validated state file from active and historical snapshots.
func NewStateFile(
	active KeySnapshot,
	historical []KeySnapshot,
	generation uint64,
	previousHash string,
) (StateFile, error) {
	if generation == 0 {
		return StateFile{}, fmt.Errorf("state generation must be positive")
	}
	registry, err := NewRegistry(active, historical)
	if err != nil {
		return StateFile{}, err
	}
	normalizedActive, ok := registry.Active()
	if !ok {
		return StateFile{}, fmt.Errorf("active snapshot missing")
	}

	records := make([]SnapshotStateRecord, 0, len(historical)+1)
	records = append(records, SnapshotStateRecordFromSnapshot(normalizedActive))
	for _, snapshot := range historical {
		normalized, normalizeErr := snapshot.Normalize()
		if normalizeErr != nil {
			return StateFile{}, normalizeErr
		}
		records = append(records, SnapshotStateRecordFromSnapshot(normalized))
	}

	state := StateFile{
		SchemaVersion: stateFileVersion,
		Generation:    generation,
		PreviousHash:  previousHash,
		ActiveKeyID:   normalizedActive.KubernetesKeyID,
		Snapshots:     records,
	}
	hash, err := state.computeHash()
	if err != nil {
		return StateFile{}, err
	}
	state.CurrentHash = hash
	if err := state.Validate(); err != nil {
		return StateFile{}, err
	}
	return state, nil
}

// NewStateFileFromRecords builds a validated state file from pre-normalized records.
func NewStateFileFromRecords(
	activeKeyID string,
	records []SnapshotStateRecord,
	generation uint64,
	previousHash string,
) (StateFile, error) {
	if generation == 0 {
		return StateFile{}, fmt.Errorf("state generation must be positive")
	}
	if activeKeyID == "" {
		return StateFile{}, fmt.Errorf("active key_id is required")
	}
	if len(records) == 0 {
		return StateFile{}, fmt.Errorf("state records are required")
	}

	normalizedRecords := make([]SnapshotStateRecord, 0, len(records))
	for _, record := range records {
		normalized, err := normalizeStateRecord(record)
		if err != nil {
			return StateFile{}, err
		}
		normalizedRecords = append(normalizedRecords, normalized)
	}

	state := StateFile{
		SchemaVersion: stateFileVersion,
		Generation:    generation,
		PreviousHash:  previousHash,
		ActiveKeyID:   activeKeyID,
		Snapshots:     normalizedRecords,
	}
	hash, err := state.computeHash()
	if err != nil {
		return StateFile{}, err
	}
	state.CurrentHash = hash
	if err := state.Validate(); err != nil {
		return StateFile{}, err
	}
	return state, nil
}

// RebuildStateFromMetadata creates a restart-safe state from config and Transit metadata when no state exists.
func RebuildStateFromMetadata(active KeySnapshot, historical []KeySnapshot) (StateFile, error) {
	return NewStateFile(active, historical, 1, "")
}

// PromoteState creates the next state generation and rejects active key rollback.
func PromoteState(previous StateFile, active KeySnapshot, historical []KeySnapshot) (StateFile, error) {
	if err := previous.Validate(); err != nil {
		return StateFile{}, err
	}
	next, err := NewStateFile(active, historical, previous.Generation+1, previous.CurrentHash)
	if err != nil {
		return StateFile{}, err
	}
	if err := ValidateStateProgress(previous, next); err != nil {
		return StateFile{}, err
	}
	return next, nil
}

// SnapshotStateRecordFromSnapshot converts a snapshot into a stable JSON state record.
func SnapshotStateRecordFromSnapshot(snapshot KeySnapshot) SnapshotStateRecord {
	return SnapshotStateRecord{
		ProviderName:                snapshot.ProviderName,
		ClusterID:                   snapshot.ClusterID,
		OpenBaoInstanceID:           snapshot.OpenBaoInstanceID,
		OpenBaoNamespace:            snapshot.OpenBaoNamespace,
		TransitMountID:              snapshot.TransitMountID,
		TransitKeyLineageID:         snapshot.TransitKeyLineageID,
		TransitVersion:              snapshot.TransitVersion,
		TransitVersionCreatedAtUnix: snapshot.TransitVersionCreatedAt.Unix(),
		KubernetesKeyID:             snapshot.KubernetesKeyID,
		State:                       string(snapshot.State),
		AADMode:                     string(snapshot.AADMode),
	}
}

func normalizeStateRecord(record SnapshotStateRecord) (SnapshotStateRecord, error) {
	snapshot, err := record.Snapshot()
	if err != nil {
		return SnapshotStateRecord{}, err
	}
	record.ProviderName = snapshot.ProviderName
	record.ClusterID = snapshot.ClusterID
	record.OpenBaoInstanceID = snapshot.OpenBaoInstanceID
	record.OpenBaoNamespace = snapshot.OpenBaoNamespace
	record.TransitMountID = snapshot.TransitMountID
	record.TransitKeyLineageID = snapshot.TransitKeyLineageID
	record.TransitVersion = snapshot.TransitVersion
	record.TransitVersionCreatedAtUnix = snapshot.TransitVersionCreatedAt.Unix()
	record.KubernetesKeyID = snapshot.KubernetesKeyID
	record.State = string(snapshot.State)
	record.AADMode = string(snapshot.AADMode)
	return record, nil
}

// Snapshot returns the runtime snapshot represented by the record.
func (r SnapshotStateRecord) Snapshot() (KeySnapshot, error) {
	if err := validateObservationMetadata(r); err != nil {
		return KeySnapshot{}, err
	}
	snapshot := KeySnapshot{
		ProviderName:            r.ProviderName,
		ClusterID:               r.ClusterID,
		OpenBaoInstanceID:       r.OpenBaoInstanceID,
		OpenBaoNamespace:        r.OpenBaoNamespace,
		TransitMountID:          r.TransitMountID,
		TransitKeyLineageID:     r.TransitKeyLineageID,
		TransitVersion:          r.TransitVersion,
		TransitVersionCreatedAt: time.Unix(r.TransitVersionCreatedAtUnix, 0).UTC(),
		KubernetesKeyID:         r.KubernetesKeyID,
		State:                   SnapshotState(r.State),
		AADMode:                 AADMode(r.AADMode),
	}
	return snapshot.Normalize()
}

func validateObservationMetadata(record SnapshotStateRecord) error {
	if record.ObservedAtUnix < 0 {
		return fmt.Errorf("%w: observed time must not be negative", ErrStateCorrupt)
	}
	if record.StableObservationCount < 0 {
		return fmt.Errorf("%w: stable observation count must not be negative", ErrStateCorrupt)
	}
	if record.StableAtUnix < 0 {
		return fmt.Errorf("%w: stable time must not be negative", ErrStateCorrupt)
	}
	if record.PromotedAtUnix < 0 {
		return fmt.Errorf("%w: promoted time must not be negative", ErrStateCorrupt)
	}
	if record.StableObservationCount > 0 && record.ObservedAtUnix == 0 {
		return fmt.Errorf("%w: observed time is required with stable observations", ErrStateCorrupt)
	}
	if record.StableAtUnix != 0 && record.StableObservationCount == 0 {
		return fmt.Errorf("%w: stable observations are required with stable time", ErrStateCorrupt)
	}
	if record.StableAtUnix != 0 && record.ObservedAtUnix != 0 && record.StableAtUnix < record.ObservedAtUnix {
		return fmt.Errorf("%w: stable time precedes observed time", ErrStateCorrupt)
	}
	if record.PromotedAtUnix != 0 && record.ObservedAtUnix != 0 && record.PromotedAtUnix < record.ObservedAtUnix {
		return fmt.Errorf("%w: promoted time precedes observed time", ErrStateCorrupt)
	}
	return nil
}

// Validate verifies state schema, content, active snapshot, and current hash.
func (s StateFile) Validate() error {
	if s.SchemaVersion != stateFileVersion {
		return fmt.Errorf("%w: unsupported schema version", ErrStateCorrupt)
	}
	if s.Generation == 0 {
		return fmt.Errorf("%w: generation must be positive", ErrStateCorrupt)
	}
	if s.ActiveKeyID == "" {
		return fmt.Errorf("%w: active key_id is required", ErrStateCorrupt)
	}
	if len(s.Snapshots) == 0 {
		return fmt.Errorf("%w: snapshots are required", ErrStateCorrupt)
	}
	if s.PreviousHash != "" {
		if err := validateStateHash(s.PreviousHash); err != nil {
			return err
		}
	}
	if err := validateStateHash(s.CurrentHash); err != nil {
		return err
	}
	hash, err := s.computeHash()
	if err != nil {
		return err
	}
	if s.CurrentHash != hash {
		return fmt.Errorf("%w: state hash mismatch", ErrStateCorrupt)
	}
	if _, err := s.Registry(); err != nil {
		return err
	}
	return nil
}

// Registry returns a decryptable in-memory registry from the persisted state.
func (s StateFile) Registry() (Registry, error) {
	active, historical, err := s.snapshots()
	if err != nil {
		return Registry{}, err
	}
	decryptableHistorical := make([]KeySnapshot, 0, len(historical))
	for _, snapshot := range historical {
		switch snapshot.State {
		case StateRetired:
			decryptableHistorical = append(decryptableHistorical, snapshot)
		case StatePending, StateRejected:
		default:
			return Registry{}, fmt.Errorf("%w: snapshot state %q is invalid", ErrStateCorrupt, snapshot.State)
		}
	}
	return NewRegistry(active, decryptableHistorical)
}

// ActiveSnapshot returns the promoted active snapshot from the state.
func (s StateFile) ActiveSnapshot() (KeySnapshot, error) {
	active, _, err := s.snapshots()
	if err != nil {
		return KeySnapshot{}, err
	}
	return active, nil
}

// ValidateStateProgress verifies monotonic generation, hash chain, and active key version progress.
func ValidateStateProgress(previous StateFile, next StateFile) error {
	if err := previous.Validate(); err != nil {
		return err
	}
	if err := next.Validate(); err != nil {
		return err
	}
	if next.Generation <= previous.Generation {
		return fmt.Errorf("%w: generation did not increase", ErrStateRollback)
	}
	if next.PreviousHash != previous.CurrentHash {
		return fmt.Errorf("%w: previous hash does not match", ErrStateRollback)
	}

	previousActive, err := previous.ActiveSnapshot()
	if err != nil {
		return err
	}
	nextActive, err := next.ActiveSnapshot()
	if err != nil {
		return err
	}
	if nextActive.TransitVersion < previousActive.TransitVersion {
		return fmt.Errorf("%w: active Transit version decreased", ErrStateRollback)
	}
	return nil
}

// LoadStateFile loads and validates local registry state from disk.
func LoadStateFile(path string, opts StateLoadOptions) (StateFile, Registry, error) {
	if err := validateStateFilePath(path); err != nil {
		return StateFile{}, Registry{}, err
	}

	// #nosec G304 -- registry state path is operator-controlled local configuration.
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return StateFile{}, Registry{}, ErrStateNotFound
		}
		return StateFile{}, Registry{}, fmt.Errorf("open registry state: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	state, err := decodeState(file)
	if err != nil {
		return StateFile{}, Registry{}, err
	}
	if err := validateLoadedState(state, opts); err != nil {
		return StateFile{}, Registry{}, err
	}
	registry, err := state.Registry()
	if err != nil {
		return StateFile{}, Registry{}, err
	}
	return state, registry, nil
}

// SaveStateFile writes validated local registry state atomically.
func SaveStateFile(path string, state StateFile) error {
	if err := state.Validate(); err != nil {
		return err
	}
	if err := validateStateFileWritePath(path); err != nil {
		return err
	}

	if err := saveEncodedFile(path, func(encoder *json.Encoder) error {
		return encoder.Encode(state)
	}); err != nil {
		return err
	}
	return nil
}

// StateCheckpointPath returns the replay-checkpoint path associated with a state path.
func StateCheckpointPath(statePath string) string {
	return statePath + ".checkpoint"
}

// NewStateCheckpoint builds a validated replay checkpoint for a state file.
func NewStateCheckpoint(state StateFile) (StateCheckpoint, error) {
	if err := state.Validate(); err != nil {
		return StateCheckpoint{}, err
	}
	checkpoint := StateCheckpoint{
		SchemaVersion: stateCheckpointVersion,
		Generation:    state.Generation,
		CurrentHash:   state.CurrentHash,
	}
	if err := checkpoint.Validate(); err != nil {
		return StateCheckpoint{}, err
	}
	return checkpoint, nil
}

// Validate verifies checkpoint schema and hash shape.
func (c StateCheckpoint) Validate() error {
	if c.SchemaVersion != stateCheckpointVersion {
		return fmt.Errorf("%w: unsupported state checkpoint schema version", ErrStateCorrupt)
	}
	if c.Generation == 0 {
		return fmt.Errorf("%w: checkpoint generation must be positive", ErrStateCorrupt)
	}
	if err := validateStateHash(c.CurrentHash); err != nil {
		return err
	}
	return nil
}

// ValidateState rejects state older than the checkpoint or with a mismatched same-generation hash.
func (c StateCheckpoint) ValidateState(state StateFile) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if err := state.Validate(); err != nil {
		return err
	}
	if state.Generation < c.Generation {
		return fmt.Errorf("%w: generation below checkpoint", ErrStateRollback)
	}
	if state.Generation == c.Generation && state.CurrentHash != c.CurrentHash {
		return fmt.Errorf("%w: current hash differs from checkpoint", ErrStateRollback)
	}
	return nil
}

// LoadStateCheckpoint loads and validates the replay checkpoint from disk.
func LoadStateCheckpoint(path string) (StateCheckpoint, error) {
	if err := validateStateFilePath(path); err != nil {
		return StateCheckpoint{}, err
	}

	// #nosec G304 -- checkpoint path is derived from operator-controlled local state configuration.
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return StateCheckpoint{}, ErrStateNotFound
		}
		return StateCheckpoint{}, fmt.Errorf("open registry state checkpoint: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var checkpoint StateCheckpoint
	if err := decoder.Decode(&checkpoint); err != nil {
		return StateCheckpoint{}, fmt.Errorf("%w: checkpoint decode failed: %w", ErrStateCorrupt, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return StateCheckpoint{}, fmt.Errorf("%w: checkpoint trailing content", ErrStateCorrupt)
	}
	if err := checkpoint.Validate(); err != nil {
		return StateCheckpoint{}, err
	}
	return checkpoint, nil
}

// SaveStateCheckpoint writes a replay checkpoint durably.
func SaveStateCheckpoint(path string, checkpoint StateCheckpoint) error {
	if err := checkpoint.Validate(); err != nil {
		return err
	}
	if err := validateStateFileWritePath(path); err != nil {
		return err
	}
	return saveEncodedFile(path, func(encoder *json.Encoder) error {
		return encoder.Encode(checkpoint)
	})
}

func saveEncodedFile(path string, encode func(*json.Encoder) error) error {
	dir := filepath.Dir(path)
	name := filepath.Base(path)
	tempPath := filepath.Join(dir, "."+name+".tmp")
	_ = os.Remove(tempPath)

	// #nosec G304 -- registry state path is operator-controlled local configuration.
	file, err := os.OpenFile(tempPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, stateFileMode)
	if err != nil {
		return fmt.Errorf("create registry state temp file: %w", err)
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	encodeErr := encode(encoder)
	if encodeErr != nil {
		_ = file.Close()
		_ = os.Remove(tempPath)
		return fmt.Errorf("encode registry state: %w", encodeErr)
	}
	if err := file.Chmod(stateFileMode); err != nil {
		_ = file.Close()
		_ = os.Remove(tempPath)
		return fmt.Errorf("chmod registry state temp file: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(tempPath)
		return fmt.Errorf("sync registry state temp file: %w", err)
	}
	closeErr := file.Close()
	if closeErr != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("close registry state temp file: %w", closeErr)
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("rename registry state file: %w", err)
	}
	if err := syncParentDir(path); err != nil {
		return err
	}
	return nil
}

type stateHashBody struct {
	SchemaVersion string                `json:"schemaVersion"`
	Generation    uint64                `json:"generation"`
	PreviousHash  string                `json:"previousHash,omitempty"`
	ActiveKeyID   string                `json:"activeKeyId"`
	Snapshots     []SnapshotStateRecord `json:"snapshots"`
}

func (s StateFile) computeHash() (string, error) {
	body := stateHashBody{
		SchemaVersion: s.SchemaVersion,
		Generation:    s.Generation,
		PreviousHash:  s.PreviousHash,
		ActiveKeyID:   s.ActiveKeyID,
		Snapshots:     s.Snapshots,
	}
	canonical, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal registry state hash body: %w", err)
	}
	sum := sha256.Sum256(canonical)
	return stateHashPrefix + base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

func (s StateFile) snapshots() (KeySnapshot, []KeySnapshot, error) {
	historical := make([]KeySnapshot, 0, len(s.Snapshots)-1)
	var active KeySnapshot
	activeSeen := false
	seen := make(map[string]struct{}, len(s.Snapshots))
	for _, record := range s.Snapshots {
		snapshot, err := record.Snapshot()
		if err != nil {
			return KeySnapshot{}, nil, fmt.Errorf("%w: invalid snapshot: %w", ErrStateCorrupt, err)
		}
		if _, ok := seen[snapshot.KubernetesKeyID]; ok {
			return KeySnapshot{}, nil, fmt.Errorf("%w: duplicate key_id", ErrStateCorrupt)
		}
		seen[snapshot.KubernetesKeyID] = struct{}{}
		if snapshot.KubernetesKeyID == s.ActiveKeyID {
			if snapshot.State != StateActive {
				return KeySnapshot{}, nil, fmt.Errorf("%w: active key_id must have active state", ErrStateCorrupt)
			}
			if activeSeen {
				return KeySnapshot{}, nil, fmt.Errorf("%w: duplicate active key_id", ErrStateCorrupt)
			}
			active = snapshot
			activeSeen = true
			continue
		}
		if snapshot.State == StateActive {
			return KeySnapshot{}, nil, fmt.Errorf("%w: non-selected snapshot has active state", ErrStateCorrupt)
		}
		historical = append(historical, snapshot)
	}
	if !activeSeen {
		return KeySnapshot{}, nil, fmt.Errorf("%w: active snapshot missing", ErrStateCorrupt)
	}
	return active, historical, nil
}

func decodeState(reader io.Reader) (StateFile, error) {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	var state StateFile
	if err := decoder.Decode(&state); err != nil {
		return StateFile{}, fmt.Errorf("%w: decode failed: %w", ErrStateCorrupt, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return StateFile{}, fmt.Errorf("%w: trailing content", ErrStateCorrupt)
	}
	if err := state.Validate(); err != nil {
		return StateFile{}, err
	}
	return state, nil
}

func validateLoadedState(state StateFile, opts StateLoadOptions) error {
	if opts.MinimumGeneration > 0 && state.Generation < opts.MinimumGeneration {
		return fmt.Errorf("%w: generation below expected minimum", ErrStateRollback)
	}
	if opts.ExpectedHash != "" && state.CurrentHash != opts.ExpectedHash {
		return fmt.Errorf("%w: current hash differs from expected hash", ErrStateRollback)
	}
	return nil
}

func validateStateHash(hash string) error {
	if !strings.HasPrefix(hash, stateHashPrefix) {
		return fmt.Errorf("%w: state hash prefix is invalid", ErrStateCorrupt)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(hash, stateHashPrefix))
	if err != nil || len(decoded) != sha256.Size {
		return fmt.Errorf("%w: state hash encoding is invalid", ErrStateCorrupt)
	}
	return nil
}

func validateStateFilePath(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("inspect registry state file: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: state file must not be a symlink", ErrStatePermission)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: state file must be regular", ErrStatePermission)
	}
	if info.Mode().Perm()&stateFileDisallowedMode != 0 {
		return fmt.Errorf("%w: state file mode must not include %04o", ErrStatePermission, stateFileDisallowedMode)
	}
	return validateStateParent(path)
}

func validateStateFileWritePath(path string) error {
	if err := validateStateParent(path); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("inspect registry state file: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: state file must not be a symlink", ErrStatePermission)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: state file must be regular", ErrStatePermission)
	}
	return nil
}

func validateStateParent(path string) error {
	parent := filepath.Dir(path)
	info, err := os.Lstat(parent)
	if err != nil {
		return fmt.Errorf("%w: parent directory must exist", ErrStatePermission)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: parent directory must not be a symlink", ErrStatePermission)
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: parent path must be a directory", ErrStatePermission)
	}
	if info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("%w: parent directory must not be group/world writable", ErrStatePermission)
	}
	return nil
}

func syncParentDir(path string) error {
	parent := filepath.Dir(path)
	// #nosec G304 -- parent path is derived from operator-controlled local state configuration.
	dir, err := os.Open(parent)
	if err != nil {
		return fmt.Errorf("open registry state parent directory: %w", err)
	}
	defer func() {
		_ = dir.Close()
	}()
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("sync registry state parent directory: %w", err)
	}
	return nil
}
