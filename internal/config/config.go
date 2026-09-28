// Package config owns the Viper-backed configuration boundary.
package config

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/oauth2"
	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

const (
	envLogLevel                      = "BAO_KMS_PROVIDER_LOG_LEVEL"
	envLoggingLevel                  = "BAO_KMS_PROVIDER_LOGGING_LEVEL"
	envServerHealthAddress           = "BAO_KMS_PROVIDER_SERVER_HEALTH_ADDRESS"
	envServerHealthAddressCanonical  = "BAO_KMS_PROVIDER_SERVER_HEALTHADDRESS"
	envServerMetricsAddress          = "BAO_KMS_PROVIDER_SERVER_METRICS_ADDRESS"
	envServerMetricsAddressCanonical = "BAO_KMS_PROVIDER_SERVER_METRICSADDRESS"

	defaultConfigVersion          = "v1alpha1"
	defaultSocketPath             = "/run/openbao-kms/kms.sock"
	defaultSocketMode             = "0660"
	defaultMetricsAddress         = "127.0.0.1:8081"
	defaultHealthAddress          = "127.0.0.1:8082"
	defaultMaxConcurrentStatus    = 16
	defaultMaxConcurrentEncrypt   = 32
	defaultMaxConcurrentDecrypt   = 64
	defaultOpenBaoTimeout         = 2 * time.Second
	defaultAuthMethod             = "jwt"
	defaultMinJWTRemainingTTL     = 2 * time.Minute
	defaultMinCertRemainingTTL    = 24 * time.Hour
	defaultClockSkewLeeway        = 30 * time.Second
	defaultLoginBeforeExpiry      = 5 * time.Minute
	defaultTokenRenewalIncrement  = time.Hour
	defaultBootstrapGraceTimeout  = time.Minute
	defaultBootstrapRetryInterval = 5 * time.Second
	defaultProbeInterval          = 30 * time.Second
	defaultDeepProbeInterval      = 5 * time.Minute
	defaultStatusMaxStaleness     = 2 * time.Minute
	defaultStatePath              = "/var/lib/openbao-kms/state/key-registry.json"
	defaultRotationMode           = "observed"
	defaultActivationDelay        = 2 * time.Minute
	defaultStableObservation      = 3
	defaultLogLevel               = "info"
	defaultLogFormat              = "json"
	defaultLogOpenBaoRequestIDs   = true
	defaultDebugCorrelationTTL    = 15 * time.Minute
)

// Runtime owns the Viper-backed config state at the config boundary.
type Runtime struct {
	v *viper.Viper
}

// LoadOptions controls typed configuration loading.
type LoadOptions struct {
	Path string
	// Content, when set, is decoded as the configuration file instead of Path.
	Content []byte
}

// Config is the typed provider configuration model.
type Config struct {
	ConfigVersion string          `mapstructure:"configVersion"`
	Server        ServerConfig    `mapstructure:"server"`
	OpenBao       OpenBaoConfig   `mapstructure:"openbao"`
	Auth          AuthConfig      `mapstructure:"auth"`
	Transit       TransitConfig   `mapstructure:"transit"`
	Bootstrap     BootstrapConfig `mapstructure:"bootstrap"`
	Status        StatusConfig    `mapstructure:"status"`
	State         StateConfig     `mapstructure:"state"`
	Rotation      RotationConfig  `mapstructure:"rotation"`
	Logging       LoggingConfig   `mapstructure:"logging"`
}

// ServerConfig contains local listener and socket settings.
type ServerConfig struct {
	SocketPath           string `mapstructure:"socketPath"`
	SocketMode           string `mapstructure:"socketMode"`
	SocketGroup          string `mapstructure:"socketGroup"`
	MetricsAddress       string `mapstructure:"metricsAddress"`
	HealthAddress        string `mapstructure:"healthAddress"`
	MaxConcurrentStatus  int    `mapstructure:"maxConcurrentStatus"`
	MaxConcurrentEncrypt int    `mapstructure:"maxConcurrentEncrypt"`
	MaxConcurrentDecrypt int    `mapstructure:"maxConcurrentDecrypt"`
}

// OpenBaoConfig contains OpenBao client settings.
type OpenBaoConfig struct {
	Address       string        `mapstructure:"address"`
	Namespace     string        `mapstructure:"namespace"`
	CACertFile    string        `mapstructure:"caCertFile"`
	TLSServerName string        `mapstructure:"tlsServerName"`
	Timeout       time.Duration `mapstructure:"timeout"`
	InstanceID    string        `mapstructure:"instanceId"`
}

// AuthConfig contains OpenBao authentication settings.
type AuthConfig struct {
	Method                 string         `mapstructure:"method"`
	LoginBeforeTokenExpiry time.Duration  `mapstructure:"loginBeforeTokenExpiry"`
	TokenRenewalIncrement  time.Duration  `mapstructure:"tokenRenewalIncrement"`
	LoginTimeout           time.Duration  `mapstructure:"loginTimeout"`
	JWT                    JWTAuthConfig  `mapstructure:"jwt"`
	Cert                   CertAuthConfig `mapstructure:"cert"`
}

// JWTAuthConfig contains OpenBao JWT auth settings.
type JWTAuthConfig struct {
	Source           string        `mapstructure:"source"`
	OAuth2           OAuth2Config  `mapstructure:"oauth2"`
	MountPath        string        `mapstructure:"mountPath"`
	Role             string        `mapstructure:"role"`
	JWTFile          string        `mapstructure:"jwtFile"`
	MinRemainingTTL  time.Duration `mapstructure:"minRemainingTtl"`
	ClockSkewLeeway  time.Duration `mapstructure:"clockSkewLeeway"`
	ExpectedIssuer   string        `mapstructure:"expectedIssuer"`
	ExpectedAudience []string      `mapstructure:"expectedAudience"`
	ExpectedSubject  string        `mapstructure:"expectedSubject"`
}

// JWT source names select how the provider obtains its OpenBao login credential.
const (
	JWTSourceFile   = "file"
	JWTSourceOAuth2 = "oauth2"
)

// OAuth2Config contains provider-independent client credentials grant settings.
type OAuth2Config struct {
	TokenURL         string   `mapstructure:"tokenUrl"`
	ClientID         string   `mapstructure:"clientId"`
	ClientSecretFile string   `mapstructure:"clientSecretFile"`
	AuthMethod       string   `mapstructure:"authMethod"`
	Scopes           []string `mapstructure:"scopes"`
	Audience         string   `mapstructure:"audience"`
	Resources        []string `mapstructure:"resources"`
	CACertFile       string   `mapstructure:"caCertFile"`
}

// ClientConfig applies the shared login deadline to the OAuth transport settings.
func (c OAuth2Config) ClientConfig(timeout time.Duration) oauth2.Config {
	return oauth2.Config{
		TokenURL: c.TokenURL, ClientID: c.ClientID, ClientSecretFile: c.ClientSecretFile,
		AuthMethod: c.AuthMethod, Scopes: c.Scopes, Audience: c.Audience, Resources: c.Resources,
		CACertFile: c.CACertFile, Timeout: timeout,
	}
}

// CertAuthConfig contains OpenBao certificate auth settings.
type CertAuthConfig struct {
	MountPath       string               `mapstructure:"mountPath"`
	Name            string               `mapstructure:"name"`
	MinRemainingTTL time.Duration        `mapstructure:"minRemainingTtl"`
	ClockSkewLeeway time.Duration        `mapstructure:"clockSkewLeeway"`
	Source          string               `mapstructure:"source"`
	PKCS11          PKCS11CertAuthConfig `mapstructure:"pkcs11"`
	SPIFFE          SPIFFECertAuthConfig `mapstructure:"spiffe"`
}

// PKCS11CertAuthConfig contains PKCS#11-backed certificate auth settings.
type PKCS11CertAuthConfig struct {
	CertificateFile string `mapstructure:"certificateFile"`
	ModulePath      string `mapstructure:"modulePath"`
	TokenLabel      string `mapstructure:"tokenLabel"`
	KeyLabel        string `mapstructure:"keyLabel"`
	PINFile         string `mapstructure:"pinFile"`
	MaxSessions     int    `mapstructure:"maxSessions"`
}

// SPIFFECertAuthConfig contains SPIFFE Workload API certificate auth settings.
type SPIFFECertAuthConfig struct {
	WorkloadAPISocket string `mapstructure:"workloadAPISocket"`
	SPIFFEID          string `mapstructure:"spiffeID"`
	TrustDomain       string `mapstructure:"trustDomain"`
}

// TransitConfig contains Transit key and AAD settings.
type TransitConfig struct {
	MountPath  string           `mapstructure:"mountPath"`
	KeyName    string           `mapstructure:"keyName"`
	KeyIDScope KeyIDScopeConfig `mapstructure:"keyIdScope"`
}

// BootstrapConfig contains fail-fast startup probe grace settings.
type BootstrapConfig struct {
	GraceTimeout  time.Duration `mapstructure:"graceTimeout"`
	RetryInterval time.Duration `mapstructure:"retryInterval"`
}

// KeyIDScopeConfig contains identity-bearing key ID scope settings.
type KeyIDScopeConfig struct {
	ProviderName   string `mapstructure:"providerName"`
	ClusterID      string `mapstructure:"clusterId"`
	TransitMountID string `mapstructure:"transitMountId"`
	KeyLineageID   string `mapstructure:"keyLineageId"`
}

// StatusConfig contains status probe timing settings.
type StatusConfig struct {
	ProbeInterval      time.Duration `mapstructure:"probeInterval"`
	DeepProbeInterval  time.Duration `mapstructure:"deepProbeInterval"`
	StatusMaxStaleness time.Duration `mapstructure:"statusMaxStaleness"`
}

// StateConfig contains local non-secret state file settings.
type StateConfig struct {
	Path string `mapstructure:"path"`
}

// RotationConfig contains key version observation and promotion settings.
type RotationConfig struct {
	Mode                          string        `mapstructure:"mode"`
	ActivationDelay               time.Duration `mapstructure:"activationDelay"`
	RequireStableObservationCount int           `mapstructure:"requireStableObservationCount"`
	RejectVersionRollback         bool          `mapstructure:"rejectVersionRollback"`
}

// LoggingConfig contains structured logging settings.
type LoggingConfig struct {
	Level                string                 `mapstructure:"level"`
	Format               string                 `mapstructure:"format"`
	LogOpenBaoRequestIDs bool                   `mapstructure:"logOpenBaoRequestIDs"`
	DebugCorrelation     DebugCorrelationConfig `mapstructure:"debugCorrelation"`
}

// DebugCorrelationConfig controls temporary incident-response correlation fields.
type DebugCorrelationConfig struct {
	Enabled    bool          `mapstructure:"enabled"`
	TTL        time.Duration `mapstructure:"ttl"`
	IncidentID string        `mapstructure:"incidentId"`
}

// NewRuntime returns a config runtime with project defaults.
func NewRuntime() *Runtime {
	runtime := NewFileRuntime()
	bindAllowedEnv(runtime.v)
	return runtime
}

// NewFileRuntime uses file values and defaults without ambient environment overrides.
// Installation generators use it to produce the same output on different hosts.
func NewFileRuntime() *Runtime {
	v := viper.New()
	v.SetConfigType("yaml")
	v.SetEnvPrefix("BAO_KMS_PROVIDER")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	runtime := &Runtime{v: v}
	applyDefaults(runtime)
	return runtime
}

func bindAllowedEnv(v *viper.Viper) {
	envBindings := []struct {
		key  string
		envs []string
	}{
		{key: "logging.level", envs: []string{envLogLevel, envLoggingLevel}},
		{key: "server.metricsAddress", envs: []string{
			envServerMetricsAddress,
			envServerMetricsAddressCanonical,
		}},
		{key: "server.healthAddress", envs: []string{
			envServerHealthAddress,
			envServerHealthAddressCanonical,
		}},
	}
	for _, binding := range envBindings {
		args := append([]string{binding.key}, binding.envs...)
		_ = v.BindEnv(args...)
	}
}

// BindRootFlags binds supported root flags into the config runtime.
func BindRootFlags(runtime *Runtime, flags *pflag.FlagSet) error {
	bindings := []struct {
		key  string
		name string
	}{
		{key: "logging.level", name: "log-level"},
		{key: "server.metricsAddress", name: "metrics-address"},
		{key: "server.healthAddress", name: "health-address"},
	}

	for _, binding := range bindings {
		flag := flags.Lookup(binding.name)
		if flag == nil {
			return fmt.Errorf("missing flag %q", binding.name)
		}
		if err := runtime.v.BindPFlag(binding.key, flag); err != nil {
			return err
		}
	}
	return nil
}

// Load reads optional file config and decodes it into typed configuration.
func Load(runtime *Runtime, opts LoadOptions) (Config, error) {
	switch {
	case opts.Content != nil:
		if err := runtime.v.ReadConfig(bytes.NewReader(opts.Content)); err != nil {
			return Config{}, fmt.Errorf("read config: %w", err)
		}
	case opts.Path != "":
		runtime.v.SetConfigFile(opts.Path)
		if err := runtime.v.ReadInConfig(); err != nil {
			return Config{}, fmt.Errorf("read config: %w", err)
		}
	}
	if (opts.Content != nil || opts.Path != "") && !runtime.v.InConfig("configVersion") {
		return Config{}, fmt.Errorf("decode config: configVersion is required")
	}

	if err := validateDurationInputTypes(runtime.v, reflect.TypeFor[Config](), ""); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	var cfg Config
	if err := runtime.v.UnmarshalExact(&cfg,
		viper.DecodeHook(mapstructure.StringToTimeDurationHookFunc()),
		func(decoder *mapstructure.DecoderConfig) { decoder.WeaklyTypedInput = false },
	); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	if legacyFileJWT(runtime, cfg) {
		cfg.Auth.JWT.Source = JWTSourceFile
	}

	return cfg, nil
}

// Before OAuth support, jwtFile selected the only JWT source. Explicit source
// values (including null) and OAuth settings must never trigger file inference.
func legacyFileJWT(runtime *Runtime, cfg Config) bool {
	if cfg.Auth.Method != "jwt" || cfg.Auth.JWT.JWTFile == "" {
		return false
	}
	if runtime.v.InConfig("auth.jwt.oauth2") {
		return false
	}
	for _, key := range runtime.v.AllKeys() {
		if key == "auth.jwt.source" || key == "auth.jwt.oauth2" || strings.HasPrefix(key, "auth.jwt.oauth2.") {
			return false
		}
	}
	return true
}

// validateDurationInputTypes rejects numeric durations before mapstructure can
// interpret them as nanoseconds, even with WeaklyTypedInput disabled. Native
// time.Duration values come from defaults; file values must be strings.
func validateDurationInputTypes(v *viper.Viper, configType reflect.Type, prefix string) error {
	for index := range configType.NumField() {
		field := configType.Field(index)
		key := prefix + field.Tag.Get("mapstructure")
		switch {
		case field.Type == reflect.TypeFor[time.Duration]():
			switch v.Get(key).(type) {
			case nil, string, time.Duration:
			default:
				return fmt.Errorf("%s must be a duration string with units, such as 30s or 0s", key)
			}
		case field.Type.Kind() == reflect.Struct:
			if err := validateDurationInputTypes(v, field.Type, key+"."); err != nil {
				return err
			}
		}
	}
	return nil
}

func applyDefaults(runtime *Runtime) {
	runtime.v.SetDefault("configVersion", defaultConfigVersion)
	runtime.v.SetDefault("server.socketPath", defaultSocketPath)
	runtime.v.SetDefault("server.socketMode", defaultSocketMode)
	runtime.v.SetDefault("server.metricsAddress", defaultMetricsAddress)
	runtime.v.SetDefault("server.healthAddress", defaultHealthAddress)
	runtime.v.SetDefault("server.maxConcurrentStatus", defaultMaxConcurrentStatus)
	runtime.v.SetDefault("server.maxConcurrentEncrypt", defaultMaxConcurrentEncrypt)
	runtime.v.SetDefault("server.maxConcurrentDecrypt", defaultMaxConcurrentDecrypt)
	runtime.v.SetDefault("openbao.timeout", defaultOpenBaoTimeout)
	runtime.v.SetDefault("auth.method", defaultAuthMethod)
	runtime.v.SetDefault("auth.jwt.minRemainingTtl", defaultMinJWTRemainingTTL)
	runtime.v.SetDefault("auth.jwt.clockSkewLeeway", defaultClockSkewLeeway)
	runtime.v.SetDefault("auth.loginBeforeTokenExpiry", defaultLoginBeforeExpiry)
	runtime.v.SetDefault("auth.tokenRenewalIncrement", defaultTokenRenewalIncrement)
	runtime.v.SetDefault("auth.cert.minRemainingTtl", defaultMinCertRemainingTTL)
	runtime.v.SetDefault("auth.cert.clockSkewLeeway", defaultClockSkewLeeway)
	runtime.v.SetDefault("bootstrap.graceTimeout", defaultBootstrapGraceTimeout)
	runtime.v.SetDefault("bootstrap.retryInterval", defaultBootstrapRetryInterval)
	runtime.v.SetDefault("status.probeInterval", defaultProbeInterval)
	runtime.v.SetDefault("status.deepProbeInterval", defaultDeepProbeInterval)
	runtime.v.SetDefault("status.statusMaxStaleness", defaultStatusMaxStaleness)
	runtime.v.SetDefault("state.path", defaultStatePath)
	runtime.v.SetDefault("rotation.mode", defaultRotationMode)
	runtime.v.SetDefault("rotation.activationDelay", defaultActivationDelay)
	runtime.v.SetDefault("rotation.requireStableObservationCount", defaultStableObservation)
	runtime.v.SetDefault("rotation.rejectVersionRollback", true)
	runtime.v.SetDefault("logging.level", defaultLogLevel)
	runtime.v.SetDefault("logging.format", defaultLogFormat)
	runtime.v.SetDefault("logging.logOpenBaoRequestIDs", defaultLogOpenBaoRequestIDs)
	runtime.v.SetDefault("logging.debugCorrelation.enabled", false)
	runtime.v.SetDefault("logging.debugCorrelation.ttl", defaultDebugCorrelationTTL)
}
