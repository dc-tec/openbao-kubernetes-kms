// Package keyregistry owns Kubernetes key_id derivation and local snapshot lookup.
package keyregistry

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	keyIDDomain = "openbao-kubernetes-kms/key-id/v1"
	keyIDPrefix = "obk2."

	sha256RawURLLength = 43
	keyIDLength        = len(keyIDPrefix) + sha256RawURLLength
)

var (
	// ErrMalformedKeyID identifies key_id values that do not match the wire format.
	ErrMalformedKeyID = errors.New("key_id malformed")
	// ErrUnknownKeyID identifies well-formed key_id values that are not in the registry.
	ErrUnknownKeyID = errors.New("key_id unknown")
)

// SnapshotState is the local lifecycle state for a Transit-backed Kubernetes key snapshot.
type SnapshotState string

const (
	// StateActive marks the snapshot currently exposed through KMS Status and Encrypt.
	StateActive SnapshotState = "active"
	// StatePending marks a snapshot observed during rotation but not yet promoted.
	StatePending SnapshotState = "pending"
	// StateRetired marks a historical snapshot retained for decrypt.
	StateRetired SnapshotState = "retired"
	// StateRejected marks a snapshot rejected by validation or rollback checks.
	StateRejected SnapshotState = "rejected"
	// StateDisasterRecovery marks a snapshot accepted only under explicit recovery handling.
	StateDisasterRecovery SnapshotState = "disaster_recovery"
)

// Valid reports whether the state is one of the recognized snapshot states.
func (s SnapshotState) Valid() bool {
	switch s {
	case StateActive, StatePending, StateRetired, StateRejected, StateDisasterRecovery:
		return true
	default:
		return false
	}
}

// AADMode records how associated data is expected for a snapshot epoch.
type AADMode string

const (
	// AADModeRequired requires valid AAD annotations for encrypt and decrypt.
	AADModeRequired AADMode = "aad.required"
	// AADModeOptionalRead is reserved for future bounded pre-AAD compatibility reads.
	AADModeOptionalRead AADMode = "aad.optional-read"
	// AADModeDisabled is reserved for compatibility testing only.
	AADModeDisabled AADMode = "aad.disabled"
)

// Valid reports whether the mode is one of the recognized AAD compatibility modes.
func (m AADMode) Valid() bool {
	switch m {
	case AADModeRequired, AADModeOptionalRead, AADModeDisabled:
		return true
	default:
		return false
	}
}

// KeySnapshot is the non-secret identity and Transit metadata used to derive a Kubernetes key_id.
type KeySnapshot struct {
	ProviderName            string
	ClusterID               string
	OpenBaoInstanceID       string
	TransitMountID          string
	TransitKeyLineageID     string
	TransitVersion          int
	TransitVersionCreatedAt time.Time
	KeyEpoch                string
	KubernetesKeyID         string
	State                   SnapshotState
	AADMode                 AADMode
}

// DeriveKeyID returns the opaque Kubernetes key_id for a snapshot.
func DeriveKeyID(snapshot KeySnapshot) (string, error) {
	if err := validateSnapshotIdentity(snapshot); err != nil {
		return "", err
	}

	input := make([]byte, 0, 256)
	input = appendPart(input, keyIDDomain)
	input = appendPart(input, snapshot.ProviderName)
	input = appendPart(input, snapshot.ClusterID)
	input = appendPart(input, snapshot.OpenBaoInstanceID)
	input = appendPart(input, snapshot.TransitMountID)
	input = appendPart(input, snapshot.TransitKeyLineageID)
	input = appendPart(input, strconv.Itoa(snapshot.TransitVersion))
	input = appendPart(input, strconv.FormatInt(snapshot.TransitVersionCreatedAt.Unix(), 10))
	input = appendPart(input, snapshot.KeyEpoch)

	sum := sha256.Sum256(input)
	return keyIDPrefix + base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

// ParseKeyID validates and normalizes a Kubernetes key_id value.
func ParseKeyID(keyID string) (string, error) {
	if len(keyID) != keyIDLength {
		return "", ErrMalformedKeyID
	}
	if !strings.HasPrefix(keyID, keyIDPrefix) {
		return "", ErrMalformedKeyID
	}

	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(keyID, keyIDPrefix))
	if err != nil {
		return "", ErrMalformedKeyID
	}
	if len(decoded) != sha256.Size {
		return "", ErrMalformedKeyID
	}

	return keyID, nil
}

// Normalize derives or verifies the embedded Kubernetes key_id and validates typed state.
func (s KeySnapshot) Normalize() (KeySnapshot, error) {
	if !s.State.Valid() {
		return KeySnapshot{}, fmt.Errorf("snapshot state %q is invalid", s.State)
	}
	if !s.AADMode.Valid() {
		return KeySnapshot{}, fmt.Errorf("snapshot AAD mode %q is invalid", s.AADMode)
	}

	derived, err := DeriveKeyID(s)
	if err != nil {
		return KeySnapshot{}, err
	}
	if s.KubernetesKeyID != "" {
		parsed, parseErr := ParseKeyID(s.KubernetesKeyID)
		if parseErr != nil {
			return KeySnapshot{}, parseErr
		}
		if parsed != derived {
			return KeySnapshot{}, fmt.Errorf("snapshot key_id does not match derived key_id")
		}
		return s, nil
	}

	s.KubernetesKeyID = derived
	return s, nil
}

// Registry provides active and historical key lookup before any Transit decrypt is attempted.
type Registry struct {
	activeKeyID string
	snapshots   map[string]KeySnapshot
}

// NewRegistry builds an in-memory registry from one active snapshot and optional historical snapshots.
func NewRegistry(active KeySnapshot, historical []KeySnapshot) (Registry, error) {
	active, err := active.Normalize()
	if err != nil {
		return Registry{}, err
	}
	if active.State != StateActive {
		return Registry{}, fmt.Errorf("active snapshot state must be %q", StateActive)
	}

	registry := Registry{
		activeKeyID: active.KubernetesKeyID,
		snapshots:   make(map[string]KeySnapshot, len(historical)+1),
	}
	if err := registry.insert(active); err != nil {
		return Registry{}, err
	}

	for _, snapshot := range historical {
		normalized, normalizeErr := snapshot.Normalize()
		if normalizeErr != nil {
			return Registry{}, normalizeErr
		}
		if insertErr := registry.insert(normalized); insertErr != nil {
			return Registry{}, insertErr
		}
	}

	return registry, nil
}

// Active returns the active snapshot if the registry has one.
func (r Registry) Active() (KeySnapshot, bool) {
	snapshot, ok := r.snapshots[r.activeKeyID]
	return snapshot, ok
}

// Lookup validates key_id syntax and returns the matching snapshot or ErrUnknownKeyID.
func (r Registry) Lookup(keyID string) (KeySnapshot, error) {
	parsed, err := ParseKeyID(keyID)
	if err != nil {
		return KeySnapshot{}, err
	}
	snapshot, ok := r.snapshots[parsed]
	if !ok {
		return KeySnapshot{}, ErrUnknownKeyID
	}
	return snapshot, nil
}

func (r Registry) insert(snapshot KeySnapshot) error {
	if existing, ok := r.snapshots[snapshot.KubernetesKeyID]; ok && existing != snapshot {
		return fmt.Errorf("duplicate key_id with different snapshot metadata")
	}
	r.snapshots[snapshot.KubernetesKeyID] = snapshot
	return nil
}

func validateSnapshotIdentity(snapshot KeySnapshot) error {
	required := []struct {
		name  string
		value string
	}{
		{name: "provider name", value: snapshot.ProviderName},
		{name: "cluster ID", value: snapshot.ClusterID},
		{name: "OpenBao instance ID", value: snapshot.OpenBaoInstanceID},
		{name: "Transit mount ID", value: snapshot.TransitMountID},
		{name: "Transit key lineage ID", value: snapshot.TransitKeyLineageID},
	}
	for _, field := range required {
		if field.value == "" {
			return fmt.Errorf("%s is required for key_id derivation", field.name)
		}
	}
	if snapshot.TransitVersion <= 0 {
		return fmt.Errorf("transit version must be positive")
	}
	if snapshot.TransitVersionCreatedAt.IsZero() {
		return fmt.Errorf("transit version creation time is required")
	}
	return nil
}

func appendPart(input []byte, value string) []byte {
	input = append(input, value...)
	return append(input, 0)
}
