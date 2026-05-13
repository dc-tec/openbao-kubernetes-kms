package config

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	configFingerprintDomain = "openbao-kubernetes-kms/config-identity/v1"

	configFileDisallowedMode = os.FileMode(0o037)
	jwtFileDisallowedMode    = os.FileMode(0o037)
	certFileDisallowedMode   = os.FileMode(0o037)
	pinFileDisallowedMode    = os.FileMode(0o037)
	caFileDisallowedMode     = os.FileMode(0o022)
	socketDisallowedMode     = os.FileMode(0o117)
	maxDebugCorrelationTTL   = time.Hour
	maxIncidentIDLength      = 64

	authMethodJWT  = "jwt"
	authMethodCert = "cert"

	certSourcePKCS11 = "pkcs11"
	certSourceSPIFFE = "spiffe"
)

var (
	// ErrInvalidConfig identifies provider configuration that is syntactically valid but unsafe.
	ErrInvalidConfig = errors.New("config invalid")
	// ErrUnsafeFile identifies local filesystem permissions that are too broad for provider inputs.
	ErrUnsafeFile = errors.New("file permissions unsafe")
	// ErrInvalidEncryptionConfiguration identifies Kubernetes EncryptionConfiguration drift.
	ErrInvalidEncryptionConfiguration = errors.New("encryption configuration invalid")
)

// ValidationOptions controls local host checks that require files or directories to exist.
type ValidationOptions struct {
	ConfigFilePath          string
	CheckFilesystem         bool
	AllowedSocketParentDirs []string
}

// ValidationProblem is one actionable configuration validation failure.
type ValidationProblem struct {
	Field   string
	Message string
}

// ValidationError contains all validation failures found in one pass.
type ValidationError struct {
	Kind     error
	Problems []ValidationProblem
}

// Error returns a redacted, actionable validation summary.
func (e ValidationError) Error() string {
	var builder strings.Builder
	builder.WriteString(e.Kind.Error())
	for _, problem := range e.Problems {
		builder.WriteString("; ")
		builder.WriteString(problem.Field)
		builder.WriteString(": ")
		builder.WriteString(problem.Message)
	}
	return builder.String()
}

// Unwrap returns the validation error kind for errors.Is checks.
func (e ValidationError) Unwrap() error {
	return e.Kind
}

// Validate checks required fields, identity-bearing settings, and optional local filesystem policy.
func Validate(cfg Config, opts ValidationOptions) error {
	var problems []ValidationProblem

	problems = append(problems, validateRequired(cfg)...)
	problems = append(problems, validateValues(cfg)...)
	problems = append(problems, validateSocketPolicy(cfg, opts)...)
	if opts.CheckFilesystem {
		problems = append(problems, validateFilesystem(cfg, opts)...)
	}

	if len(problems) > 0 {
		return ValidationError{Kind: ErrInvalidConfig, Problems: problems}
	}
	return nil
}

// IdentityFingerprint returns a stable fingerprint for identity-bearing configuration fields.
func IdentityFingerprint(cfg Config) (string, error) {
	var problems []ValidationProblem
	appendRequired(&problems, "transit.keyIdScope.providerName", cfg.Transit.KeyIDScope.ProviderName)
	appendRequired(&problems, "transit.keyIdScope.clusterId", cfg.Transit.KeyIDScope.ClusterID)
	appendRequired(&problems, "openbao.instanceId", cfg.OpenBao.InstanceID)
	appendRequired(&problems, "transit.keyIdScope.transitMountId", cfg.Transit.KeyIDScope.TransitMountID)
	appendRequired(&problems, "transit.keyIdScope.keyLineageId", cfg.Transit.KeyIDScope.KeyLineageID)
	appendRequired(&problems, "transit.mountPath", cfg.Transit.MountPath)
	appendRequired(&problems, "transit.keyName", cfg.Transit.KeyName)
	if len(problems) > 0 {
		return "", ValidationError{Kind: ErrInvalidConfig, Problems: problems}
	}

	material := configIdentityMaterial{
		Domain:            configFingerprintDomain,
		ProviderName:      cfg.Transit.KeyIDScope.ProviderName,
		ClusterID:         cfg.Transit.KeyIDScope.ClusterID,
		OpenBaoInstanceID: cfg.OpenBao.InstanceID,
		OpenBaoNamespace:  cfg.OpenBao.Namespace,
		TransitMountID:    cfg.Transit.KeyIDScope.TransitMountID,
		KeyLineageID:      cfg.Transit.KeyIDScope.KeyLineageID,
		TransitMountPath:  cfg.Transit.MountPath,
		TransitKeyName:    cfg.Transit.KeyName,
	}
	canonical, err := json.Marshal(material)
	if err != nil {
		return "", fmt.Errorf("marshal config identity fingerprint: %w", err)
	}
	sum := sha256.Sum256(canonical)
	return "cfg1." + base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

// ParseSocketMode parses an octal socket mode string.
func ParseSocketMode(value string) (os.FileMode, error) {
	parsed, err := strconv.ParseUint(value, 8, 32)
	if err != nil {
		return 0, fmt.Errorf("parse socket mode: %w", err)
	}
	mode := os.FileMode(parsed)
	if mode > 0o777 {
		return 0, fmt.Errorf("socket mode must be a permission mode")
	}
	return mode, nil
}

type configIdentityMaterial struct {
	Domain            string `json:"domain"`
	ProviderName      string `json:"provider_name"`
	ClusterID         string `json:"cluster_id"`
	OpenBaoInstanceID string `json:"openbao_instance_id"`
	OpenBaoNamespace  string `json:"openbao_namespace,omitempty"`
	TransitMountID    string `json:"transit_mount_id"`
	KeyLineageID      string `json:"key_lineage_id"`
	TransitMountPath  string `json:"transit_mount_path"`
	TransitKeyName    string `json:"transit_key_name"`
}

func validateRequired(cfg Config) []ValidationProblem {
	var problems []ValidationProblem
	appendRequired(&problems, "server.socketPath", cfg.Server.SocketPath)
	appendRequired(&problems, "server.socketMode", cfg.Server.SocketMode)
	appendRequired(&problems, "server.socketGroup", cfg.Server.SocketGroup)
	appendRequired(&problems, "openbao.address", cfg.OpenBao.Address)
	appendRequired(&problems, "openbao.caCertFile", cfg.OpenBao.CACertFile)
	appendRequired(&problems, "openbao.tlsServerName", cfg.OpenBao.TLSServerName)
	appendRequired(&problems, "openbao.instanceId", cfg.OpenBao.InstanceID)
	appendRequired(&problems, "auth.method", cfg.Auth.Method)
	appendAuthRequired(&problems, cfg.Auth)
	appendRequired(&problems, "transit.mountPath", cfg.Transit.MountPath)
	appendRequired(&problems, "transit.keyName", cfg.Transit.KeyName)
	appendRequired(&problems, "transit.keyIdScope.providerName", cfg.Transit.KeyIDScope.ProviderName)
	appendRequired(&problems, "transit.keyIdScope.clusterId", cfg.Transit.KeyIDScope.ClusterID)
	appendRequired(&problems, "transit.keyIdScope.transitMountId", cfg.Transit.KeyIDScope.TransitMountID)
	appendRequired(&problems, "transit.keyIdScope.keyLineageId", cfg.Transit.KeyIDScope.KeyLineageID)
	return problems
}

func appendAuthRequired(problems *[]ValidationProblem, auth AuthConfig) {
	switch auth.Method {
	case authMethodJWT:
		appendRequired(problems, "auth.jwt.mountPath", auth.JWT.MountPath)
		appendRequired(problems, "auth.jwt.role", auth.JWT.Role)
		appendRequired(problems, "auth.jwt.jwtFile", auth.JWT.JWTFile)
	case authMethodCert:
		appendRequired(problems, "auth.cert.mountPath", auth.Cert.MountPath)
		appendRequired(problems, "auth.cert.source", auth.Cert.Source)
		appendCertSourceRequired(problems, auth.Cert)
	}
}

func appendCertSourceRequired(problems *[]ValidationProblem, cert CertAuthConfig) {
	switch cert.Source {
	case certSourcePKCS11:
		appendRequired(problems, "auth.cert.pkcs11.certificateFile", cert.PKCS11.CertificateFile)
		appendRequired(problems, "auth.cert.pkcs11.modulePath", cert.PKCS11.ModulePath)
		appendRequired(problems, "auth.cert.pkcs11.tokenLabel", cert.PKCS11.TokenLabel)
		appendRequired(problems, "auth.cert.pkcs11.keyLabel", cert.PKCS11.KeyLabel)
		appendRequired(problems, "auth.cert.pkcs11.pinFile", cert.PKCS11.PINFile)
	case certSourceSPIFFE:
		if unsupportedSPIFFECertAuthAllowed {
			appendRequired(problems, "auth.cert.spiffe.workloadAPISocket", cert.SPIFFE.WorkloadAPISocket)
			appendRequired(problems, "auth.cert.spiffe.spiffeID", cert.SPIFFE.SPIFFEID)
		}
	}
}

func validateValues(cfg Config) []ValidationProblem {
	var problems []ValidationProblem

	if cfg.ConfigVersion != defaultConfigVersion {
		appendProblem(&problems, "configVersion", "must be v1alpha1")
	}
	validateOpenBaoAddress(&problems, cfg.OpenBao.Address)
	validateOpenBaoNamespace(&problems, cfg.OpenBao.Namespace)
	validateAddress(&problems, "server.metricsAddress", cfg.Server.MetricsAddress)
	validateAddress(&problems, "server.healthAddress", cfg.Server.HealthAddress)
	validateAuthValues(&problems, cfg.Auth)
	validateMountPath(&problems, "transit.mountPath", cfg.Transit.MountPath)
	validateTransitKeyName(&problems, cfg.Transit.KeyName)
	validateIdentifier(&problems, "transit.keyIdScope.providerName", cfg.Transit.KeyIDScope.ProviderName)
	validateIdentifier(&problems, "transit.keyIdScope.clusterId", cfg.Transit.KeyIDScope.ClusterID)
	validateIdentifier(&problems, "transit.keyIdScope.transitMountId", cfg.Transit.KeyIDScope.TransitMountID)
	validateIdentifier(&problems, "transit.keyIdScope.keyLineageId", cfg.Transit.KeyIDScope.KeyLineageID)
	validateIdentifier(&problems, "openbao.instanceId", cfg.OpenBao.InstanceID)

	if cfg.Rotation.Mode != defaultRotationMode {
		appendProblem(&problems, "rotation.mode", "only observed is supported")
	}
	validatePositiveDuration(&problems, "openbao.timeout", cfg.OpenBao.Timeout)
	validatePositiveDuration(&problems, "auth.loginBeforeTokenExpiry", cfg.Auth.LoginBeforeTokenExpiry)
	validatePositiveDuration(&problems, "auth.tokenRenewalIncrement", cfg.Auth.TokenRenewalIncrement)
	validateNonNegativeDuration(&problems, "auth.loginTimeout", cfg.Auth.LoginTimeout)
	validateNonNegativeDuration(&problems, "bootstrap.graceTimeout", cfg.Bootstrap.GraceTimeout)
	validatePositiveDuration(&problems, "bootstrap.retryInterval", cfg.Bootstrap.RetryInterval)
	validatePositiveDuration(&problems, "status.probeInterval", cfg.Status.ProbeInterval)
	validatePositiveDuration(&problems, "status.deepProbeInterval", cfg.Status.DeepProbeInterval)
	validatePositiveDuration(&problems, "status.statusMaxStaleness", cfg.Status.StatusMaxStaleness)
	validateAbsolutePath(&problems, "state.path", cfg.State.Path)
	validatePositiveDuration(&problems, "rotation.activationDelay", cfg.Rotation.ActivationDelay)
	if cfg.Rotation.RequireStableObservationCount <= 0 {
		appendProblem(&problems, "rotation.requireStableObservationCount", "must be positive")
	}
	switch cfg.Logging.Level {
	case "debug", "info", "warn", "error":
	default:
		appendProblem(&problems, "logging.level", "must be one of debug, info, warn, or error")
	}
	if cfg.Logging.Format != "json" && cfg.Logging.Format != "text" {
		appendProblem(&problems, "logging.format", "must be json or text")
	}
	validateDebugCorrelation(&problems, cfg.Logging)

	return problems
}

func validateAuthValues(problems *[]ValidationProblem, auth AuthConfig) {
	switch auth.Method {
	case authMethodJWT:
		validateJWTAuthValues(problems, auth.JWT)
	case authMethodCert:
		validateCertAuthValues(problems, auth.Cert)
	default:
		if auth.Method != "" {
			appendProblem(problems, "auth.method", "must be one of jwt or cert")
		}
	}
}

func validateJWTAuthValues(problems *[]ValidationProblem, jwt JWTAuthConfig) {
	validateMountPath(problems, "auth.jwt.mountPath", jwt.MountPath)
	validateIdentifier(problems, "auth.jwt.role", jwt.Role)
	validateClaimExpectation(problems, "auth.jwt.expectedIssuer", jwt.ExpectedIssuer)
	validateClaimExpectation(problems, "auth.jwt.expectedSubject", jwt.ExpectedSubject)
	for _, audience := range jwt.ExpectedAudience {
		if audience == "" {
			appendProblem(problems, "auth.jwt.expectedAudience", "must not contain empty values")
			continue
		}
		validateClaimExpectation(problems, "auth.jwt.expectedAudience", audience)
	}
	validatePositiveDuration(problems, "auth.jwt.minRemainingTtl", jwt.MinRemainingTTL)
	validateNonNegativeDuration(problems, "auth.jwt.clockSkewLeeway", jwt.ClockSkewLeeway)
}

func validateCertAuthValues(problems *[]ValidationProblem, cert CertAuthConfig) {
	validateMountPath(problems, "auth.cert.mountPath", cert.MountPath)
	validateIdentifier(problems, "auth.cert.name", cert.Name)
	validatePositiveDuration(problems, "auth.cert.minRemainingTtl", cert.MinRemainingTTL)
	validateNonNegativeDuration(problems, "auth.cert.clockSkewLeeway", cert.ClockSkewLeeway)
	switch cert.Source {
	case certSourcePKCS11:
		validatePKCS11AuthValues(problems, cert.PKCS11)
	case certSourceSPIFFE:
		if !unsupportedSPIFFECertAuthAllowed {
			appendProblem(
				problems,
				"auth.cert.source",
				"spiffe is unavailable until the supported OpenBao version can derive certificate identity aliases from URI SANs",
			)
			return
		}
		validateSPIFFEAuthValues(problems, cert.SPIFFE)
	default:
		if cert.Source != "" {
			if unsupportedSPIFFECertAuthAllowed {
				appendProblem(problems, "auth.cert.source", "must be one of pkcs11 or spiffe")
			} else {
				appendProblem(problems, "auth.cert.source", "must be pkcs11")
			}
		}
	}
}

func validatePKCS11AuthValues(problems *[]ValidationProblem, pkcs11 PKCS11CertAuthConfig) {
	validateAbsolutePath(problems, "auth.cert.pkcs11.certificateFile", pkcs11.CertificateFile)
	validateAbsolutePath(problems, "auth.cert.pkcs11.modulePath", pkcs11.ModulePath)
	validateIdentifier(problems, "auth.cert.pkcs11.tokenLabel", pkcs11.TokenLabel)
	validateIdentifier(problems, "auth.cert.pkcs11.keyLabel", pkcs11.KeyLabel)
	validateAbsolutePath(problems, "auth.cert.pkcs11.pinFile", pkcs11.PINFile)
	if pkcs11.MaxSessions < 2 {
		appendProblem(problems, "auth.cert.pkcs11.maxSessions", "must be at least 2")
	}
}

func validateSPIFFEAuthValues(problems *[]ValidationProblem, spiffe SPIFFECertAuthConfig) {
	validateWorkloadAPISocket(problems, spiffe.WorkloadAPISocket)
	validateSPIFFEID(problems, spiffe.SPIFFEID, spiffe.TrustDomain)
	validateTrustDomain(problems, spiffe.TrustDomain)
}

func validateDebugCorrelation(problems *[]ValidationProblem, logging LoggingConfig) {
	correlation := logging.DebugCorrelation
	if !correlation.Enabled {
		return
	}
	if logging.Level != "debug" {
		appendProblem(
			problems,
			"logging.debugCorrelation.enabled",
			"requires logging.level debug",
		)
	}
	if !logging.LogOpenBaoRequestIDs {
		appendProblem(
			problems,
			"logging.logOpenBaoRequestIDs",
			"must be true when debug correlation is enabled",
		)
	}
	validatePositiveDuration(problems, "logging.debugCorrelation.ttl", correlation.TTL)
	if correlation.TTL > maxDebugCorrelationTTL {
		appendProblem(
			problems,
			"logging.debugCorrelation.ttl",
			"must not exceed 1h",
		)
	}
	if correlation.IncidentID == "" {
		appendProblem(
			problems,
			"logging.debugCorrelation.incidentId",
			"is required when debug correlation is enabled",
		)
		return
	}
	if len(correlation.IncidentID) > maxIncidentIDLength {
		appendProblem(
			problems,
			"logging.debugCorrelation.incidentId",
			"must be at most 64 characters",
		)
	}
	validateIdentifier(problems, "logging.debugCorrelation.incidentId", correlation.IncidentID)
}

func validateSocketPolicy(cfg Config, opts ValidationOptions) []ValidationProblem {
	var problems []ValidationProblem
	if !filepath.IsAbs(cfg.Server.SocketPath) {
		appendProblem(&problems, "server.socketPath", "must be an absolute Unix socket path")
	}
	mode, err := ParseSocketMode(cfg.Server.SocketMode)
	if err != nil {
		appendProblem(&problems, "server.socketMode", err.Error())
	} else if mode&socketDisallowedMode != 0 {
		appendProblem(&problems, "server.socketMode", "must not allow world access or execute bits")
	}

	parent := filepath.Clean(filepath.Dir(cfg.Server.SocketPath))
	if !pathWithinAllowedDirs(parent, socketParentDirs(opts)) {
		appendProblem(&problems, "server.socketPath", "must be under an approved runtime socket directory")
	}
	return problems
}

func validateFilesystem(cfg Config, opts ValidationOptions) []ValidationProblem {
	var problems []ValidationProblem
	if opts.ConfigFilePath != "" {
		validateRegularFile(&problems, "config", opts.ConfigFilePath, configFileDisallowedMode)
	}
	validateRegularFile(&problems, "openbao.caCertFile", cfg.OpenBao.CACertFile, caFileDisallowedMode)
	switch cfg.Auth.Method {
	case authMethodJWT:
		validateRegularFile(&problems, "auth.jwt.jwtFile", cfg.Auth.JWT.JWTFile, jwtFileDisallowedMode)
	case authMethodCert:
		if cfg.Auth.Cert.Source == certSourcePKCS11 {
			validateRegularFile(
				&problems,
				"auth.cert.pkcs11.certificateFile",
				cfg.Auth.Cert.PKCS11.CertificateFile,
				certFileDisallowedMode,
			)
			validateRegularFile(
				&problems,
				"auth.cert.pkcs11.pinFile",
				cfg.Auth.Cert.PKCS11.PINFile,
				pinFileDisallowedMode,
			)
		}
	}
	validateSocketParent(&problems, cfg.Server.SocketPath)
	validateSocketTarget(&problems, cfg.Server.SocketPath)
	return problems
}

func appendRequired(problems *[]ValidationProblem, field string, value string) {
	if strings.TrimSpace(value) == "" {
		appendProblem(problems, field, "is required")
	}
}

func appendProblem(problems *[]ValidationProblem, field string, message string) {
	*problems = append(*problems, ValidationProblem{Field: field, Message: message})
}

func validateOpenBaoAddress(problems *[]ValidationProblem, value string) {
	parsed, err := url.Parse(value)
	if err != nil ||
		parsed.Scheme != "https" ||
		parsed.Host == "" ||
		parsed.User != nil ||
		parsed.RawQuery != "" ||
		parsed.Fragment != "" {
		appendProblem(problems, "openbao.address", "must be an https URL with a host and no user info, query, or fragment")
	}
}

func validateAddress(problems *[]ValidationProblem, field string, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	if _, _, err := net.SplitHostPort(value); err != nil {
		appendProblem(problems, field, "must be host:port")
	}
}

func validateMountPath(problems *[]ValidationProblem, field string, value string) {
	if value == "" {
		return
	}
	if strings.TrimSpace(value) != value || strings.ContainsAny(value, "\x00\r\n\t") {
		appendProblem(problems, field, "must not contain control characters or surrounding whitespace")
		return
	}
	if strings.HasPrefix(value, "/") || strings.Contains(value, "//") {
		appendProblem(problems, field, "must be a relative OpenBao path")
		return
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "." || segment == ".." || segment == "" {
			appendProblem(problems, field, "must not contain empty, dot, or dot-dot path segments")
			return
		}
	}
}

func validateOpenBaoNamespace(problems *[]ValidationProblem, value string) {
	if value == "" {
		return
	}
	if strings.TrimSpace(value) != value || strings.ContainsAny(value, "\x00\r\n\t") {
		appendProblem(problems, "openbao.namespace", "must not contain control characters or surrounding whitespace")
		return
	}
	if strings.HasPrefix(value, "/") || strings.Contains(value, "//") || strings.Contains(value, "%") {
		appendProblem(problems, "openbao.namespace", "must be a relative OpenBao namespace path without percent encoding")
		return
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "." || segment == ".." || segment == "" {
			appendProblem(problems, "openbao.namespace", "must not contain empty, dot, or dot-dot path segments")
			return
		}
	}
}

func validateIdentifier(problems *[]ValidationProblem, field string, value string) {
	if value == "" {
		return
	}
	if strings.TrimSpace(value) != value || strings.ContainsAny(value, "\x00\r\n\t") {
		appendProblem(problems, field, "must not contain control characters or surrounding whitespace")
	}
}

func validateTransitKeyName(problems *[]ValidationProblem, value string) {
	validateIdentifier(problems, "transit.keyName", value)
	if value == "" {
		return
	}
	if strings.ContainsAny(value, "/%") {
		appendProblem(problems, "transit.keyName", "must be a single OpenBao path segment without / or %")
	}
}

func validateClaimExpectation(problems *[]ValidationProblem, field string, value string) {
	if value == "" {
		return
	}
	if strings.TrimSpace(value) != value || strings.ContainsAny(value, "\x00\r\n\t") {
		appendProblem(problems, field, "must not contain control characters or surrounding whitespace")
	}
}

func validateWorkloadAPISocket(problems *[]ValidationProblem, value string) {
	if value == "" {
		return
	}
	parsed, err := url.Parse(value)
	if err != nil ||
		parsed.Scheme != "unix" ||
		parsed.Host != "" ||
		parsed.RawQuery != "" ||
		parsed.Fragment != "" ||
		!filepath.IsAbs(parsed.Path) {
		appendProblem(problems, "auth.cert.spiffe.workloadAPISocket", "must be a unix:// URI with an absolute socket path")
	}
}

func validateSPIFFEID(problems *[]ValidationProblem, value string, trustDomain string) {
	if value == "" {
		return
	}
	parsed, err := url.Parse(value)
	if err != nil ||
		parsed.Scheme != certSourceSPIFFE ||
		parsed.Host == "" ||
		parsed.RawQuery != "" ||
		parsed.Fragment != "" ||
		parsed.User != nil ||
		parsed.Path == "" {
		appendProblem(problems, "auth.cert.spiffe.spiffeID", "must be a spiffe:// URI with a trust domain and path")
		return
	}
	if trustDomain != "" && parsed.Host != trustDomain {
		appendProblem(problems, "auth.cert.spiffe.trustDomain", "must match auth.cert.spiffe.spiffeID trust domain")
	}
}

func validateTrustDomain(problems *[]ValidationProblem, value string) {
	if value == "" {
		return
	}
	if strings.TrimSpace(value) != value ||
		strings.ContainsAny(value, "\x00\r\n\t /:%") {
		appendProblem(
			problems,
			"auth.cert.spiffe.trustDomain",
			"must be a DNS-style trust domain without whitespace, slash, colon, or percent encoding",
		)
	}
}

func validateAbsolutePath(problems *[]ValidationProblem, field string, value string) {
	if value == "" {
		return
	}
	if !filepath.IsAbs(value) {
		appendProblem(problems, field, "must be an absolute path")
	}
}

func validatePositiveDuration(problems *[]ValidationProblem, field string, value time.Duration) {
	if value <= 0 {
		appendProblem(problems, field, "must be positive")
	}
}

func validateNonNegativeDuration(problems *[]ValidationProblem, field string, value time.Duration) {
	if value < 0 {
		appendProblem(problems, field, "must not be negative")
	}
}

func validateRegularFile(problems *[]ValidationProblem, field string, path string, disallowed os.FileMode) {
	if path == "" {
		return
	}
	if !filepath.IsAbs(path) {
		appendProblem(problems, field, "must be an absolute path")
		return
	}
	info, err := os.Lstat(path)
	if err != nil {
		appendProblem(problems, field, "must exist and be readable")
		return
	}
	if info.Mode()&os.ModeSymlink != 0 {
		appendProblem(problems, field, "must not be a symlink")
		return
	}
	if !info.Mode().IsRegular() {
		appendProblem(problems, field, "must be a regular file")
		return
	}
	if info.Mode().Perm()&disallowed != 0 {
		appendProblem(problems, field, fmt.Sprintf("%s: mode must not include %04o", ErrUnsafeFile, disallowed))
	}
}

func validateSocketParent(problems *[]ValidationProblem, socketPath string) {
	parent := filepath.Dir(socketPath)
	info, err := os.Lstat(parent)
	if err != nil {
		appendProblem(problems, "server.socketPath", "parent directory must exist")
		return
	}
	if info.Mode()&os.ModeSymlink != 0 {
		appendProblem(problems, "server.socketPath", "parent directory must not be a symlink")
		return
	}
	if !info.IsDir() {
		appendProblem(problems, "server.socketPath", "parent path must be a directory")
		return
	}
	if info.Mode().Perm()&0o022 != 0 {
		appendProblem(problems, "server.socketPath", "parent directory must not be group-writable or world-writable")
	}
}

func validateSocketTarget(problems *[]ValidationProblem, socketPath string) {
	info, err := os.Lstat(socketPath)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		appendProblem(problems, "server.socketPath", "cannot inspect socket path")
		return
	}
	if info.Mode()&os.ModeSymlink != 0 {
		appendProblem(problems, "server.socketPath", "socket path must not be a symlink")
		return
	}
	if info.Mode()&os.ModeSocket == 0 {
		appendProblem(problems, "server.socketPath", "existing path must be a Unix socket")
	}
}

func socketParentDirs(opts ValidationOptions) []string {
	if len(opts.AllowedSocketParentDirs) > 0 {
		return opts.AllowedSocketParentDirs
	}
	return []string{"/run/openbao-kms", "/var/run/openbao-kms"}
}

func pathWithinAllowedDirs(path string, allowed []string) bool {
	cleanPath := filepath.Clean(path)
	for _, root := range allowed {
		cleanRoot := filepath.Clean(root)
		if cleanPath == cleanRoot {
			return true
		}
		if strings.HasPrefix(cleanPath, cleanRoot+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
