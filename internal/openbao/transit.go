package openbao

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	transitOperationMetadataRead      = "transit metadata read"
	transitOperationDisableUpsertRead = "transit disable_upsert read"
	transitOperationEncrypt           = "transit encrypt"
	transitOperationDecrypt           = "transit decrypt"
	transitOperationBatchDecrypt      = "transit batch decrypt"
	transitOperationCapabilitiesSelf  = "capabilities self"

	transitPathSegmentKeys    = "keys"
	transitPathSegmentConfig  = "config"
	transitPathSegmentEncrypt = "encrypt"
	transitPathSegmentDecrypt = "decrypt"
	capabilitiesSelfPath      = "sys/capabilities-self"

	// TransitKeyTypeAES256GCM96 is the only Transit key type supported by this release line.
	TransitKeyTypeAES256GCM96 = "aes256-gcm96"

	findingCodeUnsupportedType          = "unsupported_type"
	findingCodeExportable               = "exportable"
	findingCodePlaintextBackup          = "plaintext_backup"
	findingCodeDeletionAllowed          = "deletion_allowed"
	findingCodeDerived                  = "derived"
	findingCodeConvergent               = "convergent"
	findingCodeMinEncryptionVersion     = "min_encryption_version"
	findingCodeMinDecryptionVersion     = "min_decryption_version"
	findingCodeEncryptionUnsupported    = "encryption_unsupported"
	findingCodeDecryptionUnsupported    = "decryption_unsupported"
	findingMessageUnsupportedType       = "key type is not aes256-gcm96"
	findingMessageExportable            = "key material export is enabled"
	findingMessagePlaintextBackup       = "plaintext backup is enabled"
	findingMessageDeletionAllowed       = "key deletion is allowed"
	findingMessageDerived               = "derived key mode is enabled"
	findingMessageConvergent            = "convergent encryption is enabled"
	findingMessageMinEncryptionVersion  = "minimum encryption version blocks latest version"
	findingMessageMinDecryptionVersion  = "minimum decryption version exceeds latest version"
	findingMessageEncryptionUnsupported = "key does not support encryption"
	findingMessageDecryptionUnsupported = "key does not support decryption"
)

// KeyProfileFindingSeverity identifies whether a Transit profile finding blocks runtime use.
type KeyProfileFindingSeverity string

const (
	// KeyProfileFindingSeverityBlocking means the provider fails closed while the finding is present.
	KeyProfileFindingSeverityBlocking KeyProfileFindingSeverity = "blocking"
	// KeyProfileFindingSeverityWarning is reserved for non-blocking operator warnings.
	KeyProfileFindingSeverityWarning KeyProfileFindingSeverity = "warning"
)

// KeyProfileFindingImpact identifies the operator-facing impact of a Transit profile finding.
type KeyProfileFindingImpact string

const (
	// KeyProfileFindingImpactCryptographicSafety covers settings that weaken the key or AAD contract.
	KeyProfileFindingImpactCryptographicSafety KeyProfileFindingImpact = "cryptographic_safety"
	// KeyProfileFindingImpactAvailability covers settings that can make KMS operations unavailable.
	KeyProfileFindingImpactAvailability KeyProfileFindingImpact = "api_server_availability"
)

// TransitClient is the narrow OpenBao Transit surface needed by the KMS provider.
type TransitClient interface {
	ReadKeyProfile(context.Context, string, string) (KeyProfile, error)
	ReadDisableUpsert(context.Context, string) (bool, error)
	Encrypt(context.Context, EncryptRequest) (EncryptResponse, error)
	Decrypt(context.Context, DecryptRequest) (DecryptResponse, error)
	BatchDecrypt(context.Context, BatchDecryptRequest) (BatchDecryptResponse, error)
	Capabilities(context.Context, []string) (CapabilitiesResult, error)
	ProbeEncryptDecrypt(context.Context, ProbeRequest) (ProbeResult, error)
}

// KeyProfile is parsed OpenBao Transit key metadata.
type KeyProfile struct {
	Name                 string
	Type                 string
	LatestVersion        int
	MinAvailableVersion  int
	MinEncryptionVersion int
	MinDecryptionVersion int
	VersionCreationTimes []KeyVersion
	Derived              bool
	ConvergentEncryption bool
	Exportable           bool
	AllowPlaintextBackup bool
	DeletionAllowed      bool
	ImportedKey          bool
	SoftDeleted          bool
	SupportsEncryption   bool
	SupportsDecryption   bool
	SupportsDerivation   bool
	SupportsSigning      bool
}

// KeyVersion records one Transit key version creation time.
type KeyVersion struct {
	Version   int
	CreatedAt time.Time
}

// KeyProfileFinding describes a policy-relevant Transit key profile issue.
type KeyProfileFinding struct {
	Code     string
	Message  string
	Impact   KeyProfileFindingImpact
	Severity KeyProfileFindingSeverity
}

// AssessKeyProfile returns policy findings for dangerous Transit key settings.
func AssessKeyProfile(profile KeyProfile) []KeyProfileFinding {
	findings := make([]KeyProfileFinding, 0)
	if profile.Type != TransitKeyTypeAES256GCM96 {
		findings = append(findings, blockingProfileFinding(
			findingCodeUnsupportedType,
			findingMessageUnsupportedType,
			KeyProfileFindingImpactCryptographicSafety,
		))
	}
	if profile.Exportable {
		findings = append(findings, blockingProfileFinding(
			findingCodeExportable,
			findingMessageExportable,
			KeyProfileFindingImpactCryptographicSafety,
		))
	}
	if profile.AllowPlaintextBackup {
		findings = append(findings, blockingProfileFinding(
			findingCodePlaintextBackup,
			findingMessagePlaintextBackup,
			KeyProfileFindingImpactCryptographicSafety,
		))
	}
	if profile.DeletionAllowed {
		findings = append(findings, blockingProfileFinding(
			findingCodeDeletionAllowed,
			findingMessageDeletionAllowed,
			KeyProfileFindingImpactAvailability,
		))
	}
	if profile.Derived {
		findings = append(findings, blockingProfileFinding(
			findingCodeDerived,
			findingMessageDerived,
			KeyProfileFindingImpactCryptographicSafety,
		))
	}
	if profile.ConvergentEncryption {
		findings = append(findings, blockingProfileFinding(
			findingCodeConvergent,
			findingMessageConvergent,
			KeyProfileFindingImpactCryptographicSafety,
		))
	}
	if profile.MinEncryptionVersion > 0 && profile.MinEncryptionVersion > profile.LatestVersion {
		findings = append(findings, blockingProfileFinding(
			findingCodeMinEncryptionVersion,
			findingMessageMinEncryptionVersion,
			KeyProfileFindingImpactAvailability,
		))
	}
	if profile.MinDecryptionVersion > profile.LatestVersion {
		findings = append(findings, blockingProfileFinding(
			findingCodeMinDecryptionVersion,
			findingMessageMinDecryptionVersion,
			KeyProfileFindingImpactAvailability,
		))
	}
	if !profile.SupportsEncryption {
		findings = append(findings, blockingProfileFinding(
			findingCodeEncryptionUnsupported,
			findingMessageEncryptionUnsupported,
			KeyProfileFindingImpactAvailability,
		))
	}
	if !profile.SupportsDecryption {
		findings = append(findings, blockingProfileFinding(
			findingCodeDecryptionUnsupported,
			findingMessageDecryptionUnsupported,
			KeyProfileFindingImpactAvailability,
		))
	}
	return findings
}

// BlockingKeyProfileFindings returns the findings that make the provider fail closed.
func BlockingKeyProfileFindings(findings []KeyProfileFinding) []KeyProfileFinding {
	blocking := make([]KeyProfileFinding, 0, len(findings))
	for _, finding := range findings {
		if finding.Severity == "" || finding.Severity == KeyProfileFindingSeverityBlocking {
			blocking = append(blocking, finding)
		}
	}
	return blocking
}

// FormatKeyProfileFindings renders bounded, non-secret findings for diagnostics.
func FormatKeyProfileFindings(findings []KeyProfileFinding) string {
	parts := make([]string, 0, len(findings))
	for _, finding := range findings {
		impact := string(finding.Impact)
		if impact == "" {
			impact = "unspecified"
		}
		severity := string(finding.Severity)
		if severity == "" {
			severity = string(KeyProfileFindingSeverityBlocking)
		}
		parts = append(parts, fmt.Sprintf("%s/%s: %s", severity, impact, finding.Message))
	}
	return strings.Join(parts, "; ")
}

func blockingProfileFinding(
	code string,
	message string,
	impact KeyProfileFindingImpact,
) KeyProfileFinding {
	return KeyProfileFinding{
		Code:     code,
		Message:  message,
		Impact:   impact,
		Severity: KeyProfileFindingSeverityBlocking,
	}
}

// EncryptRequest is an OpenBao Transit encrypt request with explicit key version.
type EncryptRequest struct {
	MountPath      string
	KeyName        string
	Plaintext      []byte
	AssociatedData []byte
	KeyVersion     int
}

// EncryptResponse is the parsed OpenBao Transit encrypt response.
type EncryptResponse struct {
	Ciphertext string
	KeyVersion int
}

// DecryptRequest is an OpenBao Transit decrypt request.
type DecryptRequest struct {
	MountPath      string
	KeyName        string
	Ciphertext     string
	AssociatedData []byte
}

// DecryptResponse is the parsed OpenBao Transit decrypt response.
type DecryptResponse struct {
	Plaintext []byte
}

// BatchDecryptRequest is a direct Transit batch decrypt request.
type BatchDecryptRequest struct {
	MountPath string
	KeyName   string
	Items     []BatchDecryptItem
}

// BatchDecryptItem is one Transit batch decrypt item.
type BatchDecryptItem struct {
	Ciphertext     string
	AssociatedData []byte
	Reference      string
}

// BatchDecryptResponse is a direct Transit batch decrypt response.
type BatchDecryptResponse struct {
	Results []BatchDecryptResult
}

// BatchDecryptResult is one Transit batch decrypt result.
type BatchDecryptResult struct {
	Plaintext []byte
	Reference string
	Error     string
}

// CapabilitiesResult maps OpenBao paths to policy capabilities.
type CapabilitiesResult struct {
	ByPath map[string][]string
}

// ProbeRequest controls a non-secret Transit encrypt/decrypt probe.
type ProbeRequest struct {
	MountPath      string
	KeyName        string
	KeyVersion     int
	AssociatedData []byte
}

// ProbeResult reports safe metadata from a non-secret Transit encrypt/decrypt probe.
type ProbeResult struct {
	Ciphertext []byte
	KeyVersion int
}

// ReadKeyProfile reads and parses Transit key metadata.
func (c *Client) ReadKeyProfile(ctx context.Context, mountPath string, keyName string) (KeyProfile, error) {
	var response keyProfileResponse
	if err := c.do(
		ctx,
		transitOperationMetadataRead,
		http.MethodGet,
		transitPath(mountPath, transitPathSegmentKeys, keyName),
		nil,
		&response,
	); err != nil {
		return KeyProfile{}, err
	}
	return response.Data.profile()
}

// ReadDisableUpsert reads Transit global key creation policy for a mount.
func (c *Client) ReadDisableUpsert(ctx context.Context, mountPath string) (bool, error) {
	var response disableUpsertResponse
	if err := c.do(
		ctx,
		transitOperationDisableUpsertRead,
		http.MethodGet,
		transitConfigPath(mountPath),
		nil,
		&response,
	); err != nil {
		return false, err
	}
	return response.Data.DisableUpsert, nil
}

// Encrypt encrypts plaintext with explicit Transit key_version.
func (c *Client) Encrypt(ctx context.Context, req EncryptRequest) (EncryptResponse, error) {
	if req.KeyVersion <= 0 {
		return EncryptResponse{}, fmt.Errorf("transit encrypt key_version must be explicit and positive")
	}
	if len(req.Plaintext) == 0 {
		return EncryptResponse{}, fmt.Errorf("transit encrypt plaintext is required")
	}
	if err := requireAssociatedData(req.AssociatedData); err != nil {
		return EncryptResponse{}, err
	}
	body := encryptRequestBody{
		Plaintext:      base64.StdEncoding.EncodeToString(req.Plaintext),
		AssociatedData: base64.StdEncoding.EncodeToString(req.AssociatedData),
		KeyVersion:     req.KeyVersion,
	}
	var response encryptResponseBody
	if err := c.do(
		ctx,
		transitOperationEncrypt,
		http.MethodPost,
		transitPath(req.MountPath, transitPathSegmentEncrypt, req.KeyName),
		body,
		&response,
	); err != nil {
		return EncryptResponse{}, err
	}
	return EncryptResponse{
		Ciphertext: response.Data.Ciphertext,
		KeyVersion: response.Data.KeyVersion,
	}, nil
}

// Decrypt decrypts one Transit ciphertext with required associated data.
func (c *Client) Decrypt(ctx context.Context, req DecryptRequest) (DecryptResponse, error) {
	if req.Ciphertext == "" {
		return DecryptResponse{}, fmt.Errorf("transit decrypt ciphertext is required")
	}
	if err := requireAssociatedData(req.AssociatedData); err != nil {
		return DecryptResponse{}, err
	}
	body := decryptRequestBody{
		Ciphertext:     req.Ciphertext,
		AssociatedData: base64.StdEncoding.EncodeToString(req.AssociatedData),
	}
	var response decryptResponseBody
	if err := c.do(
		ctx,
		transitOperationDecrypt,
		http.MethodPost,
		transitPath(req.MountPath, transitPathSegmentDecrypt, req.KeyName),
		body,
		&response,
	); err != nil {
		return DecryptResponse{}, err
	}
	plaintext, err := base64.StdEncoding.DecodeString(response.Data.Plaintext)
	if err != nil {
		return DecryptResponse{}, fmt.Errorf("decode Transit plaintext: %w", err)
	}
	return DecryptResponse{Plaintext: plaintext}, nil
}

// BatchDecrypt decrypts multiple Transit ciphertexts while preserving per-item AAD and references.
func (c *Client) BatchDecrypt(ctx context.Context, req BatchDecryptRequest) (BatchDecryptResponse, error) {
	if len(req.Items) == 0 {
		return BatchDecryptResponse{}, fmt.Errorf("batch decrypt requires at least one item")
	}
	items := make([]batchDecryptRequestItem, 0, len(req.Items))
	for _, item := range req.Items {
		if item.Ciphertext == "" {
			return BatchDecryptResponse{}, fmt.Errorf("batch decrypt ciphertext is required")
		}
		if err := requireAssociatedData(item.AssociatedData); err != nil {
			return BatchDecryptResponse{}, err
		}
		items = append(items, batchDecryptRequestItem{
			Ciphertext:     item.Ciphertext,
			AssociatedData: base64.StdEncoding.EncodeToString(item.AssociatedData),
			Reference:      item.Reference,
		})
	}

	body := batchDecryptRequestBody{BatchInput: items}
	var response batchDecryptResponseBody
	if err := c.do(
		ctx,
		transitOperationBatchDecrypt,
		http.MethodPost,
		transitPath(req.MountPath, transitPathSegmentDecrypt, req.KeyName),
		body,
		&response,
	); err != nil {
		return BatchDecryptResponse{}, err
	}
	results := make([]BatchDecryptResult, 0, len(response.Data.BatchResults))
	for _, result := range response.Data.BatchResults {
		plaintext, err := decodeOptionalPlaintext(result.Plaintext)
		if err != nil {
			return BatchDecryptResponse{}, err
		}
		results = append(results, BatchDecryptResult{
			Plaintext: plaintext,
			Reference: result.Reference,
			Error:     result.Error,
		})
	}
	return BatchDecryptResponse{Results: results}, nil
}

// Capabilities reads the current token's capabilities for diagnostic paths.
func (c *Client) Capabilities(ctx context.Context, paths []string) (CapabilitiesResult, error) {
	if len(paths) == 0 {
		return CapabilitiesResult{}, fmt.Errorf("capability paths are required")
	}
	body := capabilitiesRequestBody{Paths: paths}
	var response capabilitiesResponseBody
	if err := c.do(
		ctx,
		transitOperationCapabilitiesSelf,
		http.MethodPost,
		capabilitiesSelfPath,
		body,
		&response,
	); err != nil {
		return CapabilitiesResult{}, err
	}
	result := make(map[string][]string, len(paths))
	for _, capabilityPath := range paths {
		result[capabilityPath] = slices.Clone(response.Data[capabilityPath])
	}
	return CapabilitiesResult{ByPath: result}, nil
}

// ProbeEncryptDecrypt performs a non-secret random Transit round trip.
func (c *Client) ProbeEncryptDecrypt(ctx context.Context, req ProbeRequest) (ProbeResult, error) {
	plaintext := make([]byte, 32)
	if _, err := rand.Read(plaintext); err != nil {
		return ProbeResult{}, fmt.Errorf("generate probe plaintext: %w", err)
	}
	encrypted, err := c.Encrypt(ctx, EncryptRequest{
		MountPath:      req.MountPath,
		KeyName:        req.KeyName,
		Plaintext:      plaintext,
		AssociatedData: req.AssociatedData,
		KeyVersion:     req.KeyVersion,
	})
	if err != nil {
		return ProbeResult{}, err
	}
	decrypted, err := c.Decrypt(ctx, DecryptRequest{
		MountPath:      req.MountPath,
		KeyName:        req.KeyName,
		Ciphertext:     encrypted.Ciphertext,
		AssociatedData: req.AssociatedData,
	})
	if err != nil {
		return ProbeResult{}, err
	}
	if !bytes.Equal(decrypted.Plaintext, plaintext) {
		return ProbeResult{}, fmt.Errorf("probe decrypt did not return original plaintext")
	}
	return ProbeResult{
		Ciphertext: []byte(encrypted.Ciphertext),
		KeyVersion: encrypted.KeyVersion,
	}, nil
}

type keyProfileResponse struct {
	responseMetadata
	Data keyProfileData `json:"data"`
}

func (*keyProfileResponse) responsePayload() {}

type keyProfileData struct {
	AllowPlaintextBackup bool             `json:"allow_plaintext_backup"`
	AutoRotatePeriod     int              `json:"auto_rotate_period"`
	DeletionAllowed      bool             `json:"deletion_allowed"`
	Derived              bool             `json:"derived"`
	Exportable           bool             `json:"exportable"`
	ImportedKey          bool             `json:"imported_key"`
	Keys                 map[string]int64 `json:"keys"`
	LatestVersion        int              `json:"latest_version"`
	MinAvailableVersion  int              `json:"min_available_version"`
	MinDecryptionVersion int              `json:"min_decryption_version"`
	MinEncryptionVersion int              `json:"min_encryption_version"`
	Name                 string           `json:"name"`
	SoftDeleted          bool             `json:"soft_deleted"`
	SupportsDecryption   bool             `json:"supports_decryption"`
	SupportsDerivation   bool             `json:"supports_derivation"`
	SupportsEncryption   bool             `json:"supports_encryption"`
	SupportsSigning      bool             `json:"supports_signing"`
	Type                 string           `json:"type"`
	ConvergentEncryption bool             `json:"convergent_encryption"`
}

func (d keyProfileData) profile() (KeyProfile, error) {
	versions := make([]KeyVersion, 0, len(d.Keys))
	for versionText, createdAtUnix := range d.Keys {
		version, err := strconv.Atoi(versionText)
		if err != nil {
			return KeyProfile{}, fmt.Errorf("parse Transit key version %q: %w", versionText, err)
		}
		versions = append(versions, KeyVersion{
			Version:   version,
			CreatedAt: time.Unix(createdAtUnix, 0).UTC(),
		})
	}
	return KeyProfile{
		Name:                 d.Name,
		Type:                 d.Type,
		LatestVersion:        d.LatestVersion,
		MinAvailableVersion:  d.MinAvailableVersion,
		MinEncryptionVersion: d.MinEncryptionVersion,
		MinDecryptionVersion: d.MinDecryptionVersion,
		VersionCreationTimes: versions,
		Derived:              d.Derived,
		ConvergentEncryption: d.ConvergentEncryption,
		Exportable:           d.Exportable,
		AllowPlaintextBackup: d.AllowPlaintextBackup,
		DeletionAllowed:      d.DeletionAllowed,
		ImportedKey:          d.ImportedKey,
		SoftDeleted:          d.SoftDeleted,
		SupportsEncryption:   d.SupportsEncryption,
		SupportsDecryption:   d.SupportsDecryption,
		SupportsDerivation:   d.SupportsDerivation,
		SupportsSigning:      d.SupportsSigning,
	}, nil
}

type disableUpsertResponse struct {
	responseMetadata
	Data disableUpsertData `json:"data"`
}

func (*disableUpsertResponse) responsePayload() {}

type disableUpsertData struct {
	DisableUpsert bool `json:"disable_upsert"`
}

type encryptRequestBody struct {
	Plaintext      string `json:"plaintext"`
	AssociatedData string `json:"associated_data"`
	KeyVersion     int    `json:"key_version"`
}

func (encryptRequestBody) requestPayload() {}

type encryptResponseBody struct {
	responseMetadata
	Data encryptResponseData `json:"data"`
}

func (*encryptResponseBody) responsePayload() {}

type encryptResponseData struct {
	Ciphertext string `json:"ciphertext"`
	KeyVersion int    `json:"key_version"`
}

type decryptRequestBody struct {
	Ciphertext     string `json:"ciphertext"`
	AssociatedData string `json:"associated_data"`
}

func (decryptRequestBody) requestPayload() {}

type decryptResponseBody struct {
	responseMetadata
	Data decryptResponseData `json:"data"`
}

func (*decryptResponseBody) responsePayload() {}

type decryptResponseData struct {
	Plaintext string `json:"plaintext"`
}

type batchDecryptRequestBody struct {
	BatchInput []batchDecryptRequestItem `json:"batch_input"`
}

func (batchDecryptRequestBody) requestPayload() {}

type batchDecryptRequestItem struct {
	Ciphertext     string `json:"ciphertext"`
	AssociatedData string `json:"associated_data"`
	Reference      string `json:"reference,omitempty"`
}

type batchDecryptResponseBody struct {
	responseMetadata
	Data batchDecryptResponseData `json:"data"`
}

func (*batchDecryptResponseBody) responsePayload() {}

type batchDecryptResponseData struct {
	BatchResults []batchDecryptResponseItem `json:"batch_results"`
}

type batchDecryptResponseItem struct {
	Plaintext string `json:"plaintext"`
	Reference string `json:"reference"`
	Error     string `json:"error"`
}

type capabilitiesRequestBody struct {
	Paths []string `json:"paths"`
}

func (capabilitiesRequestBody) requestPayload() {}

type capabilitiesResponseBody struct {
	responseMetadata
	Data map[string][]string `json:"data"`
}

func (*capabilitiesResponseBody) responsePayload() {}

func decodeOptionalPlaintext(value string) ([]byte, error) {
	if value == "" {
		return nil, nil
	}
	plaintext, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("decode Transit batch plaintext: %w", err)
	}
	return plaintext, nil
}

func requireAssociatedData(value []byte) error {
	if len(value) == 0 {
		return fmt.Errorf("transit associated data is required")
	}
	return nil
}
