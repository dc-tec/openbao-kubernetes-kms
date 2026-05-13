package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	defaultManifestPath = "test/e2e/suites.yaml"
	defaultGinkgoBinary = "ginkgo"
)

type suitesManifest struct {
	Version       int           `yaml:"version"`
	VersionPolicy versionPolicy `yaml:"versionPolicy"`
	Defaults      suiteDefaults `yaml:"defaults"`
	ReleaseGate   releaseGate   `yaml:"releaseGate"`
	Lanes         []lane        `yaml:"lanes"`
}

type versionPolicy struct {
	Source     string `yaml:"source"`
	OpenBao    string `yaml:"openbao"`
	Kubernetes string `yaml:"kubernetes"`
}

type suiteDefaults struct {
	Package       string `yaml:"package"`
	ArtifactDir   string `yaml:"artifactDir"`
	Timeout       string `yaml:"timeout"`
	ParallelNodes int    `yaml:"parallelNodes"`
}

type releaseGate struct {
	Preview releaseGateDefinition `yaml:"preview"`
}

type releaseGateDefinition struct {
	Status string              `yaml:"status"`
	Groups map[string][]string `yaml:"groups"`
}

type lane struct {
	ID               string   `yaml:"id"`
	Name             string   `yaml:"name"`
	MakeTarget       string   `yaml:"makeTarget"`
	Package          string   `yaml:"package"`
	LabelFilter      string   `yaml:"labelFilter"`
	RunRegex         string   `yaml:"runRegex"`
	PRScope          string   `yaml:"prScope"`
	Environment      string   `yaml:"environment"`
	Isolation        string   `yaml:"isolation"`
	Timeout          string   `yaml:"timeout"`
	ParallelNodes    int      `yaml:"parallelNodes"`
	RequiredEnv      []string `yaml:"requiredEnv"`
	EnableEnv        string   `yaml:"enableEnv"`
	ProviderImageEnv string   `yaml:"providerImageEnv"`
	VersionRefs      []string `yaml:"versionRefs"`
	Status           string   `yaml:"status"`
	Reports          bool     `yaml:"reports"`
}

type versionsConfig struct {
	Validation struct {
		Kubernetes kubernetesValidation `yaml:"kubernetes"`
	} `yaml:"validation"`
}

type kubernetesValidation struct {
	PreviewMatrix []kubernetesPreviewEntry `yaml:"previewMatrix"`
}

type kubernetesPreviewEntry struct {
	Line                string `yaml:"line" json:"kubernetes_line"`
	UpstreamPatch       string `yaml:"upstreamPatch" json:"upstream_patch"`
	ExactVersion        string `yaml:"exactVersion" json:"kubernetes_version"`
	KindNodeImage       string `yaml:"kindNodeImage" json:"kind_node_image"`
	KindNodeImageDigest string `yaml:"kindNodeImageDigest" json:"kind_node_image_digest"`
	ReleaseGate         bool   `yaml:"releaseGate" json:"-"`
}

type kubernetesMatrixOutput struct {
	Include []kubernetesPreviewEntry `json:"include"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "e2e release gate: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("e2e-release-gate", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	manifestPath := flags.String("manifest", defaultManifestPath, "Path to E2E suite manifest")
	versionsPath := flags.String("versions", "", "Path to version policy file")
	group := flags.String("group", "", "Release gate group to run")
	kubernetesLine := flags.String(
		"kubernetes-line",
		os.Getenv("E2E_KUBERNETES_LINE"),
		"Kubernetes release line to run for Kind release gates",
	)
	ginkgoBinary := flags.String("ginkgo", envDefault("GINKGO", defaultGinkgoBinary), "Ginkgo binary to execute")
	artifactDir := flags.String("artifact-dir", "", "Directory for release gate E2E reports")
	laneID := flags.String("lane", "", "Optional release gate lane ID to run from the selected group")
	matrix := flags.Bool("matrix", false, "Print GitHub Actions JSON matrix for the selected release gate group")
	dryRun := flags.Bool("dry-run", false, "Print selected targets without running them")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *group == "" {
		return errors.New("group is required")
	}

	manifest, err := loadManifest(*manifestPath)
	if err != nil {
		return err
	}
	if *versionsPath == "" {
		*versionsPath = manifest.VersionPolicy.Source
	}
	lanes, err := releaseGateLanes(manifest, *group)
	if err != nil {
		return err
	}
	if *laneID != "" {
		lanes, err = selectLane(lanes, *laneID)
		if err != nil {
			return err
		}
	}
	if *artifactDir == "" {
		*artifactDir = envDefault("E2E_ARTIFACT_DIR", manifest.Defaults.ArtifactDir)
	}

	if *group == "kind" {
		kubernetesEntries, err := releaseGateKubernetesMatrix(*versionsPath, *kubernetesLine)
		if err != nil {
			return err
		}
		if *matrix {
			return printKubernetesMatrix(kubernetesEntries)
		}
		return runKubernetesMatrixLanes(*ginkgoBinary, *artifactDir, manifest.Defaults, lanes, kubernetesEntries, *dryRun)
	}
	if *matrix {
		return fmt.Errorf("-matrix is only supported for the kind release gate group")
	}

	return runLanes(*ginkgoBinary, *artifactDir, manifest.Defaults, *group, lanes, nil, *dryRun)
}

func loadManifest(path string) (suitesManifest, error) {
	// #nosec G304 -- release gate helper intentionally reads the caller-selected manifest path.
	raw, err := os.ReadFile(path)
	if err != nil {
		return suitesManifest{}, fmt.Errorf("read manifest: %w", err)
	}

	var manifest suitesManifest
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&manifest); err != nil {
		return suitesManifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	return manifest, nil
}

func releaseGateLanes(manifest suitesManifest, group string) ([]lane, error) {
	if manifest.ReleaseGate.Preview.Status != "active" {
		return nil, fmt.Errorf("preview release gate status must be active, got %q", manifest.ReleaseGate.Preview.Status)
	}

	laneIDs, ok := manifest.ReleaseGate.Preview.Groups[group]
	if !ok {
		return nil, fmt.Errorf("unknown release gate group %q", group)
	}
	if len(laneIDs) == 0 {
		return nil, fmt.Errorf("release gate group %q has no lanes", group)
	}

	lanes := make(map[string]lane, len(manifest.Lanes))
	for _, entry := range manifest.Lanes {
		if entry.ID != "" {
			lanes[entry.ID] = entry
		}
	}

	selected := make([]lane, 0, len(laneIDs))
	for _, laneID := range laneIDs {
		entry, ok := lanes[laneID]
		if !ok {
			return nil, fmt.Errorf("release gate group %q references unknown lane %q", group, laneID)
		}
		if entry.Status != "active" {
			return nil, fmt.Errorf("release gate lane %q status must be active, got %q", laneID, entry.Status)
		}
		if entry.LabelFilter == "" && entry.RunRegex == "" {
			return nil, fmt.Errorf("release gate lane %q has neither labelFilter nor runRegex", laneID)
		}
		selected = append(selected, entry)
	}
	return selected, nil
}

func selectLane(lanes []lane, laneID string) ([]lane, error) {
	for _, entry := range lanes {
		if entry.ID == laneID {
			return []lane{entry}, nil
		}
	}
	return nil, fmt.Errorf("selected lane %q is not in release gate group", laneID)
}

func loadVersions(path string) (versionsConfig, error) {
	// #nosec G304 -- release gate helper intentionally reads the caller-selected version policy path.
	raw, err := os.ReadFile(path)
	if err != nil {
		return versionsConfig{}, fmt.Errorf("read versions: %w", err)
	}

	var cfg versionsConfig
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return versionsConfig{}, fmt.Errorf("decode versions: %w", err)
	}
	return cfg, nil
}

func releaseGateKubernetesMatrix(versionsPath string, selectedLine string) ([]kubernetesPreviewEntry, error) {
	cfg, err := loadVersions(versionsPath)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]struct{}, len(cfg.Validation.Kubernetes.PreviewMatrix))
	entries := make([]kubernetesPreviewEntry, 0, len(cfg.Validation.Kubernetes.PreviewMatrix))
	for _, entry := range cfg.Validation.Kubernetes.PreviewMatrix {
		if !entry.ReleaseGate {
			continue
		}
		if err := validateKubernetesPreviewEntry(entry); err != nil {
			return nil, err
		}
		if _, ok := seen[entry.Line]; ok {
			return nil, fmt.Errorf("duplicate Kubernetes release gate line %q", entry.Line)
		}
		seen[entry.Line] = struct{}{}
		if selectedLine != "" && entry.Line != selectedLine {
			continue
		}
		entries = append(entries, entry)
	}
	if selectedLine != "" && len(entries) == 0 {
		return nil, fmt.Errorf("kubernetes release gate line %q is not configured", selectedLine)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("%s has no Kubernetes previewMatrix entries with releaseGate=true", versionsPath)
	}
	return entries, nil
}

func validateKubernetesPreviewEntry(entry kubernetesPreviewEntry) error {
	if entry.Line == "" {
		return errors.New("kubernetes previewMatrix entry is missing line")
	}
	if entry.ExactVersion == "" {
		return fmt.Errorf("kubernetes previewMatrix line %q is missing exactVersion", entry.Line)
	}
	if entry.KindNodeImage == "" {
		return fmt.Errorf("kubernetes previewMatrix line %q is missing kindNodeImage", entry.Line)
	}
	if entry.KindNodeImageDigest == "" {
		return fmt.Errorf("kubernetes previewMatrix line %q is missing kindNodeImageDigest", entry.Line)
	}
	if !strings.Contains(entry.KindNodeImage, "@"+entry.KindNodeImageDigest) {
		return fmt.Errorf("kubernetes previewMatrix line %q kindNodeImage must include kindNodeImageDigest", entry.Line)
	}
	return nil
}

func printKubernetesMatrix(entries []kubernetesPreviewEntry) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(kubernetesMatrixOutput{Include: entries}); err != nil {
		return fmt.Errorf("encode Kubernetes matrix: %w", err)
	}
	return nil
}

func runKubernetesMatrixLanes(
	ginkgoBinary string,
	artifactDir string,
	defaults suiteDefaults,
	lanes []lane,
	entries []kubernetesPreviewEntry,
	dryRun bool,
) error {
	for _, entry := range entries {
		if err := runLanes(ginkgoBinary, artifactDir, defaults, "kind", lanes, &entry, dryRun); err != nil {
			return err
		}
	}
	return nil
}

func runLanes(
	ginkgoBinary string,
	artifactDir string,
	defaults suiteDefaults,
	group string,
	lanes []lane,
	kubernetesEntry *kubernetesPreviewEntry,
	dryRun bool,
) error {
	for _, entry := range lanes {
		command, err := buildLaneCommand(ginkgoBinary, artifactDir, defaults, group, entry, kubernetesEntry, !dryRun)
		if err != nil {
			return err
		}
		if dryRun {
			fmt.Println(command.String())
			continue
		}
		if err := os.MkdirAll(command.ReportDir, 0o750); err != nil {
			return fmt.Errorf("prepare report directory for lane %q: %w", entry.ID, err)
		}
		fmt.Fprintf(os.Stderr, "==> %s\n", command.String())
		logFile, err := os.Create(filepath.Join(command.ReportDir, "console.log"))
		if err != nil {
			return fmt.Errorf("create console log for lane %q: %w", entry.ID, err)
		}
		// #nosec G204 -- release gate helper intentionally executes the configured Ginkgo binary.
		cmd := exec.Command(command.Binary, command.Args...)
		cmd.Stdout = io.MultiWriter(os.Stdout, logFile)
		cmd.Stderr = io.MultiWriter(os.Stderr, logFile)
		cmd.Stdin = os.Stdin
		cmd.Env = command.Env
		runErr := cmd.Run()
		closeErr := logFile.Close()
		if runErr != nil {
			return fmt.Errorf("run release gate lane %q: %w", entry.ID, runErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close console log for lane %q: %w", entry.ID, closeErr)
		}
	}
	return nil
}

type laneCommand struct {
	Binary    string
	Args      []string
	Env       []string
	ReportDir string
}

func (c laneCommand) String() string {
	parts := make([]string, 0, 1+len(c.Args))
	parts = append(parts, shellQuote(c.Binary))
	for _, arg := range c.Args {
		parts = append(parts, shellQuote(arg))
	}
	return strings.Join(parts, " ")
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	if !strings.ContainsAny(value, " \t\n'\"\\$`&|;<>(){}[]*?!") {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func buildLaneCommand(
	ginkgoBinary string,
	artifactDir string,
	defaults suiteDefaults,
	group string,
	entry lane,
	kubernetesEntry *kubernetesPreviewEntry,
	validateEnv bool,
) (laneCommand, error) {
	if err := validateGinkgoBinary(ginkgoBinary, validateEnv); err != nil {
		return laneCommand{}, err
	}

	packagePath, err := lanePackage(entry, defaults)
	if err != nil {
		return laneCommand{}, err
	}
	timeout, err := laneTimeout(entry, defaults)
	if err != nil {
		return laneCommand{}, err
	}
	parallelNodes, err := laneParallelNodes(entry, defaults)
	if err != nil {
		return laneCommand{}, err
	}

	reportDir := laneReportDir(artifactDir, group, entry.ID, kubernetesEntry)
	args := buildGinkgoArgs(entry, packagePath, timeout, parallelNodes, reportDir)
	env, err := laneEnvironment(os.Environ(), entry, kubernetesEntry, validateEnv)
	if err != nil {
		return laneCommand{}, err
	}
	return laneCommand{Binary: ginkgoBinary, Args: args, Env: env, ReportDir: reportDir}, nil
}

func validateGinkgoBinary(ginkgoBinary string, validate bool) error {
	if ginkgoBinary == "" {
		return errors.New("ginkgo binary is required")
	}
	if !validate {
		return nil
	}
	if _, err := exec.LookPath(ginkgoBinary); err != nil && strings.Contains(ginkgoBinary, string(os.PathSeparator)) {
		if _, statErr := os.Stat(ginkgoBinary); statErr != nil {
			return fmt.Errorf("ginkgo binary %q is not available: %w", ginkgoBinary, statErr)
		}
	} else if err != nil {
		return fmt.Errorf("ginkgo binary %q is not available: %w", ginkgoBinary, err)
	}
	return nil
}

func lanePackage(entry lane, defaults suiteDefaults) (string, error) {
	packagePath := entry.Package
	if packagePath == "" {
		packagePath = defaults.Package
	}
	if packagePath == "" {
		return "", fmt.Errorf("lane %q package is required", entry.ID)
	}
	return packagePath, nil
}

func laneTimeout(entry lane, defaults suiteDefaults) (string, error) {
	timeout := entry.Timeout
	if timeout == "" {
		timeout = defaults.Timeout
	}
	if timeout == "" {
		return "", fmt.Errorf("lane %q timeout is required", entry.ID)
	}
	return timeout, nil
}

func laneParallelNodes(entry lane, defaults suiteDefaults) (int, error) {
	parallelNodes := entry.ParallelNodes
	if parallelNodes == 0 {
		parallelNodes = defaults.ParallelNodes
	}
	if parallelNodes < 1 {
		return 0, fmt.Errorf("lane %q parallelNodes must be at least 1", entry.ID)
	}
	return parallelNodes, nil
}

func buildGinkgoArgs(
	entry lane,
	packagePath string,
	timeout string,
	parallelNodes int,
	reportDir string,
) []string {
	args := []string{
		"--tags=e2e",
		"--timeout=" + timeout,
	}
	if entry.RunRegex == "" {
		args = append(args,
			"--output-dir="+reportDir,
			"--junit-report=junit.xml",
			"--json-report=ginkgo.json",
		)
	}
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		args = append(args, "--github-output")
	}
	if parallelNodes != 1 {
		args = append(args, "--procs="+strconv.Itoa(parallelNodes))
	}
	if entry.RunRegex == "" && entry.LabelFilter != "" {
		args = append(args, "--label-filter="+entry.LabelFilter)
	}
	args = append(args, packagePath)
	if entry.RunRegex != "" {
		args = append(args, "--", "-test.run="+entry.RunRegex)
	}
	return args
}

func laneEnvironment(
	base []string,
	entry lane,
	kubernetesEntry *kubernetesPreviewEntry,
	validateRequired bool,
) ([]string, error) {
	env := append([]string{}, base...)
	if entry.EnableEnv != "" {
		env = setEnv(env, entry.EnableEnv, "true")
	}
	if kubernetesEntry != nil {
		env = setEnv(env, "E2E_KUBERNETES_LINE", kubernetesEntry.Line)
		env = setEnv(env, "E2E_KUBERNETES_VERSION", kubernetesEntry.ExactVersion)
		env = setEnv(env, "E2E_KIND_NODE_IMAGE", kubernetesEntry.KindNodeImage)
		env = setEnv(env, "E2E_KIND_CI", "true")
	}
	if entry.ProviderImageEnv != "" {
		value := lookupEnv(env, entry.ProviderImageEnv)
		if value == "" {
			if validateRequired {
				return nil, fmt.Errorf("lane %q requires %s to set E2E_PROVIDER_IMAGE", entry.ID, entry.ProviderImageEnv)
			}
		} else {
			env = setEnv(env, "E2E_PROVIDER_IMAGE", value)
		}
	}
	if validateRequired {
		for _, name := range entry.RequiredEnv {
			if lookupEnv(env, name) == "" {
				return nil, fmt.Errorf("lane %q requires %s", entry.ID, name)
			}
		}
	}
	return env, nil
}

func laneReportDir(artifactDir string, group string, laneID string, kubernetesEntry *kubernetesPreviewEntry) string {
	parts := []string{artifactDir, sanitizePathComponent(group)}
	if kubernetesEntry != nil {
		parts = append(parts, sanitizePathComponent(kubernetesEntry.Line))
	}
	parts = append(parts, sanitizePathComponent(laneID))
	return filepath.Join(parts...)
}

func sanitizePathComponent(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "/", "_")
	value = strings.ReplaceAll(value, "\\", "_")
	if value == "" || value == "." || value == ".." {
		return "unknown"
	}
	return value
}

func envDefault(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func lookupEnv(env []string, name string) string {
	prefix := name + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	return ""
}

func setEnv(env []string, name string, value string) []string {
	prefix := name + "="
	next := name + "=" + value
	for index, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			env[index] = next
			return env
		}
	}
	return append(env, next)
}
