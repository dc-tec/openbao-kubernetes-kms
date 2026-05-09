// Package config owns the Viper-backed configuration boundary.
package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

const (
	defaultConfigVersion        = "v1alpha1"
	defaultSocketPath           = "/run/openbao-kms/kms.sock"
	defaultSocketMode           = "0660"
	defaultMetricsAddress       = "127.0.0.1:8081"
	defaultHealthAddress        = "127.0.0.1:8082"
	defaultOpenBaoTimeout       = 2 * time.Second
	defaultAuthMethod           = "jwt"
	defaultTokenStorage         = "memory"
	defaultMinJWTRemainingTTL   = 2 * time.Minute
	defaultClockSkewLeeway      = 30 * time.Second
	defaultLoginBeforeExpiry    = 5 * time.Minute
	defaultProbeInterval        = 30 * time.Second
	defaultDeepProbeInterval    = 5 * time.Minute
	defaultStatusMaxStaleness   = 2 * time.Minute
	defaultStatePath            = "/var/lib/openbao-kms/state/key-registry.json"
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
	ConfigVersion string            `mapstructure:"configVersion"`
	Server        ServerConfig      `mapstructure:"server"`
	OpenBao       OpenBaoConfig     `mapstructure:"openbao"`
	Auth          AuthConfig        `mapstructure:"auth"`
	Transit       TransitConfig     `mapstructure:"transit"`
	Status        StatusConfig      `mapstructure:"status"`
	State         StateConfig       `mapstructure:"state"`
	Rotation      RotationConfig    `mapstructure:"rotation"`
	Performance   PerformanceConfig `mapstructure:"performance"`
	Logging       LoggingConfig     `mapstructure:"logging"`
}

// ServerConfig contains local listener and socket settings.
type ServerConfig struct {
	SocketPath           string `mapstructure:"socketPath"`
	SocketMode           string `mapstructure:"socketMode"`
	SocketGroup          string `mapstructure:"socketGroup"`
	MetricsAddress       string `mapstructure:"metricsAddress"`
	HealthAddress        string `mapstructure:"healthAddress"`
	AdminAddress         string `mapstructure:"adminAddress"`
	UnsafeDebugEndpoints bool   `mapstructure:"unsafeDebugEndpoints"`
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
	Method                 string        `mapstructure:"method"`
	MountPath              string        `mapstructure:"mountPath"`
	Role                   string        `mapstructure:"role"`
	JWTFile                string        `mapstructure:"jwtFile"`
	MinJWTRemainingTTL     time.Duration `mapstructure:"minJwtRemainingTtl"`
	ClockSkewLeeway        time.Duration `mapstructure:"clockSkewLeeway"`
	LoginBeforeTokenExpiry time.Duration `mapstructure:"loginBeforeTokenExpiry"`
	TokenStorage           string        `mapstructure:"tokenStorage"`
}

// TransitConfig contains Transit key and AAD settings.
type TransitConfig struct {
	MountPath         string           `mapstructure:"mountPath"`
	KeyName           string           `mapstructure:"keyName"`
	KeyIDScope        KeyIDScopeConfig `mapstructure:"keyIdScope"`
	UseAssociatedData bool             `mapstructure:"useAssociatedData"`
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

// PerformanceConfig contains optional performance feature settings.
type PerformanceConfig struct {
	DecryptMicroBatching DecryptMicroBatchingConfig `mapstructure:"decryptMicroBatching"`
}

// DecryptMicroBatchingConfig contains decrypt batch tuning.
type DecryptMicroBatchingConfig struct {
	Enabled      bool          `mapstructure:"enabled"`
	MaxBatchSize int           `mapstructure:"maxBatchSize"`
	MaxWait      time.Duration `mapstructure:"maxWait"`
}

// LoggingConfig contains structured logging settings.
type LoggingConfig struct {
	Level                string `mapstructure:"level"`
	Format               string `mapstructure:"format"`
	RedactOpenBaoPaths   bool   `mapstructure:"redactOpenBaoPaths"`
	LogOpenBaoRequestIDs bool   `mapstructure:"logOpenBaoRequestIDs"`
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
	if err := runtime.v.UnmarshalExact(&cfg, viper.DecodeHook(mapstructure.StringToTimeDurationHookFunc())); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}

	return cfg, nil
}

func applyDefaults(runtime *Runtime) {
	runtime.v.SetDefault("configVersion", defaultConfigVersion)
	runtime.v.SetDefault("server.socketPath", defaultSocketPath)
	runtime.v.SetDefault("server.socketMode", defaultSocketMode)
	runtime.v.SetDefault("server.metricsAddress", defaultMetricsAddress)
	runtime.v.SetDefault("server.healthAddress", defaultHealthAddress)
	runtime.v.SetDefault("server.unsafeDebugEndpoints", false)
	runtime.v.SetDefault("openbao.timeout", defaultOpenBaoTimeout)
	runtime.v.SetDefault("auth.method", defaultAuthMethod)
	runtime.v.SetDefault("auth.tokenStorage", defaultTokenStorage)
	runtime.v.SetDefault("auth.minJwtRemainingTtl", defaultMinJWTRemainingTTL)
	runtime.v.SetDefault("auth.clockSkewLeeway", defaultClockSkewLeeway)
	runtime.v.SetDefault("auth.loginBeforeTokenExpiry", defaultLoginBeforeExpiry)
	runtime.v.SetDefault("transit.useAssociatedData", true)
	runtime.v.SetDefault("status.probeInterval", defaultProbeInterval)
	runtime.v.SetDefault("status.deepProbeInterval", defaultDeepProbeInterval)
	runtime.v.SetDefault("status.statusMaxStaleness", defaultStatusMaxStaleness)
	runtime.v.SetDefault("state.path", defaultStatePath)
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
