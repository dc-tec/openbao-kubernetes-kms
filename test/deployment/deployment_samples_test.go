package deployment_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/config"
	"gopkg.in/yaml.v3"
)

func TestProviderAndEncryptionSamplesValidate(t *testing.T) {
	systemdConfig := loadProviderConfig(t, "deploy/config/provider-systemd.yaml")
	staticPodConfig := loadProviderConfig(t, "deploy/config/provider-static-pod.yaml")

	for name, cfg := range map[string]config.Config{
		"systemd":    systemdConfig,
		"static pod": staticPodConfig,
	} {
		if err := config.Validate(cfg, config.ValidationOptions{}); err != nil {
			t.Fatalf("validate %s provider config: %v", name, err)
		}
	}

	encryptionConfig, err := config.LoadEncryptionConfiguration(repoPath("deploy/kubernetes/encryption-config.yaml"))
	if err != nil {
		t.Fatalf("load encryption config sample: %v", err)
	}
	if _, err := config.ValidateEncryptionConfiguration(
		systemdConfig,
		encryptionConfig,
		config.EncryptionValidationOptions{AllowIdentityFallback: true},
	); err != nil {
		t.Fatalf("validate encryption config sample: %v", err)
	}
	if staticPodConfig.Server.SocketGroup != "1234" {
		t.Fatalf("static pod config should use numeric socket group, got %q", staticPodConfig.Server.SocketGroup)
	}
}

func TestStaticPodManifestIsHostOnlyAndNonRoot(t *testing.T) {
	var pod podManifest
	decodeYAML(t, "deploy/static-pod/bao-kms-provider.yaml", &pod)

	requirePodBasics(t, pod)
	requirePodSecurity(t, pod.Spec.SecurityContext)
	requireContainer(t, pod.Spec.Containers)
	requireHostPathVolumes(t, pod.Spec.Volumes)
}

func requirePodBasics(t *testing.T, pod podManifest) {
	t.Helper()

	if pod.APIVersion != "v1" || pod.Kind != "Pod" {
		t.Fatalf("unexpected manifest type: %s/%s", pod.APIVersion, pod.Kind)
	}
	if pod.Metadata.Namespace != "kube-system" {
		t.Fatalf("unexpected namespace: %q", pod.Metadata.Namespace)
	}
	if pod.Spec.ServiceAccountName != "" {
		t.Fatalf("static pod must not set serviceAccountName: %q", pod.Spec.ServiceAccountName)
	}
	if pod.Spec.AutomountServiceAccountToken == nil || *pod.Spec.AutomountServiceAccountToken {
		t.Fatal("static pod must disable automountServiceAccountToken")
	}
	if !pod.Spec.HostNetwork {
		t.Fatal("static pod should use hostNetwork to avoid CNI bootstrap dependency")
	}
}

func requirePodSecurity(t *testing.T, securityContext podSecurityContext) {
	t.Helper()

	if securityContext.RunAsNonRoot == nil || !*securityContext.RunAsNonRoot {
		t.Fatal("pod security context must run as non-root")
	}
	if securityContext.RunAsUser != 65532 || securityContext.RunAsGroup != 65532 {
		t.Fatalf(
			"unexpected pod uid/gid: %d/%d",
			securityContext.RunAsUser,
			securityContext.RunAsGroup,
		)
	}
	if len(securityContext.SupplementalGroups) != 1 ||
		securityContext.SupplementalGroups[0] != 1234 {
		t.Fatalf("unexpected supplemental groups: %#v", securityContext.SupplementalGroups)
	}
	if securityContext.SeccompProfile.Type != "RuntimeDefault" {
		t.Fatalf("unexpected seccomp profile: %s", securityContext.SeccompProfile.Type)
	}
}

func requireContainer(t *testing.T, containers []container) {
	t.Helper()

	if len(containers) != 1 {
		t.Fatalf("expected one container, got %d", len(containers))
	}
	container := containers[0]
	if strings.Contains(container.Image, ":latest") {
		t.Fatalf("static pod image must not use latest: %s", container.Image)
	}
	if !strings.Contains(container.Image, "@sha256:") {
		t.Fatalf("static pod image must be digest-addressed: %s", container.Image)
	}
	if container.ImagePullPolicy == "Always" {
		t.Fatal("static pod should not use Always image pulls")
	}
	if container.SecurityContext.AllowPrivilegeEscalation == nil ||
		*container.SecurityContext.AllowPrivilegeEscalation {
		t.Fatal("container must disable privilege escalation")
	}
	if container.SecurityContext.ReadOnlyRootFilesystem == nil ||
		!*container.SecurityContext.ReadOnlyRootFilesystem {
		t.Fatal("container must use a read-only root filesystem")
	}
	if len(container.SecurityContext.Capabilities.Drop) != 1 ||
		container.SecurityContext.Capabilities.Drop[0] != "ALL" {
		t.Fatalf("container must drop all capabilities: %#v", container.SecurityContext.Capabilities.Drop)
	}
	requireVolumeMounts(t, container.VolumeMounts)
}

func TestDockerfileUsesPinnedDistrolessNonRootRuntime(t *testing.T) {
	content := readSample(t, "Dockerfile")
	required := []string{
		"# syntax=docker/dockerfile:1.7@sha256:",
		"docker.io/library/golang:1.26.3-bookworm@sha256:",
		"gcr.io/distroless/static-debian12:nonroot@sha256:",
		"CGO_ENABLED=0",
		"USER 65532:65532",
		`ENTRYPOINT ["/bao-kms-provider"]`,
	}
	for _, want := range required {
		if !strings.Contains(content, want) {
			t.Fatalf("Dockerfile missing %q", want)
		}
	}
	if strings.Contains(content, ":latest") {
		t.Fatal("Dockerfile must not reference latest tags")
	}
}

func TestSystemdAndPackageSamplesUseResolvedIdentity(t *testing.T) {
	unit := readSample(t, "deploy/systemd/bao-kms-provider.service")
	requiredUnitLines := []string{
		"User=openbao-kms",
		"Group=openbao-kms",
		"SupplementaryGroups=openbao-kms-socket",
		"ExecStart=/usr/local/bin/bao-kms-provider serve --config /etc/openbao-kms/config.yaml",
		"ReadWritePaths=/run/openbao-kms /var/lib/openbao-kms/state",
		"NoNewPrivileges=true",
	}
	for _, want := range requiredUnitLines {
		if !strings.Contains(unit, want) {
			t.Fatalf("systemd unit missing %q", want)
		}
	}

	sysusers := readSample(t, "deploy/package/linux/sysusers.d/openbao-kms.conf")
	for _, want := range []string{
		"g openbao-kms - -",
		"g openbao-kms-socket - -",
		"u openbao-kms -",
		"m openbao-kms openbao-kms-socket",
	} {
		if !strings.Contains(sysusers, want) {
			t.Fatalf("sysusers sample missing %q", want)
		}
	}

	tmpfiles := readSample(t, "deploy/package/linux/tmpfiles.d/openbao-kms.conf")
	for _, want := range []string{
		"d /run/openbao-kms 2770 openbao-kms openbao-kms-socket -",
		"d /var/lib/openbao-kms/state 0750 openbao-kms openbao-kms -",
	} {
		if !strings.Contains(tmpfiles, want) {
			t.Fatalf("tmpfiles sample missing %q", want)
		}
	}
}

func loadProviderConfig(t *testing.T, name string) config.Config {
	t.Helper()

	cfg, err := config.Load(config.NewRuntime(), config.LoadOptions{Path: repoPath(name)})
	if err != nil {
		t.Fatalf("load provider config %s: %v", name, err)
	}
	return cfg
}

func decodeYAML(t *testing.T, name string, out interface{}) {
	t.Helper()

	file, err := os.Open(repoPath(name))
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}
	defer func() {
		_ = file.Close()
	}()
	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	if err := decoder.Decode(out); err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
}

func requireHostPathVolumes(t *testing.T, volumes []volume) {
	t.Helper()

	wantTypes := map[string]string{
		"config": "File",
		"tls":    "Directory",
		"jwt":    "File",
		"run":    "Directory",
		"state":  "Directory",
	}
	wantPaths := map[string]string{
		"config": "/etc/openbao-kms/config.yaml",
		"tls":    "/etc/openbao-kms/tls",
		"jwt":    "/var/lib/openbao-kms/identity.jwt",
		"run":    "/run/openbao-kms",
		"state":  "/var/lib/openbao-kms/state",
	}
	if len(volumes) != len(wantTypes) {
		t.Fatalf("unexpected volume count: %d", len(volumes))
	}
	for _, volume := range volumes {
		if volume.HostPath == nil {
			t.Fatalf("static pod volume %q must be hostPath-only", volume.Name)
		}
		if volume.ConfigMap != nil || volume.Secret != nil || volume.Projected != nil {
			t.Fatalf("static pod volume %q references Kubernetes API objects", volume.Name)
		}
		if got := volume.HostPath.Type; got != wantTypes[volume.Name] {
			t.Fatalf("unexpected hostPath type for %s: %s", volume.Name, got)
		}
		if got := volume.HostPath.Path; got != wantPaths[volume.Name] {
			t.Fatalf("unexpected hostPath path for %s: %s", volume.Name, got)
		}
	}
}

func requireVolumeMounts(t *testing.T, mounts []volumeMount) {
	t.Helper()

	wantReadOnly := map[string]bool{
		"config": true,
		"tls":    true,
		"jwt":    true,
		"run":    false,
		"state":  false,
	}
	if len(mounts) != len(wantReadOnly) {
		t.Fatalf("unexpected volumeMount count: %d", len(mounts))
	}
	for _, mount := range mounts {
		want, ok := wantReadOnly[mount.Name]
		if !ok {
			t.Fatalf("unexpected volumeMount: %s", mount.Name)
		}
		if mount.ReadOnly != want {
			t.Fatalf("unexpected readOnly for %s: %t", mount.Name, mount.ReadOnly)
		}
	}
}

func readSample(t *testing.T, name string) string {
	t.Helper()

	content, err := os.ReadFile(repoPath(name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(content)
}

func repoPath(parts ...string) string {
	return filepath.Join(append([]string{"..", ".."}, parts...)...)
}

type podManifest struct {
	APIVersion string   `yaml:"apiVersion"`
	Kind       string   `yaml:"kind"`
	Metadata   metadata `yaml:"metadata"`
	Spec       podSpec  `yaml:"spec"`
}

type metadata struct {
	Name      string            `yaml:"name"`
	Namespace string            `yaml:"namespace"`
	Labels    map[string]string `yaml:"labels"`
}

type podSpec struct {
	HostNetwork                  bool               `yaml:"hostNetwork"`
	PriorityClassName            string             `yaml:"priorityClassName"`
	AutomountServiceAccountToken *bool              `yaml:"automountServiceAccountToken"`
	ServiceAccountName           string             `yaml:"serviceAccountName"`
	SecurityContext              podSecurityContext `yaml:"securityContext"`
	Containers                   []container        `yaml:"containers"`
	Volumes                      []volume           `yaml:"volumes"`
}

type podSecurityContext struct {
	RunAsNonRoot       *bool          `yaml:"runAsNonRoot"`
	RunAsUser          int64          `yaml:"runAsUser"`
	RunAsGroup         int64          `yaml:"runAsGroup"`
	SupplementalGroups []int64        `yaml:"supplementalGroups"`
	SeccompProfile     seccompProfile `yaml:"seccompProfile"`
}

type seccompProfile struct {
	Type string `yaml:"type"`
}

type container struct {
	Name            string                   `yaml:"name"`
	Image           string                   `yaml:"image"`
	ImagePullPolicy string                   `yaml:"imagePullPolicy"`
	Args            []string                 `yaml:"args"`
	Ports           []containerPort          `yaml:"ports"`
	SecurityContext containerSecurityContext `yaml:"securityContext"`
	VolumeMounts    []volumeMount            `yaml:"volumeMounts"`
	LivenessProbe   probe                    `yaml:"livenessProbe"`
	ReadinessProbe  probe                    `yaml:"readinessProbe"`
}

type containerPort struct {
	Name          string `yaml:"name"`
	ContainerPort int    `yaml:"containerPort"`
	Protocol      string `yaml:"protocol"`
}

type containerSecurityContext struct {
	AllowPrivilegeEscalation *bool        `yaml:"allowPrivilegeEscalation"`
	ReadOnlyRootFilesystem   *bool        `yaml:"readOnlyRootFilesystem"`
	Capabilities             capabilities `yaml:"capabilities"`
}

type capabilities struct {
	Drop []string `yaml:"drop"`
}

type volumeMount struct {
	Name      string `yaml:"name"`
	MountPath string `yaml:"mountPath"`
	ReadOnly  bool   `yaml:"readOnly"`
}

type volume struct {
	Name      string             `yaml:"name"`
	HostPath  *hostPath          `yaml:"hostPath"`
	ConfigMap *namedVolumeSource `yaml:"configMap"`
	Secret    *namedVolumeSource `yaml:"secret"`
	Projected *namedVolumeSource `yaml:"projected"`
}

type hostPath struct {
	Path string `yaml:"path"`
	Type string `yaml:"type"`
}

type namedVolumeSource struct {
	Name string `yaml:"name"`
}

type probe struct {
	HTTPGet             httpGet `yaml:"httpGet"`
	InitialDelaySeconds int     `yaml:"initialDelaySeconds"`
	PeriodSeconds       int     `yaml:"periodSeconds"`
}

type httpGet struct {
	Host string `yaml:"host"`
	Path string `yaml:"path"`
	Port int    `yaml:"port"`
}
