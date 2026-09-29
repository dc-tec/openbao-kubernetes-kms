package scaffold

import (
	"bytes"
	"fmt"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/config"
	"gopkg.in/yaml.v3"
)

// The file types mirror config.Config with YAML tags and duration strings, so
// the rendered file loads back through config.Load unchanged.

type providerConfigFile struct {
	ConfigVersion string            `yaml:"configVersion"`
	Server        serverConfigFile  `yaml:"server"`
	OpenBao       openBaoConfigFile `yaml:"openbao"`
	Auth          authConfigFile    `yaml:"auth"`
	Transit       transitConfigFile `yaml:"transit"`
	Bootstrap     bootstrapFile     `yaml:"bootstrap"`
	Status        statusFile        `yaml:"status"`
	State         stateFile         `yaml:"state"`
	Rotation      rotationFile      `yaml:"rotation"`
	Logging       loggingFile       `yaml:"logging"`
}

type serverConfigFile struct {
	SocketPath           string `yaml:"socketPath"`
	SocketMode           string `yaml:"socketMode"`
	SocketGroup          string `yaml:"socketGroup"`
	MetricsAddress       string `yaml:"metricsAddress"`
	HealthAddress        string `yaml:"healthAddress"`
	MaxConcurrentStatus  int    `yaml:"maxConcurrentStatus"`
	MaxConcurrentEncrypt int    `yaml:"maxConcurrentEncrypt"`
	MaxConcurrentDecrypt int    `yaml:"maxConcurrentDecrypt"`
}

type openBaoConfigFile struct {
	Address       string `yaml:"address"`
	Namespace     string `yaml:"namespace"`
	CACertFile    string `yaml:"caCertFile"`
	TLSServerName string `yaml:"tlsServerName"`
	Timeout       string `yaml:"timeout"`
	InstanceID    string `yaml:"instanceId"`
}

type authConfigFile struct {
	Method                 string        `yaml:"method"`
	LoginBeforeTokenExpiry string        `yaml:"loginBeforeTokenExpiry"`
	TokenRenewalIncrement  string        `yaml:"tokenRenewalIncrement"`
	LoginTimeout           string        `yaml:"loginTimeout"`
	JWT                    *jwtAuthFile  `yaml:"jwt,omitempty"`
	Cert                   *certAuthFile `yaml:"cert,omitempty"`
}

type jwtAuthFile struct {
	Source           string          `yaml:"source"`
	OAuth2           *oauth2AuthFile `yaml:"oauth2,omitempty"`
	MountPath        string          `yaml:"mountPath"`
	Role             string          `yaml:"role"`
	JWTFile          string          `yaml:"jwtFile,omitempty"`
	MinRemainingTTL  string          `yaml:"minRemainingTtl"`
	ClockSkewLeeway  string          `yaml:"clockSkewLeeway"`
	ExpectedIssuer   string          `yaml:"expectedIssuer"`
	ExpectedAudience []string        `yaml:"expectedAudience"`
	ExpectedSubject  string          `yaml:"expectedSubject"`
}

type oauth2AuthFile struct {
	TokenURL         string   `yaml:"tokenUrl"`
	ClientID         string   `yaml:"clientId"`
	ClientSecretFile string   `yaml:"clientSecretFile"`
	AuthMethod       string   `yaml:"authMethod"`
	Scopes           []string `yaml:"scopes,omitempty"`
	Audience         string   `yaml:"audience,omitempty"`
	Resources        []string `yaml:"resources,omitempty"`
	CACertFile       string   `yaml:"caCertFile,omitempty"`
}

type certAuthFile struct {
	MountPath       string         `yaml:"mountPath"`
	Name            string         `yaml:"name"`
	MinRemainingTTL string         `yaml:"minRemainingTtl"`
	ClockSkewLeeway string         `yaml:"clockSkewLeeway"`
	Source          string         `yaml:"source"`
	PKCS11          pkcs11AuthFile `yaml:"pkcs11"`
}

type pkcs11AuthFile struct {
	CertificateFile string `yaml:"certificateFile"`
	ModulePath      string `yaml:"modulePath"`
	TokenLabel      string `yaml:"tokenLabel"`
	KeyLabel        string `yaml:"keyLabel"`
	PINFile         string `yaml:"pinFile"`
	MaxSessions     int    `yaml:"maxSessions"`
}

type transitConfigFile struct {
	MountPath  string         `yaml:"mountPath"`
	KeyName    string         `yaml:"keyName"`
	KeyIDScope keyIDScopeFile `yaml:"keyIdScope"`
}

type keyIDScopeFile struct {
	ProviderName   string `yaml:"providerName"`
	ClusterID      string `yaml:"clusterId"`
	TransitMountID string `yaml:"transitMountId"`
	KeyLineageID   string `yaml:"keyLineageId"`
}

type bootstrapFile struct {
	GraceTimeout  string `yaml:"graceTimeout"`
	RetryInterval string `yaml:"retryInterval"`
}

type statusFile struct {
	ProbeInterval      string `yaml:"probeInterval"`
	DeepProbeInterval  string `yaml:"deepProbeInterval"`
	StatusMaxStaleness string `yaml:"statusMaxStaleness"`
}

type stateFile struct {
	Path string `yaml:"path"`
}

type rotationFile struct {
	Mode                          string `yaml:"mode"`
	ActivationDelay               string `yaml:"activationDelay"`
	RequireStableObservationCount int    `yaml:"requireStableObservationCount"`
	RejectVersionRollback         bool   `yaml:"rejectVersionRollback"`
}

type loggingFile struct {
	Level                string               `yaml:"level"`
	Format               string               `yaml:"format"`
	LogOpenBaoRequestIDs bool                 `yaml:"logOpenBaoRequestIDs"`
	DebugCorrelation     debugCorrelationFile `yaml:"debugCorrelation"`
}

type debugCorrelationFile struct {
	Enabled    bool   `yaml:"enabled"`
	TTL        string `yaml:"ttl"`
	IncidentID string `yaml:"incidentId"`
}

// RenderProviderConfig renders a complete provider configuration file.
func RenderProviderConfig(cfg config.Config) ([]byte, error) {
	file := providerConfigFile{
		ConfigVersion: cfg.ConfigVersion,
		Server: serverConfigFile{
			SocketPath:           cfg.Server.SocketPath,
			SocketMode:           cfg.Server.SocketMode,
			SocketGroup:          cfg.Server.SocketGroup,
			MetricsAddress:       cfg.Server.MetricsAddress,
			HealthAddress:        cfg.Server.HealthAddress,
			MaxConcurrentStatus:  cfg.Server.MaxConcurrentStatus,
			MaxConcurrentEncrypt: cfg.Server.MaxConcurrentEncrypt,
			MaxConcurrentDecrypt: cfg.Server.MaxConcurrentDecrypt,
		},
		OpenBao: openBaoConfigFile{
			Address:       cfg.OpenBao.Address,
			Namespace:     cfg.OpenBao.Namespace,
			CACertFile:    cfg.OpenBao.CACertFile,
			TLSServerName: cfg.OpenBao.TLSServerName,
			Timeout:       formatDuration(cfg.OpenBao.Timeout),
			InstanceID:    cfg.OpenBao.InstanceID,
		},
		Auth: authConfigFile{
			Method:                 string(cfg.Auth.Method),
			LoginBeforeTokenExpiry: formatDuration(cfg.Auth.LoginBeforeTokenExpiry),
			TokenRenewalIncrement:  formatDuration(cfg.Auth.TokenRenewalIncrement),
			LoginTimeout:           formatDuration(cfg.Auth.LoginTimeout),
		},
		Transit: transitConfigFile{
			MountPath: cfg.Transit.MountPath,
			KeyName:   cfg.Transit.KeyName,
			KeyIDScope: keyIDScopeFile{
				ProviderName:   cfg.Transit.KeyIDScope.ProviderName,
				ClusterID:      cfg.Transit.KeyIDScope.ClusterID,
				TransitMountID: cfg.Transit.KeyIDScope.TransitMountID,
				KeyLineageID:   cfg.Transit.KeyIDScope.KeyLineageID,
			},
		},
		Bootstrap: bootstrapFile{
			GraceTimeout:  formatDuration(cfg.Bootstrap.GraceTimeout),
			RetryInterval: formatDuration(cfg.Bootstrap.RetryInterval),
		},
		Status: statusFile{
			ProbeInterval:      formatDuration(cfg.Status.ProbeInterval),
			DeepProbeInterval:  formatDuration(cfg.Status.DeepProbeInterval),
			StatusMaxStaleness: formatDuration(cfg.Status.StatusMaxStaleness),
		},
		State: stateFile{Path: cfg.State.Path},
		Rotation: rotationFile{
			Mode:                          cfg.Rotation.Mode,
			ActivationDelay:               formatDuration(cfg.Rotation.ActivationDelay),
			RequireStableObservationCount: cfg.Rotation.RequireStableObservationCount,
			RejectVersionRollback:         cfg.Rotation.RejectVersionRollback,
		},
		Logging: loggingFile{
			Level:                cfg.Logging.Level,
			Format:               cfg.Logging.Format,
			LogOpenBaoRequestIDs: cfg.Logging.LogOpenBaoRequestIDs,
			DebugCorrelation: debugCorrelationFile{
				Enabled:    cfg.Logging.DebugCorrelation.Enabled,
				TTL:        formatDuration(cfg.Logging.DebugCorrelation.TTL),
				IncidentID: cfg.Logging.DebugCorrelation.IncidentID,
			},
		},
	}

	switch cfg.Auth.Method {
	case config.AuthMethodJWT:
		audience := cfg.Auth.JWT.ExpectedAudience
		if audience == nil {
			audience = []string{}
		}
		file.Auth.JWT = &jwtAuthFile{
			Source:           cfg.Auth.JWT.Source,
			MountPath:        cfg.Auth.JWT.MountPath,
			Role:             cfg.Auth.JWT.Role,
			JWTFile:          cfg.Auth.JWT.JWTFile,
			MinRemainingTTL:  formatDuration(cfg.Auth.JWT.MinRemainingTTL),
			ClockSkewLeeway:  formatDuration(cfg.Auth.JWT.ClockSkewLeeway),
			ExpectedIssuer:   cfg.Auth.JWT.ExpectedIssuer,
			ExpectedAudience: audience,
			ExpectedSubject:  cfg.Auth.JWT.ExpectedSubject,
		}
		if cfg.Auth.JWT.Source == config.JWTSourceOAuth2 {
			oauth := cfg.Auth.JWT.OAuth2
			file.Auth.JWT.OAuth2 = &oauth2AuthFile{
				TokenURL: oauth.TokenURL, ClientID: oauth.ClientID, ClientSecretFile: oauth.ClientSecretFile,
				AuthMethod: oauth.AuthMethod, Scopes: oauth.Scopes, Audience: oauth.Audience,
				Resources: oauth.Resources, CACertFile: oauth.CACertFile,
			}
		}
	case config.AuthMethodCert:
		file.Auth.Cert = &certAuthFile{
			MountPath:       cfg.Auth.Cert.MountPath,
			Name:            cfg.Auth.Cert.Name,
			MinRemainingTTL: formatDuration(cfg.Auth.Cert.MinRemainingTTL),
			ClockSkewLeeway: formatDuration(cfg.Auth.Cert.ClockSkewLeeway),
			Source:          string(cfg.Auth.Cert.Source),
			PKCS11: pkcs11AuthFile{
				CertificateFile: cfg.Auth.Cert.PKCS11.CertificateFile,
				ModulePath:      cfg.Auth.Cert.PKCS11.ModulePath,
				TokenLabel:      cfg.Auth.Cert.PKCS11.TokenLabel,
				KeyLabel:        cfg.Auth.Cert.PKCS11.KeyLabel,
				PINFile:         cfg.Auth.Cert.PKCS11.PINFile,
				MaxSessions:     cfg.Auth.Cert.PKCS11.MaxSessions,
			},
		}
	default:
		return nil, fmt.Errorf("unsupported auth method %q", cfg.Auth.Method)
	}

	return encodeYAML(file)
}

func encodeYAML[T yamlDocument](value T) ([]byte, error) {
	var out bytes.Buffer
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	if err := encoder.Encode(value); err != nil {
		return nil, fmt.Errorf("encode yaml: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("encode yaml: %w", err)
	}
	return out.Bytes(), nil
}

// yamlDocument lists the typed documents this package encodes.
type yamlDocument interface {
	providerConfigFile | encryptionConfigFile | podManifest
}

// formatDuration renders a duration with the largest exact unit, such as 30s,
// 5m, or 1h, which config.Load parses back to the same value.
func formatDuration(value time.Duration) string {
	if value == 0 {
		return "0s"
	}
	for _, unit := range []struct {
		size   time.Duration
		suffix string
	}{
		{time.Hour, "h"},
		{time.Minute, "m"},
		{time.Second, "s"},
		{time.Millisecond, "ms"},
	} {
		if value%unit.size == 0 {
			return fmt.Sprintf("%d%s", value/unit.size, unit.suffix)
		}
	}
	return value.String()
}
