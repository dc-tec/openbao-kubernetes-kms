package deployment_test

import (
	"encoding/json"
	"os"
	"os/exec"
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
		requireHardenedAuthConfig(t, name, cfg)
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

func TestGrafanaDashboardSampleIsValid(t *testing.T) {
	data := readSample(t, "deploy/grafana/dashboards/openbao-kms-overview.json")

	var dashboard grafanaDashboard
	if err := json.Unmarshal([]byte(data), &dashboard); err != nil {
		t.Fatalf("decode Grafana dashboard sample: %v", err)
	}
	if dashboard.Title != "OpenBao Kubernetes KMS Overview" {
		t.Fatalf("unexpected dashboard title: %q", dashboard.Title)
	}
	if dashboard.UID != "openbao-kms-overview" {
		t.Fatalf("unexpected dashboard uid: %q", dashboard.UID)
	}
	if len(dashboard.Panels) < 12 {
		t.Fatalf("expected a deployment dashboard with broad metric coverage, got %d panels", len(dashboard.Panels))
	}

	requiredMetrics := []string{
		"openbao_kms_grpc_requests_total",
		"openbao_kms_grpc_duration_seconds_bucket",
		"openbao_kms_openbao_requests_total",
		"openbao_kms_openbao_duration_seconds_bucket",
		"openbao_kms_auth_login_total",
		"openbao_kms_auth_renewal_total",
		"openbao_kms_status_key_id_hash",
		"openbao_kms_rotation_state",
		"openbao_kms_aad_validation_errors_total",
		"openbao_kms_decrypt_key_id_errors_total",
		"openbao_kms_circuit_breaker_state",
		"openbao_kms_socket_restarts_total",
	}
	for _, metric := range requiredMetrics {
		if !dashboardContainsTarget(dashboard, metric) {
			t.Fatalf("Grafana dashboard sample missing metric %q", metric)
		}
	}
}

func TestPrometheusRuleSampleIsValid(t *testing.T) {
	data := readSample(t, "deploy/prometheus/rules/openbao-kms.rules.yaml")

	var groups prometheusRuleGroups
	if err := yaml.Unmarshal([]byte(data), &groups); err != nil {
		t.Fatalf("decode Prometheus rule sample: %v", err)
	}
	if len(groups.Groups) != 1 {
		t.Fatalf("expected one Prometheus rule group, got %d", len(groups.Groups))
	}
	group := groups.Groups[0]
	if group.Name != "openbao-kubernetes-kms.rules" {
		t.Fatalf("unexpected Prometheus rule group name: %q", group.Name)
	}
	if len(group.Rules) < 10 {
		t.Fatalf("expected broad alert rule coverage, got %d rules", len(group.Rules))
	}

	requiredAlerts := []string{
		"OpenBaoKMSStatusCacheStale",
		"OpenBaoKMSCircuitBreakerOpen",
		"OpenBaoKMSAuthFailures",
		"OpenBaoKMSKeyIDHashDiverged",
		"OpenBaoKMSAADValidationErrors",
		"OpenBaoKMSGRPCLatencyHigh",
	}
	for _, alert := range requiredAlerts {
		if !prometheusRulesContainAlert(group.Rules, alert) {
			t.Fatalf("Prometheus rule sample missing alert %q", alert)
		}
	}
}

type prometheusRuleGroups struct {
	Groups []prometheusRuleGroup `yaml:"groups"`
}

type prometheusRuleGroup struct {
	Name  string           `yaml:"name"`
	Rules []prometheusRule `yaml:"rules"`
}

type prometheusRule struct {
	Alert string `yaml:"alert"`
}

func prometheusRulesContainAlert(rules []prometheusRule, alert string) bool {
	for _, rule := range rules {
		if rule.Alert == alert {
			return true
		}
	}
	return false
}

type grafanaDashboard struct {
	Title  string         `json:"title"`
	UID    string         `json:"uid"`
	Panels []grafanaPanel `json:"panels"`
}

type grafanaPanel struct {
	Targets []grafanaTarget `json:"targets"`
}

type grafanaTarget struct {
	Expression string `json:"expr"`
}

func dashboardContainsTarget(dashboard grafanaDashboard, metric string) bool {
	for _, panel := range dashboard.Panels {
		for _, target := range panel.Targets {
			if strings.Contains(target.Expression, metric) {
				return true
			}
		}
	}
	return false
}

func requireHardenedAuthConfig(t *testing.T, name string, cfg config.Config) {
	t.Helper()

	if cfg.Auth.JWT.ExpectedIssuer == "" {
		t.Fatalf("%s provider config should set auth.jwt.expectedIssuer", name)
	}
	if len(cfg.Auth.JWT.ExpectedAudience) == 0 {
		t.Fatalf("%s provider config should set auth.jwt.expectedAudience", name)
	}
	if cfg.Auth.JWT.ExpectedSubject == "" {
		t.Fatalf("%s provider config should set auth.jwt.expectedSubject", name)
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

func TestPublicDeploymentSurfacesAvoidFloatingInputs(t *testing.T) {
	publicSurfaces := []string{
		"README.md",
		"docs/getting-started/install.md",
		"docs/deployment/choosing-a-model.md",
		"docs/deployment/static-pod.md",
		"docs/deployment/systemd.md",
		"deploy/README.md",
		"deploy/package/bundles/static-pod/README.md",
		"deploy/package/bundles/systemd/README.md",
		"deploy/package/linux/README.md",
		"deploy/static-pod/bao-kms-provider.yaml",
	}
	for _, path := range publicSurfaces {
		content := readSample(t, path)
		for _, forbidden := range []string{":latest", "@main", "@master", "@HEAD"} {
			if strings.Contains(content, forbidden) {
				t.Fatalf("%s must not contain floating input %q", path, forbidden)
			}
		}
		if strings.Contains(content, "curl") &&
			(strings.Contains(content, "| sh") || strings.Contains(content, "| bash")) {
			t.Fatalf("%s must not contain curl-pipe-shell install guidance", path)
		}
	}
}

func TestProviderImageReferencesAreDigestAddressed(t *testing.T) {
	for _, path := range []string{
		"docs/getting-started/install.md",
		"docs/deployment/static-pod.md",
		"deploy/static-pod/bao-kms-provider.yaml",
	} {
		content := readSample(t, path)
		for _, line := range strings.Split(content, "\n") {
			if !strings.Contains(line, "ghcr.io/dc-tec/bao-kms-provider") {
				continue
			}
			if !strings.Contains(line, "@sha256:") {
				t.Fatalf("%s provider image reference must be digest-addressed: %s", path, strings.TrimSpace(line))
			}
		}
	}
}

func TestSystemdAndPackageSamplesUseResolvedIdentity(t *testing.T) {
	unit := readSample(t, "deploy/systemd/bao-kms-provider.service")
	requiredUnitLines := []string{
		"User=openbao-kms",
		"Group=openbao-kms",
		"SupplementaryGroups=openbao-kms-socket",
		"ExecStart=/usr/bin/bao-kms-provider serve --config /etc/openbao-kms/config.yaml",
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
		"d /run/openbao-kms 2750 openbao-kms openbao-kms-socket -",
		"d /var/lib/openbao-kms/state 0750 openbao-kms openbao-kms -",
	} {
		if !strings.Contains(tmpfiles, want) {
			t.Fatalf("tmpfiles sample missing %q", want)
		}
	}

	nfpm := readSample(t, "deploy/package/linux/nfpm.yaml")
	for _, want := range []string{
		"name: bao-kms-provider",
		"arch: ${NFPM_ARCH}",
		"dst: /usr/bin/bao-kms-provider",
		"dst: /usr/lib/systemd/system/bao-kms-provider.service",
		"postinstall: deploy/package/linux/scripts/postinstall.sh",
		"postremove: deploy/package/linux/scripts/postremove.sh",
	} {
		if !strings.Contains(nfpm, want) {
			t.Fatalf("nFPM config missing %q", want)
		}
	}

	postinstall := readSample(t, "deploy/package/linux/scripts/postinstall.sh")
	for _, forbidden := range []string{"systemctl enable", "systemctl start"} {
		if strings.Contains(postinstall, forbidden) {
			t.Fatalf("package postinstall must not run %q", forbidden)
		}
	}
}

func TestOpenTofuModuleConfiguresOpenBaoTransit(t *testing.T) {
	modulePath := repoPath("deploy/opentofu/openbao-kubernetes-kms")
	tfFiles, err := filepath.Glob(filepath.Join(modulePath, "*.tf"))
	if err != nil {
		t.Fatalf("glob OpenTofu .tf files: %v", err)
	}
	if len(tfFiles) > 0 {
		t.Fatalf("OpenTofu module should use .tofu files, found .tf files: %#v", tfFiles)
	}

	for _, name := range []string{"versions.tofu", "variables.tofu", "main.tofu", "outputs.tofu"} {
		if _, err := os.Stat(filepath.Join(modulePath, name)); err != nil {
			t.Fatalf("missing OpenTofu module file %s: %v", name, err)
		}
	}

	templateFiles, err := filepath.Glob(filepath.Join(modulePath, "templates", "*"))
	if err != nil {
		t.Fatalf("glob OpenTofu template files: %v", err)
	}
	if len(templateFiles) > 0 {
		t.Fatalf("OpenTofu module should configure OpenBao resources, not render config templates: %#v", templateFiles)
	}

	main := readSample(t, "deploy/opentofu/openbao-kubernetes-kms/main.tofu")
	required := []string{
		`resource "vault_mount" "transit"`,
		`resource "vault_generic_endpoint" "transit_disable_upsert"`,
		`disable_upsert = true`,
		`resource "vault_transit_secret_backend_key" "kubernetes_kms"`,
		`deletion_allowed`,
		`exportable`,
		`allow_plaintext_backup`,
		`auto_rotate_period`,
		`resource "vault_policy" "provider"`,
		`path "sys/capabilities-self"`,
		`path "auth/token/renew-self"`,
	}
	for _, want := range required {
		if !strings.Contains(main, want) {
			t.Fatalf("OpenTofu main.tofu missing %q", want)
		}
	}
}

func TestInstallScriptsStageSystemdAndStaticPodLayouts(t *testing.T) {
	binaryPath := filepath.Join(t.TempDir(), "bao-kms-provider")
	if err := os.WriteFile(binaryPath, []byte("#!/bin/sh\nexit 0\n"), 0o600); err != nil {
		t.Fatalf("write fake provider binary: %v", err)
	}

	systemdRoot := t.TempDir()
	runSystemdInstallScript(t, []string{
		"ROOT=" + systemdRoot,
		"BINARY=" + binaryPath,
	})
	requirePathMode(t, systemdRoot, "/usr/bin/bao-kms-provider", 0o755)
	requirePathMode(t, systemdRoot, "/etc/openbao-kms/config.yaml", 0o640)
	requirePathMode(t, systemdRoot, "/etc/systemd/system/bao-kms-provider.service", 0o644)
	requirePathMode(t, systemdRoot, "/usr/lib/sysusers.d/openbao-kms.conf", 0o644)
	requirePathMode(t, systemdRoot, "/usr/lib/tmpfiles.d/openbao-kms.conf", 0o644)

	staticPodRoot := t.TempDir()
	runStaticPodInstallScript(t, []string{
		"ROOT=" + staticPodRoot,
	})
	requirePathMode(t, staticPodRoot, "/etc/openbao-kms", 0o750)
	requirePathMode(t, staticPodRoot, "/etc/openbao-kms/tls", 0o755)
	requirePathMode(t, staticPodRoot, "/var/lib/openbao-kms", 0o750)
	requirePathMode(t, staticPodRoot, "/var/lib/openbao-kms/state", 0o750)
	requirePathMode(t, staticPodRoot, "/run/openbao-kms", 0o750)
	requirePathMode(t, staticPodRoot, "/etc/openbao-kms/config.yaml", 0o640)
	requirePathMode(t, staticPodRoot, "/etc/kubernetes/manifests/bao-kms-provider.yaml", 0o644)
	requireSetGID(t, stagedPath(staticPodRoot, "/run/openbao-kms"))
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

func runSystemdInstallScript(t *testing.T, env []string) {
	t.Helper()

	cmd := exec.Command("sh", "hack/kubeadm/install-systemd-lab.sh")
	cmd.Dir = repoPath(".")
	cmd.Env = append(os.Environ(), env...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run systemd install script: %v: %s", err, strings.TrimSpace(string(output)))
	}
}

func runStaticPodInstallScript(t *testing.T, env []string) {
	t.Helper()

	cmd := exec.Command("sh", "hack/kubeadm/install-static-pod-lab.sh")
	cmd.Dir = repoPath(".")
	cmd.Env = append(os.Environ(), env...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run static pod install script: %v: %s", err, strings.TrimSpace(string(output)))
	}
}

func requirePathMode(t *testing.T, root string, path string, mode os.FileMode) {
	t.Helper()

	info, err := os.Stat(stagedPath(root, path))
	if err != nil {
		t.Fatalf("stat staged path %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != mode {
		t.Fatalf("staged path %s mode %04o, want %04o", path, got, mode)
	}
}

func requireSetGID(t *testing.T, path string) {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat staged path %s: %v", path, err)
	}
	if info.Mode()&os.ModeSetgid == 0 {
		t.Fatalf("staged path %s should preserve setgid bit", path)
	}
}

func stagedPath(root string, path string) string {
	return filepath.Join(root, strings.TrimPrefix(path, "/"))
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
