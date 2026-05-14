package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	defaultHarvesterRelease   = "openbao-kms-lab"
	defaultHarvesterNamespace = "default"
	defaultHarvesterNetwork   = "default/vm4000"
	defaultHarvesterImageName = "REPLACE_WITH_HARVESTER_IMAGE_NAME"
	defaultHarvesterImageNS   = "default"
	defaultHarvesterSSHUser   = "ubuntu"

	openBaoHostAlias         = "obk-openbao"
	openBaoHostName          = "obk-openbao-1"
	systemdHostAlias         = "obk-kubeadm-systemd"
	staticPodHostAlias       = "obk-kubeadm-static"
	providerImageDefault     = "localhost/openbao-kms/bao-kms-provider:harvester-lab"
	remoteValuePath          = "/tmp/openbao-kms-smoke-value"
	remoteProviderAssetDir   = "/tmp/openbao-kms-provider-assets"
	staticPodPlaceholderHash = "0000000000000000000000000000000000000000000000000000000000000000"
)

type labCommand struct {
	name string
	run  func(context.Context, *labConfig, []string) error
}

type labConfig struct {
	root       string
	chart      string
	release    string
	namespace  string
	valuesPath string
	kubeconfig string

	artifactDir string
	sshConfig   string
	identityDir string

	sshPublicKeyFile  string
	sshPrivateKeyFile string
	sshUser           string

	imageNamespace string
	imageName      string
	networkName    string

	insecureSkipTLSVerify bool
	waitTimeout           time.Duration
	sshWaitTimeout        time.Duration

	openBaoHost          string
	openBaoVersion       string
	openBaoTLSServerName string
	jwtTTL               time.Duration

	kubernetesVersion string
	flannelVersion    string
	kubeadmInstallCNI string
	kubeadmHosts      []string

	multiControlPlaneEnabled bool
	multiControlPlaneHosts   []string

	systemdHost      string
	staticPodHost    string
	providerImage    string
	providerAssetDir string

	loadSecretCount int

	decryptWarmupSecretCount       int
	decryptWarmupDuration          time.Duration
	decryptWarmupWorkers           int
	decryptWarmupMaxP95            time.Duration
	decryptWarmupMinLists          int
	decryptWarmupRestartAPIServers bool
	decryptWarmupRestartParallel   bool
	decryptWarmupReuseCorpus       bool
	decryptWarmupSeedBatchSize     int
	decryptWarmupSeedWorkers       int

	decryptColdStartMaxP95  time.Duration
	decryptColdStartTimeout time.Duration
}

type versionsConfig struct {
	Validation struct {
		OpenBao struct {
			Primary string `yaml:"primary"`
		} `yaml:"openbao"`
		Kubernetes struct {
			ExactVersion string `yaml:"exactVersion"`
			Flannel      string `yaml:"flannel"`
		} `yaml:"kubernetes"`
	} `yaml:"validation"`
}

type labValues struct {
	Namespace string           `yaml:"namespace"`
	Network   labNetworkValues `yaml:"network"`
	Image     labImageValues   `yaml:"image"`
	CloudInit labCloudInit     `yaml:"cloudInit"`
	VMs       []labVMValues    `yaml:"vms"`
}

type labNetworkValues struct {
	Name string `yaml:"name"`
}

type labImageValues struct {
	Namespace string `yaml:"namespace"`
	Name      string `yaml:"name"`
}

type labCloudInit struct {
	Username         string   `yaml:"username"`
	SSHAuthorizedKey []string `yaml:"sshAuthorizedKeys"`
}

type labVMValues struct {
	Name       string `yaml:"name"`
	Role       string `yaml:"role"`
	Hostname   string `yaml:"hostname"`
	CPU        int    `yaml:"cpu"`
	MemoryGi   int    `yaml:"memoryGi"`
	DiskSizeGi int    `yaml:"diskSizeGi"`
}

type openBaoLoginRequest struct {
	JWT  string `json:"jwt"`
	Role string `json:"role"`
}

type openBaoLoginResponse struct {
	Auth openBaoAuthResponse `json:"auth"`
}

type openBaoAuthResponse struct {
	ClientToken string `json:"client_token"`
}

var labCommands = []labCommand{
	{name: "values", run: labRenderValues},
	{name: "lint", run: labLint},
	{name: "render", run: labRender},
	{name: "dry-run", run: labDryRun},
	{name: "create", run: labCreate},
	{name: "status", run: labStatus},
	{name: "wait", run: labWaitVMs},
	{name: "ssh-config", run: labSSHConfigCommand},
	{name: "wait-ssh", run: labWaitSSH},
	{name: "bootstrap-openbao", run: labBootstrapOpenBao},
	{name: "bootstrap-kubeadm", run: labBootstrapKubeadm},
	{name: "bootstrap-mcp", run: labBootstrapMultiControlPlane},
	{name: "bootstrap-guests", run: labBootstrapGuests},
	{name: "verify-guests", run: labVerifyGuests},
	{name: "wire-provider", run: labWireProviderCommand},
	{name: "wire-systemd", run: labWireSystemdCommand},
	{name: "wire-static", run: labWireStaticCommand},
	{name: "wire-mcp", run: labWireMultiControlPlaneCommand},
	{name: "verify-kms", run: labVerifyKMS},
	{name: "verify-recovery", run: labVerifyRecovery},
	{name: "verify-openbao-outage", run: labVerifyOpenBaoOutage},
	{name: "verify-load", run: labVerifyLoad},
	{name: "verify-decrypt-warmup", run: labVerifyDecryptWarmup},
	{name: "verify-decrypt-cold-start", run: labVerifyDecryptColdStart},
	{name: "verify-upgrade-rollback", run: labVerifyUpgradeRollback},
	{name: "verify-paired-restore", run: labVerifyPairedRestore},
	{name: "verify-mcp-recovery", run: labVerifyMultiControlPlaneRecovery},
	{name: "production-gate", run: labProductionGate},
	{name: "e2e", run: labE2E},
	{name: "destroy", run: labDestroy},
}

func runLab(argv []string) error {
	if len(argv) == 0 {
		return fmt.Errorf("usage: harvester_lab lab <%s>", labCommandNames())
	}
	cfg, err := newLabConfig()
	if err != nil {
		return err
	}
	ctx := context.Background()
	for _, command := range labCommands {
		if command.name == argv[0] {
			return command.run(ctx, cfg, argv[1:])
		}
	}
	return fmt.Errorf("unknown lab command %q; expected one of: %s", argv[0], labCommandNames())
}

func labCommandNames() string {
	names := make([]string, 0, len(labCommands))
	for _, command := range labCommands {
		names = append(names, command.name)
	}
	return strings.Join(names, ", ")
}

func newLabConfig() (*labConfig, error) {
	root, err := repoRoot()
	if err != nil {
		return nil, err
	}
	versions, err := loadVersions(filepath.Join(root, ".ci", "versions.yaml"))
	if err != nil {
		return nil, err
	}
	artifactDir := envOrDefault("HARVESTER_ARTIFACT_DIR", filepath.Join(root, "artifacts", "harvester"))
	cfg := &labConfig{
		root:      root,
		chart:     envOrDefault("HARVESTER_CHART", filepath.Join(root, "deploy", "harvester", "openbao-kms-lab")),
		release:   envOrDefault("HARVESTER_RELEASE", defaultHarvesterRelease),
		namespace: envOrDefault("HARVESTER_NAMESPACE", defaultHarvesterNamespace),
		valuesPath: envOrDefault(
			"HARVESTER_VALUES",
			filepath.Join(root, "hack", "harvester", "values.local.yaml"),
		),
		kubeconfig:            envOrDefault("KUBECONFIG", filepath.Join(root, "kubeconfig.yaml")),
		artifactDir:           artifactDir,
		sshConfig:             envOrDefault("HARVESTER_SSH_CONFIG", filepath.Join(artifactDir, "ssh-config")),
		identityDir:           envOrDefault("HARVESTER_IDENTITY_DIR", filepath.Join(artifactDir, "identity")),
		sshPublicKeyFile:      envOrDefault("SSH_PUBLIC_KEY_FILE", filepath.Join(homeDir(), ".ssh", "id_ed25519.pub")),
		sshPrivateKeyFile:     envOrDefault("SSH_PRIVATE_KEY_FILE", filepath.Join(homeDir(), ".ssh", "id_ed25519")),
		sshUser:               envOrDefault("HARVESTER_SSH_USER", defaultHarvesterSSHUser),
		imageNamespace:        envOrDefault("HARVESTER_IMAGE_NAMESPACE", defaultHarvesterImageNS),
		imageName:             envOrDefault("HARVESTER_IMAGE_NAME", defaultHarvesterImageName),
		networkName:           envOrDefault("HARVESTER_NETWORK_NAME", defaultHarvesterNetwork),
		insecureSkipTLSVerify: envBool("HARVESTER_INSECURE_SKIP_TLS_VERIFY"),
		waitTimeout:           envDuration("WAIT_TIMEOUT_SECONDS", 15*time.Minute),
		sshWaitTimeout:        envDuration("SSH_WAIT_TIMEOUT_SECONDS", 15*time.Minute),
		openBaoHost:           envOrDefault("OPENBAO_HOST", openBaoHostAlias),
		openBaoVersion:        envOrDefault("OPENBAO_VERSION", versions.Validation.OpenBao.Primary),
		openBaoTLSServerName:  envOrDefault("OPENBAO_TLS_SERVER_NAME", openBaoHostName),
		jwtTTL:                envDuration("HARVESTER_JWT_TTL", 12*time.Hour),
		kubernetesVersion:     envOrDefault("KUBERNETES_VERSION", versions.Validation.Kubernetes.ExactVersion),
		flannelVersion:        envOrDefault("FLANNEL_VERSION", versions.Validation.Kubernetes.Flannel),
		kubeadmInstallCNI:     envOrDefault("KUBEADM_INSTALL_CNI", "true"),
		kubeadmHosts:          envList("KUBEADM_HOSTS", []string{systemdHostAlias, staticPodHostAlias}),
		multiControlPlaneEnabled: envBool(
			"HARVESTER_ENABLE_MULTI_CONTROL_PLANE",
		),
		multiControlPlaneHosts: envList("HARVESTER_MCP_HOSTS", defaultMultiControlPlaneHosts()),
		systemdHost:            envOrDefault("SYSTEMD_HOST", systemdHostAlias),
		staticPodHost:          envOrDefault("STATIC_HOST", staticPodHostAlias),
		providerImage:          envOrDefault("PROVIDER_IMAGE", providerImageDefault),
		providerAssetDir:       envOrDefault("HARVESTER_PROVIDER_ASSET_DIR", filepath.Join(artifactDir, "provider")),
		loadSecretCount:        envInt("HARVESTER_LOAD_SECRET_COUNT", 25),
		decryptWarmupSecretCount: envInt(
			"HARVESTER_DECRYPT_WARMUP_SECRET_COUNT",
			2500,
		),
		decryptWarmupDuration: envDuration(
			"HARVESTER_DECRYPT_WARMUP_DURATION",
			10*time.Minute,
		),
		decryptWarmupWorkers: envInt("HARVESTER_DECRYPT_WARMUP_WORKERS", 0),
		decryptWarmupMaxP95: envDuration(
			"HARVESTER_DECRYPT_WARMUP_MAX_P95",
			30*time.Second,
		),
		decryptWarmupMinLists: envInt("HARVESTER_DECRYPT_WARMUP_MIN_LISTS", 3),
		decryptWarmupRestartAPIServers: envBoolDefault(
			"HARVESTER_DECRYPT_WARMUP_RESTART_APISERVERS",
			true,
		),
		decryptWarmupRestartParallel: envBool(
			"HARVESTER_DECRYPT_WARMUP_RESTART_PARALLEL",
		),
		decryptWarmupReuseCorpus: envBoolDefault(
			"HARVESTER_DECRYPT_WARMUP_REUSE_CORPUS",
			true,
		),
		decryptWarmupSeedBatchSize: envInt(
			"HARVESTER_DECRYPT_WARMUP_SEED_BATCH_SIZE",
			500,
		),
		decryptWarmupSeedWorkers: envInt(
			"HARVESTER_DECRYPT_WARMUP_SEED_WORKERS",
			1,
		),
		decryptColdStartMaxP95: envDuration(
			"HARVESTER_DECRYPT_COLD_START_MAX_P95",
			30*time.Second,
		),
		decryptColdStartTimeout: envDuration(
			"HARVESTER_DECRYPT_COLD_START_TIMEOUT",
			5*time.Minute,
		),
	}
	if cfg.openBaoVersion == "" || cfg.kubernetesVersion == "" || cfg.flannelVersion == "" {
		return nil, errors.New("missing OpenBao or Kubernetes versions in .ci/versions.yaml")
	}
	return cfg, nil
}

func defaultMultiControlPlaneHosts() []string {
	return []string{
		"obk-kubeadm-mcp-1",
		"obk-kubeadm-mcp-2",
		"obk-kubeadm-mcp-3",
	}
}

func repoRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel") // #nosec G204 -- fixed tool and arguments.
	output, err := cmd.Output()
	if err == nil {
		return strings.TrimSpace(string(output)), nil
	}
	workingDir, wdErr := os.Getwd()
	if wdErr != nil {
		return "", fmt.Errorf("resolve repo root: %w", wdErr)
	}
	return workingDir, nil
}

func loadVersions(path string) (versionsConfig, error) {
	var cfg versionsConfig
	// #nosec G304 -- local lab reads the repository version policy path.
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("read %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("decode %s: %w", path, err)
	}
	return cfg, nil
}

func homeDir() string {
	if home := os.Getenv("HOME"); home != "" {
		return home
	}
	return "."
}

func envOrDefault(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func envBool(name string) bool {
	return strings.EqualFold(os.Getenv(name), "true")
}

func envBoolDefault(name string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return strings.EqualFold(value, "true")
}

func envDuration(name string, fallback time.Duration) time.Duration {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	if isDigitsOnly(value) {
		seconds, err := time.ParseDuration(value + "s")
		if err != nil {
			return fallback
		}
		return seconds
	}
	if duration, err := time.ParseDuration(value); err == nil {
		return duration
	}
	return fallback
}

func isDigitsOnly(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func envInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func envList(name string, fallback []string) []string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return strings.Fields(value)
}

func runCmd(ctx context.Context, cfg *labConfig, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 -- local lab invokes configured developer tools.
	cmd.Dir = cfg.root
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = baseLabEnv(cfg)
	return cmd.Run()
}

func runCmdEnv(ctx context.Context, cfg *labConfig, env []string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 -- local lab invokes configured developer tools.
	cmd.Dir = cfg.root
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(baseLabEnv(cfg), env...)
	return cmd.Run()
}

func quietKubectl(ctx context.Context, cfg *labConfig, env []string, args ...string) error {
	cmd := exec.CommandContext(ctx, "kubectl", args...) // #nosec G204 -- local lab invokes configured developer tools.
	cmd.Dir = cfg.root
	cmd.Env = append(baseLabEnv(cfg), env...)
	return cmd.Run()
}

func outputCmd(ctx context.Context, cfg *labConfig, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 -- local lab invokes configured developer tools.
	cmd.Dir = cfg.root
	cmd.Env = baseLabEnv(cfg)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w\n%s", name, strings.Join(args, " "), err, stderr.String())
	}
	return output, nil
}

func outputCmdEnv(ctx context.Context, cfg *labConfig, env []string, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 -- local lab invokes configured developer tools.
	cmd.Dir = cfg.root
	cmd.Env = append(baseLabEnv(cfg), env...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w\n%s", name, strings.Join(args, " "), err, stderr.String())
	}
	return output, nil
}

func runCmdEnvDiscardOutput(ctx context.Context, cfg *labConfig, env []string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 -- local lab invokes configured developer tools.
	cmd.Dir = cfg.root
	cmd.Env = append(baseLabEnv(cfg), env...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w\n%s", name, strings.Join(args, " "), err, stderr.String())
	}
	return nil
}

func baseLabEnv(cfg *labConfig) []string {
	return append(os.Environ(), "KUBECONFIG="+cfg.kubeconfig)
}

func requireCommand(name string) error {
	if _, err := exec.LookPath(name); err != nil {
		return fmt.Errorf("missing required command: %s", name)
	}
	return nil
}

func ensureKubeconfig(cfg *labConfig) error {
	if _, err := os.Stat(cfg.kubeconfig); err != nil {
		return fmt.Errorf("kubeconfig not found at KUBECONFIG path: %s", cfg.kubeconfig)
	}
	return nil
}

func ensureValues(cfg *labConfig) error {
	// #nosec G304 -- local lab reads the configured values file.
	data, err := os.ReadFile(cfg.valuesPath)
	if err != nil {
		return fmt.Errorf("values file not found: %s; run: make -C hack/harvester values", cfg.valuesPath)
	}
	if strings.Contains(string(data), defaultHarvesterImageName) {
		return errors.New("HARVESTER_IMAGE_NAME must be set to an existing Harvester image before creating VMs")
	}
	return nil
}

func kubectlArgs(cfg *labConfig, args ...string) []string {
	result := []string{"--namespace", cfg.namespace}
	if cfg.insecureSkipTLSVerify {
		result = append(result, "--insecure-skip-tls-verify=true")
	}
	return append(result, args...)
}

func helmArgs(cfg *labConfig, args ...string) []string {
	if cfg.insecureSkipTLSVerify {
		return append([]string{"--kube-insecure-skip-tls-verify"}, args...)
	}
	return args
}

func selector(cfg *labConfig) string {
	return "app.kubernetes.io/instance=" + cfg.release
}

func labRenderValues(ctx context.Context, cfg *labConfig, _ []string) error {
	if err := requireCommand("go"); err != nil {
		return err
	}
	// #nosec G304 -- local lab reads the configured SSH public key path.
	keyData, err := os.ReadFile(cfg.sshPublicKeyFile)
	if err != nil {
		return fmt.Errorf("SSH public key not found: %s", cfg.sshPublicKeyFile)
	}
	values := defaultLabValues(cfg, strings.TrimSpace(string(keyData)))
	data, err := yaml.Marshal(values)
	if err != nil {
		return fmt.Errorf("render values: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(cfg.valuesPath), 0o750); err != nil {
		return fmt.Errorf("create values directory: %w", err)
	}
	// #nosec G306 -- values contain lab VM metadata and an SSH public key.
	if err := os.WriteFile(cfg.valuesPath, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", cfg.valuesPath, err)
	}
	fmt.Printf("wrote %s\n", cfg.valuesPath)
	if cfg.imageName == defaultHarvesterImageName {
		fmt.Fprintln(os.Stderr, "set HARVESTER_IMAGE_NAME to an existing Harvester image before creating VMs")
	}
	_ = ctx
	return nil
}

func defaultLabValues(cfg *labConfig, sshPublicKey string) labValues {
	values := labValues{
		Namespace: cfg.namespace,
		Network:   labNetworkValues{Name: cfg.networkName},
		Image:     labImageValues{Namespace: cfg.imageNamespace, Name: cfg.imageName},
		CloudInit: labCloudInit{Username: cfg.sshUser, SSHAuthorizedKey: []string{sshPublicKey}},
		VMs: []labVMValues{
			{Name: openBaoHostName, Role: "openbao", Hostname: openBaoHostName, CPU: 2, MemoryGi: 4, DiskSizeGi: 50},
			{
				Name:       "obk-kubeadm-systemd-1",
				Role:       "kubeadm-systemd",
				Hostname:   "obk-kubeadm-systemd-1",
				CPU:        4,
				MemoryGi:   8,
				DiskSizeGi: 80,
			},
			{
				Name:       "obk-kubeadm-static-1",
				Role:       "kubeadm-static",
				Hostname:   "obk-kubeadm-static-1",
				CPU:        4,
				MemoryGi:   8,
				DiskSizeGi: 80,
			},
		},
	}
	if cfg.multiControlPlaneEnabled {
		for _, host := range cfg.multiControlPlaneHosts {
			values.VMs = append(values.VMs, labVMValues{
				Name:       host,
				Role:       strings.TrimPrefix(host, "obk-"),
				Hostname:   host,
				CPU:        4,
				MemoryGi:   8,
				DiskSizeGi: 80,
			})
		}
	}
	return values
}

func labLint(ctx context.Context, cfg *labConfig, _ []string) error {
	return runCmd(ctx, cfg, "helm", "lint", cfg.chart)
}

func labRender(ctx context.Context, cfg *labConfig, _ []string) error {
	if err := ensureValues(cfg); err != nil {
		return err
	}
	args := []string{"template", cfg.release, cfg.chart, "--namespace", cfg.namespace, "--values", cfg.valuesPath}
	return runCmd(ctx, cfg, "helm", helmArgs(cfg, args...)...)
}

func labDryRun(ctx context.Context, cfg *labConfig, _ []string) error {
	return labRender(ctx, cfg, nil)
}

func labCreate(ctx context.Context, cfg *labConfig, _ []string) error {
	if err := ensureValues(cfg); err != nil {
		return err
	}
	if err := ensureKubeconfig(cfg); err != nil {
		return err
	}
	args := []string{
		"upgrade", "--install", cfg.release, cfg.chart,
		"--namespace", cfg.namespace,
		"--create-namespace",
		"--values", cfg.valuesPath,
	}
	if err := runCmd(ctx, cfg, "helm", helmArgs(cfg, args...)...); err != nil {
		return err
	}
	fmt.Println("VM resources submitted. Run: make -C hack/harvester status")
	return nil
}

func labStatus(ctx context.Context, cfg *labConfig, _ []string) error {
	if err := ensureKubeconfig(cfg); err != nil {
		return err
	}
	args := kubectlArgs(
		cfg,
		"get",
		"virtualmachines.kubevirt.io,virtualmachineinstances.kubevirt.io,pvc",
		"-l",
		selector(cfg),
		"-o",
		"wide",
	)
	return runCmd(ctx, cfg, "kubectl", args...)
}

func labWaitVMs(ctx context.Context, cfg *labConfig, _ []string) error {
	if err := waitForVMICreation(ctx, cfg); err != nil {
		return err
	}
	args := kubectlArgs(
		cfg,
		"wait",
		"virtualmachineinstances.kubevirt.io",
		"-l",
		selector(cfg),
		"--for=condition=Ready",
		"--timeout="+durationForKubectl(cfg.waitTimeout),
	)
	return runCmd(ctx, cfg, "kubectl", args...)
}

func waitForVMICreation(ctx context.Context, cfg *labConfig) error {
	deadline := time.Now().Add(cfg.waitTimeout)
	for {
		count, err := vmiCount(ctx, cfg)
		if err == nil && count > 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("timed out waiting for VMIs to be created")
		}
		time.Sleep(5 * time.Second)
	}
}

func vmiCount(ctx context.Context, cfg *labConfig) (int, error) {
	args := kubectlArgs(
		cfg,
		"get",
		"virtualmachineinstances.kubevirt.io",
		"-l",
		selector(cfg),
		"-o",
		"json",
	)
	output, err := outputCmd(ctx, cfg, "kubectl", args...)
	if err != nil {
		return 0, err
	}
	var list vmiList
	if err := json.Unmarshal(output, &list); err != nil {
		return 0, fmt.Errorf("decode VMI list: %w", err)
	}
	return len(list.Items), nil
}

func durationForKubectl(duration time.Duration) string {
	return fmt.Sprintf("%ds", int(duration.Seconds()))
}

func labSSHConfigCommand(ctx context.Context, cfg *labConfig, _ []string) error {
	_, err := labWriteSSHConfig(ctx, cfg)
	return err
}

func labWriteSSHConfig(ctx context.Context, cfg *labConfig) ([]sshHost, error) {
	if err := os.MkdirAll(cfg.artifactDir, 0o750); err != nil {
		return nil, fmt.Errorf("create artifact directory: %w", err)
	}
	vmiPath := filepath.Join(cfg.artifactDir, "vmi.json")
	vmPath := filepath.Join(cfg.artifactDir, "vm.json")
	vmNetCfgPath := filepath.Join(cfg.artifactDir, "vmnetcfg.json")
	if err := kubectlJSON(ctx, cfg, vmiPath, "virtualmachineinstances.kubevirt.io", "-l", selector(cfg)); err != nil {
		return nil, err
	}
	if err := kubectlJSON(ctx, cfg, vmPath, "virtualmachines.kubevirt.io", "-l", selector(cfg)); err != nil {
		return nil, err
	}
	if err := kubectlJSON(ctx, cfg, vmNetCfgPath, "virtualmachinenetworkconfigs.network.harvesterhci.io"); err != nil {
		return nil, err
	}
	hosts, err := collectHosts("", vmiPath, vmPath, vmNetCfgPath)
	if err != nil {
		return nil, err
	}
	if len(hosts) == 0 {
		return nil, errors.New("no VM IP addresses found")
	}
	if err := writeSSHConfig(cfg.sshConfig, cfg.sshUser, cfg.sshPrivateKeyFile, hosts); err != nil {
		return nil, err
	}
	return hosts, nil
}

func kubectlJSON(ctx context.Context, cfg *labConfig, outputPath string, resource string, extra ...string) error {
	args := append([]string{"get", resource}, extra...)
	args = append(args, "-o", "json")
	output, err := outputCmd(ctx, cfg, "kubectl", kubectlArgs(cfg, args...)...)
	if err != nil {
		return err
	}
	// #nosec G306 -- generated kubectl JSON contains lab metadata.
	if err := os.WriteFile(outputPath, output, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", outputPath, err)
	}
	return nil
}

func labWaitSSH(ctx context.Context, cfg *labConfig, _ []string) error {
	hosts, err := labWriteSSHConfig(ctx, cfg)
	if err != nil {
		return err
	}
	for _, host := range hosts {
		fmt.Printf("waiting for ssh: %s\n", host.Name)
		if err := waitForSSH(ctx, cfg, host.Name); err != nil {
			return err
		}
	}
	return nil
}

func waitForSSH(ctx context.Context, cfg *labConfig, host string) error {
	deadline := time.Now().Add(cfg.sshWaitTimeout)
	for {
		err := sshLab(
			ctx,
			cfg,
			host,
			"cloud-init status --wait >/dev/null 2>&1 || true",
			"-o",
			"ConnectTimeout=5",
		)
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for ssh: %s", host)
		}
		time.Sleep(5 * time.Second)
	}
}

func sshLab(ctx context.Context, cfg *labConfig, host string, remoteCommand string, extraArgs ...string) error {
	args := make([]string, 0, 4+len(extraArgs)+2)
	args = append(args, "-F", cfg.sshConfig, "-o", "BatchMode=yes")
	args = append(args, extraArgs...)
	args = append(args, host, remoteCommand)
	return runCmd(ctx, cfg, "ssh", args...)
}

func sshLabOutput(ctx context.Context, cfg *labConfig, host string, remoteCommand string) ([]byte, error) {
	args := []string{"-F", cfg.sshConfig, "-o", "BatchMode=yes", host, remoteCommand}
	return outputCmd(ctx, cfg, "ssh", args...)
}

func scpLab(ctx context.Context, cfg *labConfig, sourcesAndDest ...string) error {
	args := append([]string{"-F", cfg.sshConfig}, sourcesAndDest...)
	return runCmd(ctx, cfg, "scp", args...)
}

func labBootstrapGuests(ctx context.Context, cfg *labConfig, _ []string) error {
	if err := labWaitSSH(ctx, cfg, nil); err != nil {
		return err
	}
	if err := labBootstrapOpenBao(ctx, cfg, nil); err != nil {
		return err
	}
	return labBootstrapKubeadm(ctx, cfg, nil)
}

func labBootstrapOpenBao(ctx context.Context, cfg *labConfig, _ []string) error {
	if err := os.MkdirAll(cfg.artifactDir, 0o750); err != nil {
		return fmt.Errorf("create artifact directory: %w", err)
	}
	if err := writeIdentityFiles(
		cfg.identityDir,
		defaultJWTIssuer,
		defaultJWTAudience,
		defaultJWTSubject,
		time.Now().UTC(),
		cfg.jwtTTL,
	); err != nil {
		return err
	}
	openBaoIP, err := sshHostIP(cfg.sshConfig, cfg.openBaoHost)
	if err != nil {
		return err
	}
	if openBaoIP == "" {
		return errors.New("could not resolve OpenBao host IP from SSH config")
	}
	if err := sshLab(ctx, cfg, cfg.openBaoHost, "sudo install -d -m 0700 /root/openbao-kms-lab"); err != nil {
		return err
	}
	remoteDir := filepath.Join(cfg.root, "hack", "harvester", "remote")
	installScript := filepath.Join(remoteDir, "install-openbao.sh")
	if err := scpLab(ctx, cfg, installScript, cfg.openBaoHost+":/tmp/install-openbao.sh"); err != nil {
		return err
	}
	configureScript := filepath.Join(remoteDir, "configure-openbao.sh")
	if err := scpLab(ctx, cfg, configureScript, cfg.openBaoHost+":/tmp/configure-openbao.sh"); err != nil {
		return err
	}
	publicKeyPath := filepath.Join(cfg.identityDir, "jwt_public_key.pem")
	if err := scpLab(ctx, cfg, publicKeyPath, cfg.openBaoHost+":/tmp/jwt_public_key.pem"); err != nil {
		return err
	}
	installCommand := joinEnvForSudo(
		"OPENBAO_VERSION", cfg.openBaoVersion,
		"OPENBAO_IP", openBaoIP,
		"OPENBAO_TLS_SERVER_NAME", cfg.openBaoTLSServerName,
	) + " sh /tmp/install-openbao.sh"
	if err := sshLab(ctx, cfg, cfg.openBaoHost, installCommand); err != nil {
		return err
	}
	if err := sshLab(
		ctx,
		cfg,
		cfg.openBaoHost,
		"sudo install -m 0644 /tmp/jwt_public_key.pem /root/openbao-kms-lab/jwt_public_key.pem",
	); err != nil {
		return err
	}
	if err := sshLab(ctx, cfg, cfg.openBaoHost, "sudo sh /tmp/configure-openbao.sh"); err != nil {
		return err
	}
	ca, err := sshLabOutput(ctx, cfg, cfg.openBaoHost, "sudo cat /etc/openbao.d/tls/ca.crt")
	if err != nil {
		return err
	}
	caPath := filepath.Join(cfg.artifactDir, "openbao-ca.crt")
	// #nosec G306 -- CA certificate is public lab material.
	if err := os.WriteFile(caPath, ca, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", caPath, err)
	}
	fmt.Printf("OpenBao lab endpoint: https://%s:8200\n", openBaoIP)
	return nil
}

func joinEnvForSudo(pairs ...string) string {
	parts := []string{"sudo"}
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, pairs[i]+"="+shellQuote(pairs[i+1]))
	}
	return strings.Join(parts, " ")
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func sshHostIP(sshConfigPath string, host string) (string, error) {
	// #nosec G304 -- local lab reads generated SSH config.
	data, err := os.ReadFile(sshConfigPath)
	if err != nil {
		return "", fmt.Errorf("read SSH config: %w", err)
	}
	return parseSSHHostIP(string(data), host), nil
}

func parseSSHHostIP(config string, host string) string {
	active := false
	for _, line := range strings.Split(config, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if fields[0] == "Host" {
			active = false
			for _, candidate := range fields[1:] {
				if candidate == host {
					active = true
				}
			}
			continue
		}
		if active && fields[0] == "HostName" && len(fields) >= 2 {
			return fields[1]
		}
	}
	return ""
}

func labBootstrapKubeadm(ctx context.Context, cfg *labConfig, _ []string) error {
	if err := os.MkdirAll(cfg.artifactDir, 0o750); err != nil {
		return fmt.Errorf("create artifact directory: %w", err)
	}
	for _, host := range cfg.kubeadmHosts {
		nodeIP, err := sshHostIP(cfg.sshConfig, host)
		if err != nil {
			return err
		}
		if nodeIP == "" {
			return fmt.Errorf("could not resolve kubeadm host IP from SSH config: %s", host)
		}
		if err := bootstrapKubeadmHost(
			ctx,
			cfg,
			host,
			nodeIP,
			"single",
			"",
			"",
			"",
			cfg.kubeadmInstallCNI,
		); err != nil {
			return err
		}
		if err := writeRemoteKubeconfig(ctx, cfg, host); err != nil {
			return err
		}
	}
	return nil
}

func labBootstrapMultiControlPlane(ctx context.Context, cfg *labConfig, _ []string) error {
	checks, err := requireMultiControlPlaneChecks(cfg)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.artifactDir, 0o750); err != nil {
		return fmt.Errorf("create artifact directory: %w", err)
	}
	nodeIPs := make([]string, 0, len(checks))
	for _, check := range checks {
		nodeIP, err := sshHostIP(cfg.sshConfig, check.host)
		if err != nil {
			return err
		}
		if nodeIP == "" {
			return fmt.Errorf("could not resolve multi-control-plane host IP from SSH config: %s", check.host)
		}
		nodeIPs = append(nodeIPs, nodeIP)
	}
	endpoint := net.JoinHostPort(nodeIPs[0], "6443")
	if err := bootstrapKubeadmHost(
		ctx,
		cfg,
		checks[0].host,
		nodeIPs[0],
		"init",
		endpoint,
		"",
		"",
		cfg.kubeadmInstallCNI,
	); err != nil {
		return err
	}
	if err := writeRemoteKubeconfigPath(ctx, cfg, checks[0].host, checks[0].kubeconfig, nodeIPs[0]); err != nil {
		return err
	}
	joinCommand, certificateKey, err := multiControlPlaneJoinMaterial(ctx, cfg, checks[0].host)
	if err != nil {
		return err
	}
	for index := 1; index < len(checks); index++ {
		check := checks[index]
		if err := bootstrapKubeadmHost(
			ctx,
			cfg,
			check.host,
			nodeIPs[index],
			"join",
			endpoint,
			joinCommand,
			certificateKey,
			"false",
		); err != nil {
			return err
		}
		if err := writeRemoteKubeconfigPath(ctx, cfg, check.host, check.kubeconfig, nodeIPs[index]); err != nil {
			return err
		}
	}
	return waitMultiControlPlaneReady(ctx, cfg, checks)
}

func bootstrapKubeadmHost(
	ctx context.Context,
	cfg *labConfig,
	host string,
	nodeIP string,
	clusterMode string,
	controlPlaneEndpoint string,
	joinCommand string,
	certificateKey string,
	installCNI string,
) error {
	remoteScript := filepath.Join(cfg.root, "hack", "harvester", "remote", "bootstrap-kubeadm-node.sh")
	fmt.Printf("bootstrapping kubeadm on %s (%s, mode=%s)\n", host, nodeIP, clusterMode)
	if err := scpLab(ctx, cfg, remoteScript, host+":/tmp/bootstrap-kubeadm-node.sh"); err != nil {
		return err
	}
	command := joinEnvForSudo(
		"KUBERNETES_VERSION", cfg.kubernetesVersion,
		"KUBEADM_NODE_IP", nodeIP,
		"KUBEADM_CLUSTER_MODE", clusterMode,
		"KUBEADM_CONTROL_PLANE_ENDPOINT", controlPlaneEndpoint,
		"KUBEADM_JOIN_COMMAND", joinCommand,
		"KUBEADM_CERTIFICATE_KEY", certificateKey,
		"KUBEADM_INSTALL_CNI", installCNI,
		"FLANNEL_VERSION", cfg.flannelVersion,
	) + " sh /tmp/bootstrap-kubeadm-node.sh"
	return sshLab(ctx, cfg, host, command)
}

func multiControlPlaneJoinMaterial(ctx context.Context, cfg *labConfig, host string) (string, string, error) {
	certificateKeyOutput, err := sshLabOutput(
		ctx,
		cfg,
		host,
		"sudo kubeadm init phase upload-certs --upload-certs | tail -n1",
	)
	if err != nil {
		return "", "", err
	}
	certificateKey := lastNonEmptyLine(string(certificateKeyOutput))
	if certificateKey == "" {
		return "", "", errors.New("kubeadm did not return a certificate key for control-plane join")
	}
	joinCommandOutput, err := sshLabOutput(ctx, cfg, host, "sudo kubeadm token create --ttl 2h --print-join-command")
	if err != nil {
		return "", "", err
	}
	joinCommand := strings.TrimSpace(string(joinCommandOutput))
	if joinCommand == "" {
		return "", "", errors.New("kubeadm did not return a control-plane join command")
	}
	return joinCommand, certificateKey, nil
}

func lastNonEmptyLine(output string) string {
	lines := strings.Split(output, "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		line := strings.TrimSpace(lines[index])
		if line != "" {
			return line
		}
	}
	return ""
}

func waitMultiControlPlaneReady(ctx context.Context, cfg *labConfig, checks []kubeadmCheck) error {
	if len(checks) == 0 {
		return errors.New("multi-control-plane check list is empty")
	}
	env := []string{"KUBECONFIG=" + checks[0].kubeconfig}
	for _, check := range checks {
		if err := runCmdEnv(
			ctx,
			cfg,
			env,
			"kubectl",
			"wait",
			"node",
			check.host,
			"--for=condition=Ready",
			"--timeout=10m",
		); err != nil {
			return err
		}
	}
	return runCmdEnv(ctx, cfg, env, "kubectl", "get", "nodes", "-o", "wide")
}

func writeRemoteKubeconfig(ctx context.Context, cfg *labConfig, host string) error {
	nodeIP, err := sshHostIP(cfg.sshConfig, host)
	if err != nil {
		return err
	}
	if nodeIP == "" {
		return fmt.Errorf("could not resolve kubeadm host IP from SSH config: %s", host)
	}
	suffix := strings.TrimPrefix(host, "obk-kubeadm-")
	path := filepath.Join(cfg.artifactDir, "kubeconfig-"+suffix+".yaml")
	return writeRemoteKubeconfigPath(ctx, cfg, host, path, nodeIP)
}

func writeRemoteKubeconfigPath(
	ctx context.Context,
	cfg *labConfig,
	host string,
	path string,
	serverHost string,
) error {
	output, err := sshLabOutput(ctx, cfg, host, "sudo cat /etc/kubernetes/admin.conf")
	if err != nil {
		return err
	}
	if serverHost != "" {
		output, err = rewriteKubeconfigServer(output, "https://"+net.JoinHostPort(serverHost, "6443"))
		if err != nil {
			return err
		}
	}
	if err := os.WriteFile(path, output, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func rewriteKubeconfigServer(data []byte, server string) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("decode kubeconfig: %w", err)
	}
	root := documentContent(&doc)
	clusters := mappingValue(root, "clusters")
	if clusters == nil || clusters.Kind != yaml.SequenceNode {
		return nil, errors.New("kubeconfig does not contain clusters")
	}
	rewrote := false
	for _, clusterEntry := range clusters.Content {
		cluster := mappingValue(clusterEntry, "cluster")
		if cluster == nil {
			continue
		}
		serverNode := mappingValue(cluster, "server")
		if serverNode == nil {
			continue
		}
		serverNode.Value = server
		rewrote = true
	}
	if !rewrote {
		return nil, errors.New("kubeconfig does not contain a cluster server")
	}
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(&doc); err != nil {
		_ = encoder.Close()
		return nil, fmt.Errorf("encode kubeconfig: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("close kubeconfig encoder: %w", err)
	}
	return buf.Bytes(), nil
}

func documentContent(node *yaml.Node) *yaml.Node {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		return node.Content[0]
	}
	return node
}

func labVerifyGuests(ctx context.Context, cfg *labConfig, _ []string) error {
	openBaoIP, err := sshHostIP(cfg.sshConfig, cfg.openBaoHost)
	if err != nil {
		return err
	}
	if openBaoIP == "" {
		return errors.New("could not resolve OpenBao host IP from SSH config")
	}
	client, err := openBaoHTTPClient(cfg, openBaoIP)
	if err != nil {
		return err
	}
	if err := verifyOpenBaoHealth(ctx, client); err != nil {
		return err
	}
	fmt.Println("OpenBao health endpoint reachable")
	if err := verifyOpenBaoJWTLogin(ctx, cfg, client); err != nil {
		return err
	}
	fmt.Println("OpenBao JWT auth login succeeded")
	return filepath.WalkDir(cfg.artifactDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "kubeconfig-") {
			return nil
		}
		return runCmdEnv(ctx, cfg, []string{"KUBECONFIG=" + path}, "kubectl", "get", "nodes", "-o", "wide")
	})
}

func openBaoHTTPClient(cfg *labConfig, openBaoIP string) (*http.Client, error) {
	caPath := filepath.Join(cfg.artifactDir, "openbao-ca.crt")
	// #nosec G304 -- local lab reads generated CA certificate.
	caData, err := os.ReadFile(caPath)
	if err != nil {
		return nil, fmt.Errorf("OpenBao CA not found: %s", caPath)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caData) {
		return nil, fmt.Errorf("parse OpenBao CA: %s", caPath)
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{ // #nosec G402 -- CA and ServerName are explicitly configured.
			RootCAs:    roots,
			ServerName: cfg.openBaoTLSServerName,
			MinVersion: tls.VersionTLS12,
		},
		DialContext: func(ctx context.Context, network string, address string) (net.Conn, error) {
			if address == cfg.openBaoTLSServerName+":8200" {
				address = net.JoinHostPort(openBaoIP, "8200")
			}
			return dialer.DialContext(ctx, network, address)
		},
	}
	return &http.Client{Transport: transport, Timeout: 15 * time.Second}, nil
}

func verifyOpenBaoHealth(ctx context.Context, client *http.Client) error {
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		"https://"+openBaoHostName+":8200/v1/sys/health?standbyok=true",
		nil,
	)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("OpenBao health check: %w", err)
	}
	defer func() {
		_ = response.Body.Close()
	}()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return fmt.Errorf("OpenBao health check returned HTTP %d", response.StatusCode)
	}
	return nil
}

func verifyOpenBaoJWTLogin(ctx context.Context, cfg *labConfig, client *http.Client) error {
	jwtPath := filepath.Join(cfg.identityDir, "identity.jwt")
	// #nosec G304 -- local lab reads generated JWT for OpenBao login verification.
	jwtData, err := os.ReadFile(jwtPath)
	if err != nil {
		return fmt.Errorf("lab identity JWT not found: %s", jwtPath)
	}
	// #nosec G117 -- lab-only login request must use OpenBao's "jwt" JSON field.
	body, err := json.Marshal(openBaoLoginRequest{
		JWT:  strings.TrimSpace(string(jwtData)),
		Role: "openbao-kms-control-plane",
	})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		"https://"+openBaoHostName+":8200/v1/auth/k8s-workload-a-jwt/login",
		bytes.NewReader(body),
	)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("OpenBao JWT auth login: %w", err)
	}
	defer func() {
		_ = response.Body.Close()
	}()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return fmt.Errorf("OpenBao JWT auth login returned HTTP %d", response.StatusCode)
	}
	var loginResponse openBaoLoginResponse
	if err := json.NewDecoder(response.Body).Decode(&loginResponse); err != nil {
		return fmt.Errorf("decode OpenBao JWT auth login response: %w", err)
	}
	if loginResponse.Auth.ClientToken == "" {
		return errors.New("OpenBao JWT auth login response did not include a client token")
	}
	return nil
}

func labWireProviderCommand(ctx context.Context, cfg *labConfig, _ []string) error {
	return labWireProvider(ctx, cfg, "both")
}

func labWireSystemdCommand(ctx context.Context, cfg *labConfig, _ []string) error {
	return labWireProvider(ctx, cfg, "systemd")
}

func labWireStaticCommand(ctx context.Context, cfg *labConfig, _ []string) error {
	return labWireProvider(ctx, cfg, "static-pod")
}

func labWireMultiControlPlaneCommand(ctx context.Context, cfg *labConfig, _ []string) error {
	checks, err := requireMultiControlPlaneChecks(cfg)
	if err != nil {
		return err
	}
	openBaoIP, err := requireProviderInputs(cfg)
	if err != nil {
		return err
	}
	if err := buildProviderBinary(ctx, cfg); err != nil {
		return err
	}
	if err := copyProviderBaseAssets(cfg); err != nil {
		return err
	}
	imageTar, err := prepareStaticPodProviderAssets(
		ctx,
		cfg,
		openBaoIP,
		"provider-mcp.yaml",
		"harvester-mcp",
	)
	if err != nil {
		return err
	}
	for _, check := range checks {
		if err := installStaticPodProviderOnHost(
			ctx,
			cfg,
			check.host,
			check.kubeconfig,
			"provider-mcp.yaml",
			imageTar,
			check.suffix,
		); err != nil {
			return err
		}
	}
	return nil
}

func labWireProvider(ctx context.Context, cfg *labConfig, mode string) error {
	openBaoIP, err := requireProviderInputs(cfg)
	if err != nil {
		return err
	}
	if err := buildProviderBinary(ctx, cfg); err != nil {
		return err
	}
	if err := copyProviderBaseAssets(cfg); err != nil {
		return err
	}
	switch mode {
	case "both":
		if err := installSystemdProvider(ctx, cfg, openBaoIP); err != nil {
			return err
		}
		return installStaticPodProvider(ctx, cfg, openBaoIP)
	case "systemd":
		return installSystemdProvider(ctx, cfg, openBaoIP)
	case "static-pod":
		return installStaticPodProvider(ctx, cfg, openBaoIP)
	default:
		return fmt.Errorf("MODE must be systemd, static-pod, or both: %s", mode)
	}
}

func requireProviderInputs(cfg *labConfig) (string, error) {
	openBaoIP, err := sshHostIP(cfg.sshConfig, cfg.openBaoHost)
	if err != nil {
		return "", err
	}
	if openBaoIP == "" {
		return "", errors.New("could not resolve OpenBao host IP from SSH config")
	}
	if _, err := os.Stat(filepath.Join(cfg.artifactDir, "openbao-ca.crt")); err != nil {
		return "", fmt.Errorf("OpenBao CA not found; run: make -C hack/harvester bootstrap-openbao")
	}
	if _, err := os.Stat(filepath.Join(cfg.identityDir, "identity.jwt")); err != nil {
		return "", fmt.Errorf("lab identity JWT not found; run: make -C hack/harvester bootstrap-openbao")
	}
	return openBaoIP, nil
}

func buildProviderBinary(ctx context.Context, cfg *labConfig) error {
	return buildProviderBinaryAt(ctx, cfg, "harvester-lab", filepath.Join(cfg.providerAssetDir, "bao-kms-provider"))
}

func buildProviderBinaryAt(ctx context.Context, cfg *labConfig, version string, outputPath string) error {
	if err := requireCommand("go"); err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.providerAssetDir, 0o750); err != nil {
		return fmt.Errorf("create provider asset directory: %w", err)
	}
	commit := gitShortCommit(ctx, cfg)
	buildDate := time.Now().UTC().Format(time.RFC3339)
	ldflags := strings.Join([]string{
		"-s -w",
		"-X github.com/dc-tec/openbao-kubernetes-kms/internal/version.version=" + version,
		"-X github.com/dc-tec/openbao-kubernetes-kms/internal/version.commit=" + commit,
		"-X github.com/dc-tec/openbao-kubernetes-kms/internal/version.buildDate=" + buildDate,
		"-X github.com/dc-tec/openbao-kubernetes-kms/internal/version.dirty=true",
	}, " ")
	return runCmdEnv(
		ctx,
		cfg,
		[]string{"CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64"},
		"go",
		"build",
		"-trimpath",
		"-buildvcs=false",
		"-ldflags",
		ldflags,
		"-o",
		outputPath,
		"./cmd/bao-kms-provider",
	)
}

func gitShortCommit(ctx context.Context, cfg *labConfig) string {
	output, err := outputCmd(ctx, cfg, "git", "rev-parse", "--short", "HEAD")
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(output))
}

func copyProviderBaseAssets(cfg *labConfig) error {
	files := []struct {
		source string
		dest   string
		mode   os.FileMode
	}{
		{filepath.Join(cfg.artifactDir, "openbao-ca.crt"), "openbao-ca.crt", 0o644},
		{filepath.Join(cfg.identityDir, "identity.jwt"), "identity.jwt", 0o600},
		{filepath.Join(cfg.root, "deploy", "systemd", "bao-kms-provider.service"), "bao-kms-provider.service", 0o644},
	}
	for _, file := range files {
		if err := copyFile(file.source, filepath.Join(cfg.providerAssetDir, file.dest), file.mode); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(source string, dest string, mode os.FileMode) error {
	// #nosec G304 -- local lab copies configured repository/artifact paths.
	data, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("read %s: %w", source, err)
	}
	// #nosec G703 -- destination is assembled from local lab config and repository paths.
	if err := os.WriteFile(dest, data, mode); err != nil {
		return fmt.Errorf("write %s: %w", dest, err)
	}
	return nil
}

func installSystemdProvider(ctx context.Context, cfg *labConfig, openBaoIP string) error {
	if err := writeProviderConfig(
		cfg,
		"provider-systemd.yaml",
		"openbao-kms-socket",
		"harvester-systemd",
		openBaoIP,
	); err != nil {
		return err
	}
	if err := writeEncryptionConfig(filepath.Join(cfg.providerAssetDir, "encryption-config.yaml")); err != nil {
		return err
	}
	if err := copyCommonProviderAssets(ctx, cfg, cfg.systemdHost); err != nil {
		return err
	}
	systemdProviderConfig := filepath.Join(cfg.providerAssetDir, "provider-systemd.yaml")
	systemdProviderDest := cfg.systemdHost + ":" + remoteProviderAssetDir + "/provider.yaml"
	if err := scpLab(ctx, cfg, systemdProviderConfig, systemdProviderDest); err != nil {
		return err
	}
	systemdUnit := filepath.Join(cfg.providerAssetDir, "bao-kms-provider.service")
	systemdUnitDest := cfg.systemdHost + ":" + remoteProviderAssetDir + "/bao-kms-provider.service"
	if err := scpLab(ctx, cfg, systemdUnit, systemdUnitDest); err != nil {
		return err
	}
	remoteScript := filepath.Join(cfg.root, "hack", "harvester", "remote", "install-provider-systemd.sh")
	if err := scpLab(ctx, cfg, remoteScript, cfg.systemdHost+":/tmp/install-provider-systemd.sh"); err != nil {
		return err
	}
	if err := sshLab(ctx, cfg, cfg.systemdHost, "sudo sh /tmp/install-provider-systemd.sh"); err != nil {
		return err
	}
	if err := patchRemoteAPIServer(ctx, cfg, cfg.systemdHost, "systemd"); err != nil {
		return err
	}
	if err := waitAPIServer(ctx, cfg, filepath.Join(cfg.artifactDir, "kubeconfig-systemd.yaml")); err != nil {
		return err
	}
	fmt.Println("systemd kube-apiserver is ready with KMS config")
	return nil
}

func installStaticPodProvider(ctx context.Context, cfg *labConfig, openBaoIP string) error {
	imageTar, err := prepareStaticPodProviderAssets(ctx, cfg, openBaoIP, "provider-static.yaml", "harvester-static")
	if err != nil {
		return err
	}
	return installStaticPodProviderOnHost(
		ctx,
		cfg,
		cfg.staticPodHost,
		filepath.Join(cfg.artifactDir, "kubeconfig-static.yaml"),
		"provider-static.yaml",
		imageTar,
		"static",
	)
}

func prepareStaticPodProviderAssets(
	ctx context.Context,
	cfg *labConfig,
	openBaoIP string,
	providerConfigName string,
	clusterID string,
) (string, error) {
	if err := requireCommand("docker"); err != nil {
		return "", err
	}
	if err := writeProviderConfig(cfg, providerConfigName, "1234", clusterID, openBaoIP); err != nil {
		return "", err
	}
	if err := writeEncryptionConfig(filepath.Join(cfg.providerAssetDir, "encryption-config.yaml")); err != nil {
		return "", err
	}
	if err := writeStaticPodManifest(cfg); err != nil {
		return "", err
	}
	if err := buildProviderImage(ctx, cfg, cfg.providerImage, "harvester-lab"); err != nil {
		return "", err
	}
	imageTar := filepath.Join(cfg.providerAssetDir, "bao-kms-provider-image.tar")
	if err := runCmd(ctx, cfg, "docker", "save", cfg.providerImage, "-o", imageTar); err != nil {
		return "", err
	}
	return imageTar, nil
}

func installStaticPodProviderOnHost(
	ctx context.Context,
	cfg *labConfig,
	host string,
	kubeconfig string,
	providerConfigName string,
	imageTar string,
	name string,
) error {
	if err := copyCommonProviderAssets(ctx, cfg, host); err != nil {
		return err
	}
	if err := copyStaticPodAssets(ctx, cfg, host, providerConfigName, imageTar); err != nil {
		return err
	}
	remoteScript := filepath.Join(cfg.root, "hack", "harvester", "remote", "install-provider-static-pod.sh")
	if err := scpLab(ctx, cfg, remoteScript, host+":/tmp/install-provider-static-pod.sh"); err != nil {
		return err
	}
	if err := sshLab(ctx, cfg, host, "sudo sh /tmp/install-provider-static-pod.sh"); err != nil {
		return err
	}
	if err := patchRemoteAPIServer(ctx, cfg, host, name); err != nil {
		return err
	}
	if err := waitAPIServer(ctx, cfg, kubeconfig); err != nil {
		return err
	}
	fmt.Printf("%s kube-apiserver is ready with KMS config\n", name)
	return nil
}

func copyCommonProviderAssets(ctx context.Context, cfg *labConfig, host string) error {
	setup := "sudo rm -rf " + remoteProviderAssetDir +
		" && sudo install -d -m 0700 " + remoteProviderAssetDir +
		" && sudo chown " + shellQuote(cfg.sshUser) + ":" + shellQuote(cfg.sshUser) + " " + remoteProviderAssetDir
	if err := sshLab(ctx, cfg, host, setup); err != nil {
		return err
	}
	for _, name := range []string{"bao-kms-provider", "openbao-ca.crt", "identity.jwt", "encryption-config.yaml"} {
		source := filepath.Join(cfg.providerAssetDir, name)
		dest := host + ":" + remoteProviderAssetDir + "/" + name
		if err := scpLab(ctx, cfg, source, dest); err != nil {
			return err
		}
	}
	return nil
}

func copyStaticPodAssets(
	ctx context.Context,
	cfg *labConfig,
	host string,
	providerConfigName string,
	imageTar string,
) error {
	staticAssets := []string{
		filepath.Join(cfg.providerAssetDir, providerConfigName),
		filepath.Join(cfg.providerAssetDir, "bao-kms-provider.yaml"),
		imageTar,
	}
	remoteNames := []string{"provider.yaml", "bao-kms-provider.yaml", "bao-kms-provider-image.tar"}
	for index, source := range staticAssets {
		dest := host + ":" + remoteProviderAssetDir + "/" + remoteNames[index]
		if err := scpLab(ctx, cfg, source, dest); err != nil {
			return err
		}
	}
	return nil
}

func writeProviderConfig(
	cfg *labConfig,
	filename string,
	socketGroup string,
	clusterID string,
	openBaoIP string,
) error {
	path := filepath.Join(cfg.providerAssetDir, filename)
	content := providerConfigYAML(socketGroup, clusterID, openBaoIP)
	// #nosec G703 -- path is assembled from local lab config.
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func providerConfigYAML(socketGroup string, clusterID string, openBaoIP string) string {
	return fmt.Sprintf(`configVersion: v1alpha1
server:
  socketPath: /run/openbao-kms/kms.sock
  socketMode: "0660"
  socketGroup: "%s"
  metricsAddress: "127.0.0.1:8081"
  healthAddress: "127.0.0.1:8082"
openbao:
  address: https://%s:8200
  namespace: ""
  caCertFile: /etc/openbao-kms/tls/ca.crt
  tlsServerName: obk-openbao-1
  timeout: 5s
  instanceId: openbao-harvester-lab
auth:
  method: jwt
  mountPath: auth/k8s-workload-a-jwt
  role: openbao-kms-control-plane
  jwtFile: /var/lib/openbao-kms/identity.jwt
  minJwtRemainingTtl: 2m
  clockSkewLeeway: 30s
  loginBeforeTokenExpiry: 30s
  tokenRenewalIncrement: 1h
  loginTimeout: 0s
  expectedIssuer: https://issuer.example.internal
  expectedAudience:
    - bao-kms-provider
  expectedSubject: system:openbao-kms:workload-a
transit:
  mountPath: transit
  keyName: k8s-workload-a-etcd
  keyIdScope:
    providerName: openbao-kms-workload-a
    clusterId: %s
    transitMountId: transit-harvester-lab
    keyLineageId: 01HXEXAMPLEKEYLINEAGEID
bootstrap:
  graceTimeout: 60s
  retryInterval: 5s
status:
  probeInterval: 1s
  deepProbeInterval: 30s
  statusMaxStaleness: 1m
state:
  path: /var/lib/openbao-kms/state/key-registry.json
rotation:
  mode: observed
  activationDelay: 1s
  requireStableObservationCount: 1
  rejectVersionRollback: true
logging:
  level: info
  format: json
  logOpenBaoRequestIDs: true
  debugCorrelation:
    enabled: false
    ttl: 15m
    incidentId: ""
`, socketGroup, openBaoIP, clusterID)
}

func writeEncryptionConfig(path string) error {
	content := `apiVersion: apiserver.config.k8s.io/v1
kind: EncryptionConfiguration
resources:
  - resources:
      - secrets
    providers:
      - kms:
          apiVersion: v2
          name: openbao-kms-workload-a
          endpoint: unix:///run/openbao-kms/kms.sock
          timeout: 3s
      - identity: {}
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func writeStaticPodManifest(cfg *labConfig) error {
	path := filepath.Join(cfg.providerAssetDir, "bao-kms-provider.yaml")
	return writeStaticPodManifestForImage(cfg, path, cfg.providerImage)
}

func writeStaticPodManifestForImage(cfg *labConfig, path string, image string) error {
	source := filepath.Join(cfg.root, "deploy", "static-pod", "bao-kms-provider.yaml")
	// #nosec G304 -- local lab reads repository static-pod manifest.
	data, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("read %s: %w", source, err)
	}
	placeholder := "ghcr.io/dc-tec/bao-kms-provider@sha256:" + staticPodPlaceholderHash
	content := strings.ReplaceAll(string(data), placeholder, image)
	// #nosec G703 -- path is assembled from local lab config.
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func buildProviderImage(ctx context.Context, cfg *labConfig, image string, version string) error {
	commit := gitShortCommit(ctx, cfg)
	buildDate := time.Now().UTC().Format(time.RFC3339)
	return runCmdEnv(
		ctx,
		cfg,
		[]string{"DOCKER_BUILDKIT=1"},
		"docker",
		"build",
		"--platform",
		"linux/amd64",
		"--build-arg",
		"VERSION="+version,
		"--build-arg",
		"COMMIT="+commit,
		"--build-arg",
		"BUILD_DATE="+buildDate,
		"--build-arg",
		"DIRTY=true",
		"-t",
		image,
		".",
	)
}

func patchRemoteAPIServer(ctx context.Context, cfg *labConfig, host string, name string) error {
	original := filepath.Join(cfg.providerAssetDir, "kube-apiserver-"+name+".yaml")
	patched := filepath.Join(cfg.providerAssetDir, "kube-apiserver-"+name+".patched.yaml")
	manifest, err := sshLabOutput(ctx, cfg, host, "sudo cat /etc/kubernetes/manifests/kube-apiserver.yaml")
	if err != nil {
		return err
	}
	if err := os.WriteFile(original, manifest, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", original, err)
	}
	output, err := patchAPIServerManifest(manifest)
	if err != nil {
		return err
	}
	if err := os.WriteFile(patched, output, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", patched, err)
	}
	if err := scpLab(ctx, cfg, patched, host+":/tmp/kube-apiserver.yaml"); err != nil {
		return err
	}
	return sshLab(
		ctx,
		cfg,
		host,
		"sudo install -m 0644 -o root -g root /tmp/kube-apiserver.yaml /etc/kubernetes/manifests/kube-apiserver.yaml",
	)
}

func waitAPIServer(ctx context.Context, cfg *labConfig, kubeconfig string) error {
	deadline := time.Now().Add(4 * time.Minute)
	for {
		err := quietKubectl(ctx, cfg, []string{"KUBECONFIG=" + kubeconfig}, "get", "--raw=/readyz")
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for kube-apiserver: %s", kubeconfig)
		}
		time.Sleep(2 * time.Second)
	}
}

func labE2E(ctx context.Context, cfg *labConfig, _ []string) error {
	type labStep struct {
		name string
		run  func(context.Context, *labConfig, []string) error
	}
	steps := []labStep{
		{"lint", labLint},
		{"create", labCreate},
		{"wait", labWaitVMs},
		{"wait-ssh", labWaitSSH},
		{"bootstrap-guests", labBootstrapGuests},
	}
	if cfg.multiControlPlaneEnabled {
		steps = append(steps, labStep{"bootstrap-mcp", labBootstrapMultiControlPlane})
	}
	steps = append(steps,
		labStep{"verify-guests", labVerifyGuests},
		labStep{"wire-provider", labWireProviderCommand},
		labStep{"verify-kms", labVerifyKMS},
	)
	if cfg.multiControlPlaneEnabled {
		steps = append(steps,
			labStep{"wire-mcp", labWireMultiControlPlaneCommand},
			labStep{"verify-mcp-recovery", labVerifyMultiControlPlaneRecovery},
		)
	}
	for _, step := range steps {
		fmt.Printf("==> harvester lab: %s\n", step.name)
		if err := step.run(ctx, cfg, nil); err != nil {
			return fmt.Errorf("%s: %w", step.name, err)
		}
	}
	return labStatus(ctx, cfg, nil)
}

func labDestroy(ctx context.Context, cfg *labConfig, _ []string) error {
	if err := ensureKubeconfig(cfg); err != nil {
		return err
	}
	args := helmArgs(cfg, "uninstall", cfg.release, "--namespace", cfg.namespace)
	if err := runCmd(ctx, cfg, "helm", args...); err != nil {
		fmt.Fprintln(os.Stderr, "helm uninstall failed; continuing with PVC cleanup")
	}
	if !envBool("DELETE_PVCS") && os.Getenv("DELETE_PVCS") != "" {
		return nil
	}
	deleteTimeout := envOrDefault("DELETE_TIMEOUT", "300s")
	kubectlDeleteArgs := kubectlArgs(
		cfg,
		"delete",
		"pvc",
		"-l",
		selector(cfg),
		"--ignore-not-found=true",
		"--wait=true",
		"--timeout="+deleteTimeout,
	)
	return runCmd(ctx, cfg, "kubectl", kubectlDeleteArgs...)
}
