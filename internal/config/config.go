// Package config owns the Viper-backed configuration boundary.
package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

const (
	defaultSocketPath           = "/run/openbao-kms/kms.sock"
	defaultSocketMode           = "0660"
	defaultMetricsAddress       = "127.0.0.1:8081"
	defaultHealthAddress        = "127.0.0.1:8082"
	defaultAuthMethod           = "jwt"
	defaultTokenStorage         = "memory"
	defaultProbeInterval        = 30 * time.Second
	defaultDeepProbeInterval    = 5 * time.Minute
	defaultStatusMaxStaleness   = 2 * time.Minute
	defaultRotationMode         = "observed"
	defaultActivationDelay      = 2 * time.Minute
	defaultStableObservation    = 3
	defaultMicroBatchSize       = 32
	defaultMicroBatchMaxWait    = 2 * time.Millisecond
	defaultLogLevel             = "info"
	defaultLogFormat            = "json"
	defaultRedactOpenBaoPaths   = true
	defaultLogOpenBaoRequestIDs = true
)

// Runtime owns the Viper-backed config state at the config boundary.
type Runtime struct {
	v *viper.Viper
}

// LoadOptions controls typed configuration loading.
type LoadOptions struct {
	Path string
}

// Config is the typed provider configuration model.
type Config struct {
	Server      ServerConfig
	OpenBao     OpenBaoConfig
	Auth        AuthConfig
	Transit     TransitConfig
	Status      StatusConfig
	Rotation    RotationConfig
	Performance PerformanceConfig
	Logging     LoggingConfig
}

// ServerConfig contains local listener and socket settings.
type ServerConfig struct {
	SocketPath           string
	SocketMode           string
	SocketGroup          string
	MetricsAddress       string
	HealthAddress        string
	AdminAddress         string
	UnsafeDebugEndpoints bool
}

// OpenBaoConfig contains OpenBao client settings.
type OpenBaoConfig struct {
	Address       string
	Namespace     string
	CACertFile    string
	TLSServerName string
	Timeout       time.Duration
	InstanceID    string
}

// AuthConfig contains OpenBao authentication settings.
type AuthConfig struct {
	Method                 string
	MountPath              string
	Role                   string
	JWTFile                string
	MinJWTRemainingTTL     time.Duration
	LoginBeforeTokenExpiry time.Duration
	TokenStorage           string
}

// TransitConfig contains Transit key and AAD settings.
type TransitConfig struct {
	MountPath         string
	KeyName           string
	KeyIDScope        KeyIDScopeConfig
	UseAssociatedData bool
}

// KeyIDScopeConfig contains identity-bearing key ID scope settings.
type KeyIDScopeConfig struct {
	ProviderName   string
	ClusterID      string
	TransitMountID string
	KeyLineageID   string
}

// StatusConfig contains status probe timing settings.
type StatusConfig struct {
	ProbeInterval      time.Duration
	DeepProbeInterval  time.Duration
	StatusMaxStaleness time.Duration
}

// RotationConfig contains key version observation and promotion settings.
type RotationConfig struct {
	Mode                          string
	ActivationDelay               time.Duration
	RequireStableObservationCount int
	RejectVersionRollback         bool
}

// PerformanceConfig contains optional performance feature settings.
type PerformanceConfig struct {
	DecryptMicroBatching DecryptMicroBatchingConfig
}

// DecryptMicroBatchingConfig contains decrypt batch tuning.
type DecryptMicroBatchingConfig struct {
	Enabled      bool
	MaxBatchSize int
	MaxWait      time.Duration
}

// LoggingConfig contains structured logging settings.
type LoggingConfig struct {
	Level                string
	Format               string
	RedactOpenBaoPaths   bool
	LogOpenBaoRequestIDs bool
}

// NewRuntime returns a config runtime with project defaults.
func NewRuntime() *Runtime {
	v := viper.New()
	v.SetConfigType("yaml")
	v.SetEnvPrefix("BAO_KMS_PROVIDER")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	v.AutomaticEnv()
	runtime := &Runtime{v: v}
	applyDefaults(runtime)
	return runtime
}

// BindRootFlags binds supported root flags into the config runtime.
func BindRootFlags(runtime *Runtime, flags *pflag.FlagSet) error {
	bindings := []struct {
		key  string
		name string
	}{
		{key: "config", name: "config"},
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
	if opts.Path != "" {
		runtime.v.SetConfigFile(opts.Path)
		if err := runtime.v.ReadInConfig(); err != nil {
			return Config{}, fmt.Errorf("read config: %w", err)
		}
	}

	var cfg Config
	if err := runtime.v.Unmarshal(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}

	return cfg, nil
}

func applyDefaults(runtime *Runtime) {
	runtime.v.SetDefault("server.socketPath", defaultSocketPath)
	runtime.v.SetDefault("server.socketMode", defaultSocketMode)
	runtime.v.SetDefault("server.metricsAddress", defaultMetricsAddress)
	runtime.v.SetDefault("server.healthAddress", defaultHealthAddress)
	runtime.v.SetDefault("server.unsafeDebugEndpoints", false)
	runtime.v.SetDefault("auth.method", defaultAuthMethod)
	runtime.v.SetDefault("auth.tokenStorage", defaultTokenStorage)
	runtime.v.SetDefault("transit.useAssociatedData", true)
	runtime.v.SetDefault("status.probeInterval", defaultProbeInterval)
	runtime.v.SetDefault("status.deepProbeInterval", defaultDeepProbeInterval)
	runtime.v.SetDefault("status.statusMaxStaleness", defaultStatusMaxStaleness)
	runtime.v.SetDefault("rotation.mode", defaultRotationMode)
	runtime.v.SetDefault("rotation.activationDelay", defaultActivationDelay)
	runtime.v.SetDefault("rotation.requireStableObservationCount", defaultStableObservation)
	runtime.v.SetDefault("rotation.rejectVersionRollback", true)
	runtime.v.SetDefault("performance.decryptMicroBatching.enabled", false)
	runtime.v.SetDefault("performance.decryptMicroBatching.maxBatchSize", defaultMicroBatchSize)
	runtime.v.SetDefault("performance.decryptMicroBatching.maxWait", defaultMicroBatchMaxWait)
	runtime.v.SetDefault("logging.level", defaultLogLevel)
	runtime.v.SetDefault("logging.format", defaultLogFormat)
	runtime.v.SetDefault("logging.redactOpenBaoPaths", defaultRedactOpenBaoPaths)
	runtime.v.SetDefault("logging.logOpenBaoRequestIDs", defaultLogOpenBaoRequestIDs)
}
