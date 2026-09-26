package scaffold

import (
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/config"
)

const (
	// ContainerUID is the distroless non-root user of the provider image.
	ContainerUID = 65532
	// ProviderConfigPath is where hosts and containers read the provider configuration.
	ProviderConfigPath = "/etc/openbao-kms/config.yaml"
)

var imageDigestPattern = regexp.MustCompile(
	`^[a-z0-9]+([._/-][a-z0-9]+)*(:[0-9]+)?(/[a-z0-9]+([._-][a-z0-9]+)*)*@sha256:[0-9a-f]{64}$`)

// The manifest types cover exactly the Pod fields the maintained static pod
// manifest in deploy/static-pod uses.

type podManifest struct {
	APIVersion string      `yaml:"apiVersion"`
	Kind       string      `yaml:"kind"`
	Metadata   podMetadata `yaml:"metadata"`
	Spec       podSpec     `yaml:"spec"`
}

type podMetadata struct {
	Name      string   `yaml:"name"`
	Namespace string   `yaml:"namespace"`
	Labels    podLabel `yaml:"labels"`
}

type podLabel struct {
	Name      string `yaml:"app.kubernetes.io/name"`
	Component string `yaml:"app.kubernetes.io/component"`
}

type podSpec struct {
	HostNetwork                  bool               `yaml:"hostNetwork"`
	PriorityClassName            string             `yaml:"priorityClassName"`
	AutomountServiceAccountToken bool               `yaml:"automountServiceAccountToken"`
	SecurityContext              podSecurityContext `yaml:"securityContext"`
	Containers                   []podContainer     `yaml:"containers"`
	Volumes                      []podVolume        `yaml:"volumes"`
}

type podSecurityContext struct {
	RunAsNonRoot       bool           `yaml:"runAsNonRoot"`
	RunAsUser          int64          `yaml:"runAsUser"`
	RunAsGroup         int64          `yaml:"runAsGroup"`
	SupplementalGroups []int64        `yaml:"supplementalGroups"`
	SeccompProfile     seccompProfile `yaml:"seccompProfile"`
}

type seccompProfile struct {
	Type string `yaml:"type"`
}

type podContainer struct {
	Name            string                   `yaml:"name"`
	Image           string                   `yaml:"image"`
	ImagePullPolicy string                   `yaml:"imagePullPolicy"`
	Args            []string                 `yaml:"args"`
	Ports           []containerPort          `yaml:"ports"`
	SecurityContext containerSecurityContext `yaml:"securityContext"`
	VolumeMounts    []volumeMount            `yaml:"volumeMounts"`
	StartupProbe    probe                    `yaml:"startupProbe"`
	LivenessProbe   probe                    `yaml:"livenessProbe"`
	ReadinessProbe  probe                    `yaml:"readinessProbe"`
}

type containerPort struct {
	Name          string `yaml:"name"`
	ContainerPort int    `yaml:"containerPort"`
	Protocol      string `yaml:"protocol"`
}

type containerSecurityContext struct {
	AllowPrivilegeEscalation bool         `yaml:"allowPrivilegeEscalation"`
	ReadOnlyRootFilesystem   bool         `yaml:"readOnlyRootFilesystem"`
	Capabilities             capabilities `yaml:"capabilities"`
}

type capabilities struct {
	Drop []string `yaml:"drop"`
}

type volumeMount struct {
	Name      string `yaml:"name"`
	MountPath string `yaml:"mountPath"`
	ReadOnly  bool   `yaml:"readOnly,omitempty"`
}

type probe struct {
	HTTPGet             httpGetAction `yaml:"httpGet"`
	InitialDelaySeconds int           `yaml:"initialDelaySeconds,omitempty"`
	PeriodSeconds       int           `yaml:"periodSeconds"`
	TimeoutSeconds      int           `yaml:"timeoutSeconds,omitempty"`
	FailureThreshold    int           `yaml:"failureThreshold,omitempty"`
}

type httpGetAction struct {
	Host string `yaml:"host"`
	Path string `yaml:"path"`
	Port int    `yaml:"port"`
}

type podVolume struct {
	Name     string         `yaml:"name"`
	HostPath hostPathVolume `yaml:"hostPath"`
}

type hostPathVolume struct {
	Path string `yaml:"path"`
	Type string `yaml:"type"`
}

// StaticPodOptions are the host-specific values of a static pod manifest.
type StaticPodOptions struct {
	// Image is the verified provider image, addressed by digest.
	Image string
	// SocketGID is the numeric host GID of the socket access group.
	SocketGID int64
}

// ValidateImageDigest rejects image references that are not pinned by digest.
func ValidateImageDigest(image string) error {
	if !imageDigestPattern.MatchString(image) {
		return errors.New("image must be a repository reference pinned by @sha256 digest, without a tag")
	}
	return nil
}

// RenderStaticPod renders the provider static pod manifest for a JWT-auth
// configuration.
func RenderStaticPod(cfg config.Config, opts StaticPodOptions) ([]byte, error) {
	manifest, err := buildStaticPod(cfg, opts)
	if err != nil {
		return nil, err
	}
	return encodeYAML(manifest)
}

func buildStaticPod(cfg config.Config, opts StaticPodOptions) (podManifest, error) {
	if cfg.Auth.Method != authMethodJWT {
		return podManifest{}, errors.New(
			"static pod manifests are generated for JWT auth only; mount certificate auth material by hand")
	}
	if err := ValidateImageDigest(opts.Image); err != nil {
		return podManifest{}, err
	}
	if opts.SocketGID <= 0 {
		return podManifest{}, errors.New("socket GID must be a positive number")
	}
	metricsPort, err := addressPort(cfg.Server.MetricsAddress)
	if err != nil {
		return podManifest{}, fmt.Errorf("server.metricsAddress: %w", err)
	}
	healthPort, err := addressPort(cfg.Server.HealthAddress)
	if err != nil {
		return podManifest{}, fmt.Errorf("server.healthAddress: %w", err)
	}
	healthHost := probeHost(cfg.Server.HealthAddress)

	tlsDir := filepath.Dir(cfg.OpenBao.CACertFile)
	runDir := filepath.Dir(cfg.Server.SocketPath)
	stateDir := filepath.Dir(cfg.State.Path)

	return podManifest{
		APIVersion: "v1",
		Kind:       "Pod",
		Metadata: podMetadata{
			Name:      "bao-kms-provider",
			Namespace: "kube-system",
			Labels:    podLabel{Name: "bao-kms-provider", Component: "kms-provider"},
		},
		Spec: podSpec{
			HostNetwork:                  true,
			PriorityClassName:            "system-node-critical",
			AutomountServiceAccountToken: false,
			SecurityContext: podSecurityContext{
				RunAsNonRoot:       true,
				RunAsUser:          ContainerUID,
				RunAsGroup:         ContainerUID,
				SupplementalGroups: []int64{opts.SocketGID},
				SeccompProfile:     seccompProfile{Type: "RuntimeDefault"},
			},
			Containers: []podContainer{{
				Name:            "bao-kms-provider",
				Image:           opts.Image,
				ImagePullPolicy: "IfNotPresent",
				Args:            []string{"serve", "--config=" + ProviderConfigPath},
				Ports: []containerPort{
					{Name: "metrics", ContainerPort: metricsPort, Protocol: "TCP"},
					{Name: "health", ContainerPort: healthPort, Protocol: "TCP"},
				},
				SecurityContext: containerSecurityContext{
					AllowPrivilegeEscalation: false,
					ReadOnlyRootFilesystem:   true,
					Capabilities:             capabilities{Drop: []string{"ALL"}},
				},
				VolumeMounts: []volumeMount{
					{Name: "config", MountPath: ProviderConfigPath, ReadOnly: true},
					{Name: "tls", MountPath: tlsDir, ReadOnly: true},
					{Name: "jwt", MountPath: cfg.Auth.JWT.JWTFile, ReadOnly: true},
					{Name: "run", MountPath: runDir},
					{Name: "state", MountPath: stateDir},
				},
				StartupProbe: probe{
					HTTPGet:          httpGetAction{Host: healthHost, Path: "/live", Port: healthPort},
					PeriodSeconds:    5,
					TimeoutSeconds:   1,
					FailureThreshold: 24,
				},
				LivenessProbe: probe{
					HTTPGet:             httpGetAction{Host: healthHost, Path: "/live", Port: healthPort},
					InitialDelaySeconds: 5,
					PeriodSeconds:       10,
				},
				ReadinessProbe: probe{
					HTTPGet:             httpGetAction{Host: healthHost, Path: "/ready", Port: healthPort},
					InitialDelaySeconds: 5,
					PeriodSeconds:       10,
				},
			}},
			Volumes: []podVolume{
				{Name: "config", HostPath: hostPathVolume{Path: ProviderConfigPath, Type: "File"}},
				{Name: "tls", HostPath: hostPathVolume{Path: tlsDir, Type: "Directory"}},
				{Name: "jwt", HostPath: hostPathVolume{Path: cfg.Auth.JWT.JWTFile, Type: "File"}},
				{Name: "run", HostPath: hostPathVolume{Path: runDir, Type: "Directory"}},
				{Name: "state", HostPath: hostPathVolume{Path: stateDir, Type: "Directory"}},
			},
		},
	}, nil
}

func addressPort(address string) (int, error) {
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return 0, err
	}
	value, err := strconv.Atoi(port)
	if err != nil || value <= 0 || value > 65535 {
		return 0, fmt.Errorf("invalid port %q", port)
	}
	return value, nil
}

// probeHost keeps kubelet probes on loopback unless the health listener binds
// a specific non-wildcard host.
func probeHost(address string) string {
	host, _, err := net.SplitHostPort(address)
	if err != nil || host == "" || host == "0.0.0.0" || host == "::" {
		return "127.0.0.1"
	}
	return host
}
