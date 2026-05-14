package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/aad"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/auth"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/cli"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/config"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/kmsv2"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/status"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/version"
	"github.com/spf13/cobra"
	kmsapi "k8s.io/kms/apis/v2"
)

const (
	reportNameDoctor    = "doctor"
	reportNameVerifyKey = "verify-key"

	checkConfigLoad             = "config.load"
	checkConfigValidate         = "config.validate"
	checkSocketGroup            = "socket.group"
	checkJWTLocal               = "jwt.local"
	checkCertLocal              = "auth.cert.local"
	checkCertSigner             = "auth.cert.signer"
	checkCertPKCS11             = "auth.cert.pkcs11"
	checkCertSPIFFE             = "auth.cert.spiffe"
	checkEncryptionConfig       = "kubernetes.encryption_config"
	checkOpenBaoTLS             = "openbao.tls"
	checkOpenBaoAuth            = "openbao.auth"
	checkTransitCapabilities    = "transit.capabilities"
	checkTransitMetadata        = "transit.metadata"
	checkTransitProfile         = "transit.profile"
	checkTransitDisableUpsert   = "transit.disable_upsert"
	checkTransitProbe           = "transit.probe"
	checkKeyIDDeterministic     = "key_id.deterministic"
	checkStatusEncryptInvariant = "kms.status_encrypt"
	checkRegistryState          = "registry.state"
	checkVersionRestrictions    = "transit.version_restrictions"

	capabilityCreate = "create"
	capabilityDelete = "delete"
	capabilityRead   = "read"
	capabilitySudo   = "sudo"
	capabilityUpdate = "update"

	transitSegmentBackup        = "backup"
	transitSegmentDecrypt       = "decrypt"
	transitSegmentEncrypt       = "encrypt"
	transitSegmentEncryptionKey = "encryption-key"
	transitSegmentExport        = "export"
	transitSegmentKeys          = "keys"
	transitSegmentRotate        = "rotate"

	messageConfigValidationFailed = "config validation failed"
	messageDiagnosticPlaintext    = "diagnostic"
	messageDiagnosticVersion      = "diagnostic"
	messageDoctorProbeAAD         = "openbao-kubernetes-kms/doctor-probe/v1"
	messageDoctorChecksFailed     = "doctor checks failed"
	messageVerifyKeyChecksFailed  = "verify-key checks failed"
)

type diagnosticClients struct {
	transitClient *openbao.Client
}

type transitDiagnostics struct {
	profile openbao.KeyProfile
	state   keyregistry.StateFile
}

func newDoctorCommand(runtimeConfig *config.Runtime, configPath *string, info version.Info) *cobra.Command {
	var encryptionConfigPath string
	var output string

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Run preflight checks",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			report, err := runDoctor(commandContext(cmd), runtimeConfig, *configPath, encryptionConfigPath, info)
			if printErr := printCLIReport(cmd.OutOrStdout(), report, output); printErr != nil {
				return printErr
			}
			if err != nil {
				return err
			}
			return reportError(report, messageDoctorChecksFailed)
		},
	}
	cmd.Flags().StringVar(
		&encryptionConfigPath,
		"encryption-config",
		"",
		"Path to Kubernetes EncryptionConfiguration for provider validation",
	)
	addOutputFlag(cmd, &output)
	return cmd
}

func newVerifyKeyCommand(runtimeConfig *config.Runtime, configPath *string) *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "verify-key",
		Short: "Verify Transit key suitability",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			report, err := runVerifyKey(commandContext(cmd), runtimeConfig, *configPath)
			if printErr := printCLIReport(cmd.OutOrStdout(), report, output); printErr != nil {
				return printErr
			}
			if err != nil {
				return err
			}
			return reportError(report, messageVerifyKeyChecksFailed)
		},
	}
	addOutputFlag(cmd, &output)
	return cmd
}

func runDoctor(
	ctx context.Context,
	runtimeConfig *config.Runtime,
	configPath string,
	encryptionConfigPath string,
	info version.Info,
) (cli.Report, error) {
	report := cli.Report{Name: reportNameDoctor}
	cfg, err := config.Load(runtimeConfig, config.LoadOptions{Path: configPath})
	if err != nil {
		report.Fail(checkConfigLoad, "Config load", safeMessage(err))
		return report, cli.WithExitCode(cli.ExitConfig, err)
	}
	report.Pass(checkConfigLoad, "Config load", "loaded typed provider configuration")

	if err := config.Validate(cfg, config.ValidationOptions{
		ConfigFilePath:  configPath,
		CheckFilesystem: true,
	}); err != nil {
		report.Fail(checkConfigValidate, "Config validation", safeMessage(err))
		report.Skip(checkOpenBaoAuth, openBaoAuthCheckName(cfg), messageConfigValidationFailed)
		return report, cli.WithExitCode(cli.ExitConfig, errors.New("doctor config validation failed"))
	}
	report.Pass(checkConfigValidate, "Config validation", "local configuration is syntactically safe")

	if _, err := lookupGroupID(cfg.Server.SocketGroup); err != nil {
		report.Fail(checkSocketGroup, "Socket group", safeMessage(err))
	} else {
		report.Pass(checkSocketGroup, "Socket group", "configured group resolves locally")
	}

	authLocalValid := checkLocalAuthForDoctor(ctx, &report, cfg)

	checkEncryptionConfiguration(&report, cfg, encryptionConfigPath)
	if !authLocalValid {
		report.Skip(checkOpenBaoAuth, openBaoAuthCheckName(cfg), "local auth validation failed")
		return report, nil
	}

	clients, ok := authenticateForDiagnostics(ctx, &report, cfg)
	if !ok {
		return report, nil
	}
	diag, ok := runTransitDiagnostics(ctx, &report, cfg, clients.transitClient, true)
	if ok {
		checkStatusEncryptConsistency(ctx, &report, diag.state, info)
	}
	return report, nil
}

func jwtValidationOptions(cfg config.Config) auth.JWTValidationOptions {
	return auth.JWTValidationOptions{
		MinRemainingTTL:  cfg.Auth.JWT.MinRemainingTTL,
		ClockSkewLeeway:  cfg.Auth.JWT.ClockSkewLeeway,
		ExpectedIssuer:   cfg.Auth.JWT.ExpectedIssuer,
		ExpectedAudience: cfg.Auth.JWT.ExpectedAudience,
		ExpectedSubject:  cfg.Auth.JWT.ExpectedSubject,
	}
}

func checkLocalAuthForDoctor(ctx context.Context, report *cli.Report, cfg config.Config) bool {
	switch cfg.Auth.Method {
	case authMethodJWT:
		if _, err := auth.ReadAndValidateJWT(cfg.Auth.JWT.JWTFile, jwtValidationOptions(cfg)); err != nil {
			report.Fail(checkJWTLocal, "JWT file", safeMessage(err))
			return false
		}
		report.Pass(checkJWTLocal, "JWT file", "readable and locally valid")
		return true
	case authMethodCert:
		return checkLocalCertificateAuthForDoctor(ctx, report, cfg)
	default:
		report.Skip(checkCertLocal, "Certificate identity", "unsupported auth method")
		return false
	}
}

func runVerifyKey(
	ctx context.Context,
	runtimeConfig *config.Runtime,
	configPath string,
) (cli.Report, error) {
	report := cli.Report{Name: reportNameVerifyKey}
	cfg, err := config.Load(runtimeConfig, config.LoadOptions{Path: configPath})
	if err != nil {
		report.Fail(checkConfigLoad, "Config load", safeMessage(err))
		return report, cli.WithExitCode(cli.ExitConfig, err)
	}
	report.Pass(checkConfigLoad, "Config load", "loaded typed provider configuration")

	if err := config.Validate(cfg, config.ValidationOptions{ConfigFilePath: configPath}); err != nil {
		report.Fail(checkConfigValidate, "Config validation", safeMessage(err))
		return report, cli.WithExitCode(cli.ExitConfig, errors.New("verify-key config validation failed"))
	}
	report.Pass(checkConfigValidate, "Config validation", "identity-bearing configuration is valid")

	clients, ok := authenticateForDiagnostics(ctx, &report, cfg)
	if !ok {
		return report, nil
	}
	diag, ok := runTransitDiagnostics(ctx, &report, cfg, clients.transitClient, false)
	if ok {
		checkRegistryVersionRestrictions(&report, cfg, diag.profile)
	}
	return report, nil
}

func authenticateForDiagnostics(
	ctx context.Context,
	report *cli.Report,
	cfg config.Config,
) (diagnosticClients, bool) {
	if _, err := openbao.NewTLSConfig(cfg.OpenBao.CACertFile, cfg.OpenBao.TLSServerName); err != nil {
		report.Fail(checkOpenBaoTLS, "OpenBao TLS config", safeMessage(err))
		report.Skip(checkOpenBaoAuth, openBaoAuthCheckName(cfg), "TLS configuration failed")
		return diagnosticClients{}, false
	}
	report.Pass(checkOpenBaoTLS, "OpenBao TLS config", "CA bundle and server name are usable")

	manager, err := buildAuthManager(ctx, cfg, nil)
	if err != nil {
		report.Fail(checkOpenBaoAuth, openBaoAuthCheckName(cfg), safeMessage(err))
		return diagnosticClients{}, false
	}
	loginCtx, cancel := withTimeout(ctx, authLoginTimeout(cfg))
	defer cancel()
	if err := manager.Refresh(loginCtx); err != nil {
		report.Fail(checkOpenBaoAuth, openBaoAuthCheckName(cfg), safeMessage(err))
		return diagnosticClients{}, false
	}
	report.Pass(checkOpenBaoAuth, openBaoAuthCheckName(cfg), openBaoAuthPassMessage(cfg))

	transitClient, err := openbao.NewClient(openbao.ClientConfig{
		Address:       cfg.OpenBao.Address,
		Namespace:     cfg.OpenBao.Namespace,
		CACertFile:    cfg.OpenBao.CACertFile,
		TLSServerName: cfg.OpenBao.TLSServerName,
		Timeout:       cfg.OpenBao.Timeout,
		TokenSource:   manager,
	})
	if err != nil {
		report.Fail(checkOpenBaoTLS, "OpenBao TLS config", safeMessage(err))
		return diagnosticClients{}, false
	}
	return diagnosticClients{transitClient: transitClient}, true
}

func openBaoAuthCheckName(cfg config.Config) string {
	if cfg.Auth.Method == authMethodCert {
		return "OpenBao cert login"
	}
	return "OpenBao JWT login"
}

func openBaoAuthPassMessage(cfg config.Config) string {
	if cfg.Auth.Method == authMethodCert {
		return "authenticated with configured certificate role"
	}
	return "authenticated with configured JWT role"
}

func certificateSourceCheckID(cfg config.Config) string {
	switch cfg.Auth.Cert.Source {
	case certSourcePKCS11:
		return checkCertPKCS11
	case certSourceSPIFFE:
		return checkCertSPIFFE
	default:
		return checkCertLocal
	}
}

func certificateSourceCheckTitle(cfg config.Config) string {
	switch cfg.Auth.Cert.Source {
	case certSourcePKCS11:
		return "PKCS#11 certificate source"
	case certSourceSPIFFE:
		return "SPIFFE certificate source"
	default:
		return "Certificate source"
	}
}

func runTransitDiagnostics(
	ctx context.Context,
	report *cli.Report,
	cfg config.Config,
	client openbao.TransitClient,
	includeProbe bool,
) (transitDiagnostics, bool) {
	if err := checkCapabilities(ctx, cfg, client); err != nil {
		report.Fail(checkTransitCapabilities, "Transit policy capabilities", safeMessage(err))
	} else {
		report.Pass(checkTransitCapabilities, "Transit policy capabilities", "required hot-path capabilities are present")
	}

	profile, err := client.ReadKeyProfile(ctx, cfg.Transit.MountPath, cfg.Transit.KeyName)
	if err != nil {
		report.Fail(checkTransitMetadata, "Transit metadata", safeMessage(err))
		return transitDiagnostics{}, false
	}
	report.Pass(checkTransitMetadata, "Transit metadata", "configured key metadata is readable")

	profileFindings := keyProfileFindings(profile)
	profileSafe := len(profileFindings) == 0
	if !profileSafe {
		report.Fail(checkTransitProfile, "Transit key profile", strings.Join(profileFindings, "; "))
	} else {
		report.Pass(checkTransitProfile, "Transit key profile", "key settings are suitable for KMS")
	}

	disableUpsert, err := client.ReadDisableUpsert(ctx, cfg.Transit.MountPath)
	if err != nil {
		report.Fail(checkTransitDisableUpsert, "Transit disable_upsert", safeMessage(err))
	} else if !disableUpsert {
		report.Fail(checkTransitDisableUpsert, "Transit disable_upsert", "mount allows implicit key creation")
	} else {
		report.Pass(checkTransitDisableUpsert, "Transit disable_upsert", "implicit key creation is disabled")
	}

	if includeProbe && profileSafe {
		if _, err := client.ProbeEncryptDecrypt(ctx, openbao.ProbeRequest{
			MountPath:      cfg.Transit.MountPath,
			KeyName:        cfg.Transit.KeyName,
			KeyVersion:     profile.LatestVersion,
			AssociatedData: []byte(messageDoctorProbeAAD),
		}); err != nil {
			report.Fail(checkTransitProbe, "Transit probe", safeMessage(err))
		} else {
			report.Pass(checkTransitProbe, "Transit probe", "non-secret encrypt/decrypt probe succeeded")
		}
	} else if includeProbe {
		report.Skip(checkTransitProbe, "Transit probe", "Transit key profile failed")
	}

	if !profileSafe {
		report.Skip(checkKeyIDDeterministic, "Key ID determinism", "Transit key profile failed")
		return transitDiagnostics{profile: profile}, false
	}

	state, _, ok := checkKeyIDDeterminism(report, cfg, profile)
	return transitDiagnostics{
		profile: profile,
		state:   state,
	}, ok
}

func checkEncryptionConfiguration(report *cli.Report, cfg config.Config, encryptionConfigPath string) {
	if encryptionConfigPath == "" {
		report.Skip(checkEncryptionConfig, "EncryptionConfiguration", "no --encryption-config path supplied")
		return
	}
	encCfg, err := config.LoadEncryptionConfiguration(encryptionConfigPath)
	if err != nil {
		report.Fail(checkEncryptionConfig, "EncryptionConfiguration", safeMessage(err))
		return
	}
	result, err := config.ValidateEncryptionConfiguration(
		cfg,
		encCfg,
		config.EncryptionValidationOptions{AllowIdentityFallback: true},
	)
	if err != nil {
		report.Fail(checkEncryptionConfig, "EncryptionConfiguration", safeMessage(err))
		return
	}
	if result.IdentityFallback {
		report.Warn(checkEncryptionConfig, "EncryptionConfiguration", "identity fallback remains configured")
		return
	}
	report.Pass(checkEncryptionConfig, "EncryptionConfiguration", "KMS v2 provider matches local config")
}

func checkCapabilities(
	ctx context.Context,
	cfg config.Config,
	client openbao.TransitClient,
) error {
	paths := transitCapabilityPaths(cfg)
	caps, err := client.Capabilities(ctx, paths.all())
	if err != nil {
		return err
	}
	if hasAnyCapability(caps, paths.metadata, capabilityCreate, capabilityUpdate, capabilityDelete, capabilitySudo) ||
		hasAnyCapability(caps, paths.rotate, capabilityUpdate, capabilitySudo) ||
		hasAnyCapability(caps, paths.export, capabilityCreate, capabilityRead, capabilityUpdate, capabilitySudo) ||
		hasAnyCapability(caps, paths.backup, capabilityCreate, capabilityRead, capabilityUpdate, capabilitySudo) ||
		hasAnyCapability(caps, paths.encrypt, capabilitySudo) ||
		hasAnyCapability(caps, paths.decrypt, capabilitySudo) {
		return fmt.Errorf("token can perform non-hot-path key management")
	}
	if !hasAnyCapability(caps, paths.metadata, capabilityRead) {
		return fmt.Errorf("metadata read capability is missing")
	}
	if !hasAnyCapability(caps, paths.encrypt, capabilityUpdate) {
		return fmt.Errorf("encrypt update capability is missing")
	}
	if !hasAnyCapability(caps, paths.decrypt, capabilityUpdate) {
		return fmt.Errorf("decrypt update capability is missing")
	}
	return nil
}

type transitCapabilityPathSet struct {
	metadata string
	encrypt  string
	decrypt  string
	rotate   string
	export   string
	backup   string
}

func transitCapabilityPaths(cfg config.Config) transitCapabilityPathSet {
	return transitCapabilityPathSet{
		metadata: path.Join(cfg.Transit.MountPath, transitSegmentKeys, cfg.Transit.KeyName),
		encrypt:  path.Join(cfg.Transit.MountPath, transitSegmentEncrypt, cfg.Transit.KeyName),
		decrypt:  path.Join(cfg.Transit.MountPath, transitSegmentDecrypt, cfg.Transit.KeyName),
		rotate: path.Join(
			cfg.Transit.MountPath,
			transitSegmentKeys,
			cfg.Transit.KeyName,
			transitSegmentRotate,
		),
		export: path.Join(
			cfg.Transit.MountPath,
			transitSegmentExport,
			transitSegmentEncryptionKey,
			cfg.Transit.KeyName,
		),
		backup: path.Join(cfg.Transit.MountPath, transitSegmentBackup, cfg.Transit.KeyName),
	}
}

func (p transitCapabilityPathSet) all() []string {
	return []string{p.metadata, p.encrypt, p.decrypt, p.rotate, p.export, p.backup}
}

func hasAnyCapability(caps openbao.CapabilitiesResult, capabilityPath string, capabilities ...string) bool {
	for _, capability := range capabilities {
		if slices.Contains(caps.ByPath[capabilityPath], capability) {
			return true
		}
	}
	return false
}

func keyProfileFindings(profile openbao.KeyProfile) []string {
	findings := make([]string, 0)
	if profile.LatestVersion <= 0 {
		findings = append(findings, keyProfileFindingMessage(
			openbao.KeyProfileFindingImpactAvailability,
			"latest version is not positive",
		))
	}
	if profile.SoftDeleted {
		findings = append(findings, keyProfileFindingMessage(
			openbao.KeyProfileFindingImpactAvailability,
			"key is soft-deleted",
		))
	}
	if profile.MinAvailableVersion > profile.LatestVersion {
		findings = append(findings, keyProfileFindingMessage(
			openbao.KeyProfileFindingImpactAvailability,
			"minimum available version exceeds latest version",
		))
	}
	for _, finding := range openbao.AssessKeyProfile(profile) {
		findings = append(findings, openbao.FormatKeyProfileFindings([]openbao.KeyProfileFinding{finding}))
	}
	return findings
}

func keyProfileFindingMessage(impact openbao.KeyProfileFindingImpact, message string) string {
	return fmt.Sprintf("%s/%s: %s", openbao.KeyProfileFindingSeverityBlocking, impact, message)
}

func checkKeyIDDeterminism(
	report *cli.Report,
	cfg config.Config,
	profile openbao.KeyProfile,
) (keyregistry.StateFile, keyregistry.KeySnapshot, bool) {
	observer, err := status.NewObserver(snapshotScope(cfg), rotationPolicy(cfg))
	if err != nil {
		report.Fail(checkKeyIDDeterministic, "Key ID determinism", safeMessage(err))
		return keyregistry.StateFile{}, keyregistry.KeySnapshot{}, false
	}
	now := time.Unix(1_778_277_600, 0).UTC()
	first, err := observer.RebuildState(profile, now)
	if err != nil {
		report.Fail(checkKeyIDDeterministic, "Key ID determinism", safeMessage(err))
		return keyregistry.StateFile{}, keyregistry.KeySnapshot{}, false
	}
	second, err := observer.RebuildState(profile, now)
	if err != nil {
		report.Fail(checkKeyIDDeterministic, "Key ID determinism", safeMessage(err))
		return keyregistry.StateFile{}, keyregistry.KeySnapshot{}, false
	}
	firstActive, err := first.ActiveSnapshot()
	if err != nil {
		report.Fail(checkKeyIDDeterministic, "Key ID determinism", safeMessage(err))
		return keyregistry.StateFile{}, keyregistry.KeySnapshot{}, false
	}
	secondActive, err := second.ActiveSnapshot()
	if err != nil {
		report.Fail(checkKeyIDDeterministic, "Key ID determinism", safeMessage(err))
		return keyregistry.StateFile{}, keyregistry.KeySnapshot{}, false
	}
	if firstActive.KubernetesKeyID != secondActive.KubernetesKeyID {
		report.Fail(checkKeyIDDeterministic, "Key ID determinism", "same metadata produced different key_id values")
		return keyregistry.StateFile{}, keyregistry.KeySnapshot{}, false
	}
	report.Pass(
		checkKeyIDDeterministic,
		"Key ID determinism",
		"active key_id hash "+aad.HashValue(firstActive.KubernetesKeyID),
	)
	return first, firstActive, true
}

func checkStatusEncryptConsistency(
	ctx context.Context,
	report *cli.Report,
	state keyregistry.StateFile,
	info version.Info,
) {
	store, err := status.NewStore(status.StoreOptions{MaxStaleness: time.Minute})
	if err != nil {
		report.Fail(checkStatusEncryptInvariant, "Status/encrypt consistency", safeMessage(err))
		return
	}
	if err := store.PublishHealthy(state, time.Now().UTC()); err != nil {
		report.Fail(checkStatusEncryptInvariant, "Status/encrypt consistency", safeMessage(err))
		return
	}
	server, err := kmsv2.NewServer(kmsv2.Options{
		StatusCache:   store,
		Registry:      store,
		Transit:       &diagnosticTransit{},
		PluginVersion: diagnosticPluginVersion(info),
	})
	if err != nil {
		report.Fail(checkStatusEncryptInvariant, "Status/encrypt consistency", safeMessage(err))
		return
	}
	statusResponse, err := server.Status(ctx, &kmsapi.StatusRequest{})
	if err != nil {
		report.Fail(checkStatusEncryptInvariant, "Status/encrypt consistency", safeMessage(err))
		return
	}
	encryptResponse, err := server.Encrypt(ctx, &kmsapi.EncryptRequest{Plaintext: []byte(messageDiagnosticPlaintext)})
	if err != nil {
		report.Fail(checkStatusEncryptInvariant, "Status/encrypt consistency", safeMessage(err))
		return
	}
	if statusResponse.GetKeyId() != encryptResponse.GetKeyId() {
		report.Fail(checkStatusEncryptInvariant, "Status/encrypt consistency", "Status and Encrypt key_id values differ")
		return
	}
	report.Pass(checkStatusEncryptInvariant, "Status/encrypt consistency", "local invariant holds")
}

func diagnosticPluginVersion(info version.Info) string {
	if info.Version == "" {
		return messageDiagnosticVersion
	}
	return info.Version
}

func checkRegistryVersionRestrictions(report *cli.Report, cfg config.Config, profile openbao.KeyProfile) {
	loaded, err := loadRegistryStateWithCheckpoint(cfg.State.Path)
	if errors.Is(err, keyregistry.ErrStateNotFound) {
		assessment := status.AssessAutoBootstrapState(profile)
		report.Warn(
			checkRegistryState,
			"Registry state",
			fmt.Sprintf(
				"state file is absent; auto-bootstrap eligible=%t: %s",
				assessment.Allowed,
				assessment.Reason,
			),
		)
		checkLatestVersionRestrictions(report, profile)
		return
	}
	if err != nil {
		report.Fail(checkRegistryState, "Registry state", safeMessage(err))
		return
	}
	switch loaded.CheckpointStatus {
	case stateCheckpointStatusMissing:
		report.Warn(
			checkRegistryState,
			"Registry state",
			"state file loaded but replay checkpoint is absent; rollback detection is not anchored",
		)
	case stateCheckpointStatusBehind:
		report.Warn(
			checkRegistryState,
			"Registry state",
			"state file loaded but replay checkpoint lags the accepted state generation",
		)
	default:
		report.Pass(checkRegistryState, "Registry state", "loaded local key registry state and checkpoint")
	}
	state := loaded.State
	failures := make([]string, 0)
	for _, record := range state.Snapshots {
		snapshot, snapshotErr := record.Snapshot()
		if snapshotErr != nil {
			failures = append(failures, snapshotErr.Error())
			continue
		}
		switch snapshot.State {
		case keyregistry.StateActive, keyregistry.StateRetired:
			if profile.MinAvailableVersion > snapshot.TransitVersion {
				failures = append(failures, "minimum available version blocks required historical version")
			}
			if profile.MinDecryptionVersion > snapshot.TransitVersion {
				failures = append(failures, "minimum decryption version blocks required historical version")
			}
			if snapshot.State == keyregistry.StateActive && profile.MinEncryptionVersion > snapshot.TransitVersion {
				failures = append(failures, "minimum encryption version blocks active version")
			}
		case keyregistry.StatePending, keyregistry.StateRejected:
		}
	}
	if len(failures) > 0 {
		report.Fail(checkVersionRestrictions, "Transit version restrictions", strings.Join(uniqueStrings(failures), "; "))
		return
	}
	report.Pass(checkVersionRestrictions, "Transit version restrictions", "active and historical versions remain usable")
}

func checkLatestVersionRestrictions(report *cli.Report, profile openbao.KeyProfile) {
	failures := make([]string, 0)
	if profile.MinAvailableVersion > profile.LatestVersion {
		failures = append(failures, "minimum available version blocks latest version")
	}
	if profile.MinEncryptionVersion > profile.LatestVersion {
		failures = append(failures, "minimum encryption version blocks latest version")
	}
	if profile.MinDecryptionVersion > profile.LatestVersion {
		failures = append(failures, "minimum decryption version blocks latest version")
	}
	if len(failures) > 0 {
		report.Fail(checkVersionRestrictions, "Transit version restrictions", strings.Join(failures, "; "))
		return
	}
	report.Pass(checkVersionRestrictions, "Transit version restrictions", "latest Transit version is usable")
}

func snapshotScope(cfg config.Config) status.SnapshotScope {
	return status.SnapshotScope{
		ProviderName:        cfg.Transit.KeyIDScope.ProviderName,
		ClusterID:           cfg.Transit.KeyIDScope.ClusterID,
		OpenBaoInstanceID:   cfg.OpenBao.InstanceID,
		OpenBaoNamespace:    cfg.OpenBao.Namespace,
		TransitMountID:      cfg.Transit.KeyIDScope.TransitMountID,
		TransitKeyLineageID: cfg.Transit.KeyIDScope.KeyLineageID,
		AADMode:             keyregistry.AADModeRequired,
	}
}

func rotationPolicy(cfg config.Config) status.RotationPolicy {
	return status.RotationPolicy{
		ActivationDelay:               cfg.Rotation.ActivationDelay,
		RequireStableObservationCount: cfg.Rotation.RequireStableObservationCount,
		RejectVersionRollback:         cfg.Rotation.RejectVersionRollback,
	}
}

func reportError(report cli.Report, message string) error {
	if !report.HasFailures() {
		return nil
	}
	return cli.WithExitCode(cli.ExitCheckFailed, errors.New(message))
}

func safeMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func randomBytes(size int) ([]byte, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return nil, fmt.Errorf("generate random bytes: %w", err)
	}
	return value, nil
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	return unique
}
