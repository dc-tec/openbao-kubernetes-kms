// Package aad builds and validates Kubernetes KMS annotations and Transit associated data.
package aad

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
)

const (
	// ProviderValue is the provider marker stored in KMS annotations and AAD.
	ProviderValue = "openbao-transit"
	// AADVersionV1 is the only supported AAD schema for v0.1 objects.
	AADVersionV1 = "v1"

	// KeyProvider is the KMS annotation key for the provider marker.
	KeyProvider = "provider.kms.openbao.org"
	// KeyKeyIDHash is the KMS annotation key for the hashed Kubernetes key_id.
	KeyKeyIDHash = "key-id-hash.kms.openbao.org"
	// KeyTransitKeyVersion is the KMS annotation key for the Transit key version.
	KeyTransitKeyVersion = "transit-key-version.kms.openbao.org"
	// KeyTransitMountHash is the KMS annotation key for the hashed Transit mount ID.
	KeyTransitMountHash = "transit-mount-hash.kms.openbao.org"
	// KeyTransitKeyHash is the KMS annotation key for the hashed Transit key lineage ID.
	KeyTransitKeyHash = "transit-key-hash.kms.openbao.org"
	// KeyPluginVersion is the KMS annotation key for the provider plugin version.
	KeyPluginVersion = "plugin-version.kms.openbao.org"
	// KeyAADVersion is the KMS annotation key for the AAD schema version.
	KeyAADVersion = "aad-version.kms.openbao.org"

	purposeValue = "kubernetes-etcd-kms-v2"
)

var (
	// ErrInvalidAnnotations identifies malformed or incomplete KMS annotation metadata.
	ErrInvalidAnnotations = errors.New("annotations invalid")
	// ErrAnnotationMismatch identifies annotations that do not match the key snapshot.
	ErrAnnotationMismatch = errors.New("annotations do not match key snapshot")
	// ErrAADRequired identifies snapshots that are not in the v0.1 required-AAD mode.
	ErrAADRequired = errors.New("AAD required")
)

// SnapshotLookup resolves Kubernetes key_id values before decrypt attempts reach Transit.
type SnapshotLookup interface {
	Lookup(keyID string) (keyregistry.KeySnapshot, error)
}

// ParsedAnnotations contains the validated values from the KMS annotation map.
type ParsedAnnotations struct {
	Provider          string
	KeyIDHash         string
	TransitKeyVersion int
	TransitMountHash  string
	TransitKeyHash    string
	PluginVersion     string
	AADVersion        string
}

type aadEnvelopeV1 struct {
	AADVersion          string `json:"aad_version"`
	Purpose             string `json:"purpose"`
	Provider            string `json:"provider"`
	ProviderName        string `json:"provider_name"`
	ClusterIDHash       string `json:"cluster_id_hash"`
	OpenBaoInstanceHash string `json:"openbao_instance_hash"`
	TransitMountHash    string `json:"transit_mount_hash"`
	TransitKeyHash      string `json:"transit_key_hash"`
	KeyIDHash           string `json:"key_id_hash"`
	KeyVersion          string `json:"key_version"`
}

// DecryptAAD contains the validated snapshot and AAD bytes needed for Transit decrypt.
type DecryptAAD struct {
	Snapshot              keyregistry.KeySnapshot
	Annotations           ParsedAnnotations
	Canonical             []byte
	TransitAssociatedData string
}

// BuildAnnotations returns the required non-secret KMS v2 annotations for a snapshot.
func BuildAnnotations(snapshot keyregistry.KeySnapshot, pluginVersion string) (map[string]string, error) {
	if pluginVersion == "" {
		return nil, fmt.Errorf("%w: plugin version is required", ErrInvalidAnnotations)
	}

	normalized, err := snapshot.Normalize()
	if err != nil {
		return nil, err
	}
	if err := RequireAADMode(normalized); err != nil {
		return nil, err
	}

	return map[string]string{
		KeyProvider:          ProviderValue,
		KeyKeyIDHash:         HashValue(normalized.KubernetesKeyID),
		KeyTransitKeyVersion: strconv.Itoa(normalized.TransitVersion),
		KeyTransitMountHash:  HashValue(normalized.TransitMountID),
		KeyTransitKeyHash:    HashValue(normalized.TransitKeyLineageID),
		KeyPluginVersion:     pluginVersion,
		KeyAADVersion:        AADVersionV1,
	}, nil
}

// ParseAnnotations validates the required KMS annotation keys and values.
func ParseAnnotations(annotations map[string]string) (ParsedAnnotations, error) {
	if len(annotations) == 0 {
		return ParsedAnnotations{}, fmt.Errorf("%w: required annotations are missing", ErrInvalidAnnotations)
	}
	if err := validateAnnotationKeys(annotations); err != nil {
		return ParsedAnnotations{}, err
	}

	provider, err := requiredValue(annotations, KeyProvider)
	if err != nil {
		return ParsedAnnotations{}, err
	}
	if provider != ProviderValue {
		return ParsedAnnotations{}, fmt.Errorf("%w: provider marker is invalid", ErrInvalidAnnotations)
	}

	keyIDHash, err := requiredHash(annotations, KeyKeyIDHash)
	if err != nil {
		return ParsedAnnotations{}, err
	}
	transitMountHash, err := requiredHash(annotations, KeyTransitMountHash)
	if err != nil {
		return ParsedAnnotations{}, err
	}
	transitKeyHash, err := requiredHash(annotations, KeyTransitKeyHash)
	if err != nil {
		return ParsedAnnotations{}, err
	}

	versionText, err := requiredValue(annotations, KeyTransitKeyVersion)
	if err != nil {
		return ParsedAnnotations{}, err
	}
	transitVersion, err := strconv.Atoi(versionText)
	if err != nil || transitVersion <= 0 {
		return ParsedAnnotations{}, fmt.Errorf("%w: transit key version is invalid", ErrInvalidAnnotations)
	}

	pluginVersion, err := requiredValue(annotations, KeyPluginVersion)
	if err != nil {
		return ParsedAnnotations{}, err
	}

	aadVersion, err := requiredValue(annotations, KeyAADVersion)
	if err != nil {
		return ParsedAnnotations{}, err
	}
	if aadVersion != AADVersionV1 {
		return ParsedAnnotations{}, fmt.Errorf("%w: AAD version is unsupported", ErrInvalidAnnotations)
	}

	return ParsedAnnotations{
		Provider:          provider,
		KeyIDHash:         keyIDHash,
		TransitKeyVersion: transitVersion,
		TransitMountHash:  transitMountHash,
		TransitKeyHash:    transitKeyHash,
		PluginVersion:     pluginVersion,
		AADVersion:        aadVersion,
	}, nil
}

// ValidateForSnapshot verifies that parsed annotations match the supplied key snapshot.
func ValidateForSnapshot(snapshot keyregistry.KeySnapshot, annotations ParsedAnnotations) error {
	normalized, err := snapshot.Normalize()
	if err != nil {
		return err
	}
	if err := RequireAADMode(normalized); err != nil {
		return err
	}

	if annotations.Provider != ProviderValue {
		return fmt.Errorf("%w: provider marker mismatch", ErrAnnotationMismatch)
	}
	if annotations.AADVersion != AADVersionV1 {
		return fmt.Errorf("%w: AAD version mismatch", ErrAnnotationMismatch)
	}
	if annotations.TransitKeyVersion != normalized.TransitVersion {
		return fmt.Errorf("%w: transit key version mismatch", ErrAnnotationMismatch)
	}
	if annotations.KeyIDHash != HashValue(normalized.KubernetesKeyID) {
		return fmt.Errorf("%w: key_id hash mismatch", ErrAnnotationMismatch)
	}
	if annotations.TransitMountHash != HashValue(normalized.TransitMountID) {
		return fmt.Errorf("%w: transit mount hash mismatch", ErrAnnotationMismatch)
	}
	if annotations.TransitKeyHash != HashValue(normalized.TransitKeyLineageID) {
		return fmt.Errorf("%w: transit key hash mismatch", ErrAnnotationMismatch)
	}

	return nil
}

// BuildCanonical returns the canonical AAD JSON bytes for a snapshot and annotation map.
func BuildCanonical(snapshot keyregistry.KeySnapshot, annotations map[string]string) ([]byte, error) {
	parsed, err := ParseAnnotations(annotations)
	if err != nil {
		return nil, err
	}
	if err := ValidateForSnapshot(snapshot, parsed); err != nil {
		return nil, err
	}

	return buildCanonicalFromParsed(snapshot, parsed)
}

// PrepareDecrypt validates key_id, registry membership, annotations, and AAD before Transit decrypt.
func PrepareDecrypt(registry SnapshotLookup, keyID string, annotations map[string]string) (DecryptAAD, error) {
	snapshot, err := registry.Lookup(keyID)
	if err != nil {
		return DecryptAAD{}, err
	}
	if err := RequireAADMode(snapshot); err != nil {
		return DecryptAAD{}, err
	}

	parsed, err := ParseAnnotations(annotations)
	if err != nil {
		return DecryptAAD{}, err
	}
	if err := ValidateForSnapshot(snapshot, parsed); err != nil {
		return DecryptAAD{}, err
	}

	canonical, err := buildCanonicalFromParsed(snapshot, parsed)
	if err != nil {
		return DecryptAAD{}, err
	}

	return DecryptAAD{
		Snapshot:              snapshot,
		Annotations:           parsed,
		Canonical:             canonical,
		TransitAssociatedData: EncodeForTransit(canonical),
	}, nil
}

// RequireAADMode enforces the v0.1 policy that all snapshots use associated data.
func RequireAADMode(snapshot keyregistry.KeySnapshot) error {
	if snapshot.AADMode != keyregistry.AADModeRequired {
		return fmt.Errorf("%w: snapshot mode %q is not supported", ErrAADRequired, snapshot.AADMode)
	}
	return nil
}

func buildCanonicalFromParsed(snapshot keyregistry.KeySnapshot, parsed ParsedAnnotations) ([]byte, error) {
	normalized, err := snapshot.Normalize()
	if err != nil {
		return nil, err
	}

	envelope := aadEnvelopeV1{
		AADVersion:          AADVersionV1,
		Purpose:             purposeValue,
		Provider:            ProviderValue,
		ProviderName:        normalized.ProviderName,
		ClusterIDHash:       HashValue(normalized.ClusterID),
		OpenBaoInstanceHash: HashValue(normalized.OpenBaoInstanceID),
		TransitMountHash:    parsed.TransitMountHash,
		TransitKeyHash:      parsed.TransitKeyHash,
		KeyIDHash:           parsed.KeyIDHash,
		KeyVersion:          strconv.Itoa(normalized.TransitVersion),
	}

	canonical, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("marshal canonical AAD: %w", err)
	}
	return canonical, nil
}

// EncodeForTransit returns the OpenBao Transit API associated_data value for canonical AAD bytes.
func EncodeForTransit(canonicalAAD []byte) string {
	return base64.StdEncoding.EncodeToString(canonicalAAD)
}

// HashValue returns the base64url-encoded SHA-256 digest used in annotations and AAD.
func HashValue(value string) string {
	sum := sha256.Sum256([]byte(value))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func validateAnnotationKeys(annotations map[string]string) error {
	for key := range annotations {
		if !isFQDNAnnotationKey(key) {
			return fmt.Errorf("%w: annotation key %q is not fully qualified", ErrInvalidAnnotations, key)
		}
	}
	return nil
}

func isFQDNAnnotationKey(key string) bool {
	if key == "" || len(key) > 253 || strings.Contains(key, "/") || !strings.Contains(key, ".") {
		return false
	}
	labels := strings.SplitSeq(key, ".")
	for label := range labels {
		if !isDNS1123Label(label) {
			return false
		}
	}
	return true
}

func isDNS1123Label(label string) bool {
	if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
		return false
	}
	for _, r := range label {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return false
	}
	return true
}

func requiredValue(annotations map[string]string, key string) (string, error) {
	value, ok := annotations[key]
	if !ok || value == "" {
		return "", fmt.Errorf("%w: %s is required", ErrInvalidAnnotations, key)
	}
	return value, nil
}

func requiredHash(annotations map[string]string, key string) (string, error) {
	value, err := requiredValue(annotations, key)
	if err != nil {
		return "", err
	}
	if err := validateHashValue(value); err != nil {
		return "", fmt.Errorf("%w: %s is invalid", ErrInvalidAnnotations, key)
	}
	return value, nil
}

func validateHashValue(value string) error {
	if len(value) != 43 {
		return ErrInvalidAnnotations
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return ErrInvalidAnnotations
	}
	if len(decoded) != sha256.Size {
		return ErrInvalidAnnotations
	}
	return nil
}
