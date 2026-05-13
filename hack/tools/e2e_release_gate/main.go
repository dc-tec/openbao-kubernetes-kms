package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"gopkg.in/yaml.v3"
)

const defaultManifestPath = "test/e2e/suites.yaml"

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
	ID            string   `yaml:"id"`
	Name          string   `yaml:"name"`
	MakeTarget    string   `yaml:"makeTarget"`
	Package       string   `yaml:"package"`
	LabelFilter   string   `yaml:"labelFilter"`
	PRScope       string   `yaml:"prScope"`
	Environment   string   `yaml:"environment"`
	Isolation     string   `yaml:"isolation"`
	Timeout       string   `yaml:"timeout"`
	ParallelNodes int      `yaml:"parallelNodes"`
	RequiredEnv   []string `yaml:"requiredEnv"`
	EnableEnv     string   `yaml:"enableEnv"`
	VersionRefs   []string `yaml:"versionRefs"`
	Status        string   `yaml:"status"`
	Reports       bool     `yaml:"reports"`
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
	kubernetesLine := flags.String("kubernetes-line", os.Getenv("E2E_KUBERNETES_LINE"), "Kubernetes release line to run for Kind release gates")
	makeCommand := flags.String("make", "make", "Make command to execute")
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
	targets, err := releaseGateTargets(manifest, *group)
	if err != nil {
		return err
	}

	if *group == "kind" {
		kubernetesEntries, err := releaseGateKubernetesMatrix(*versionsPath, *kubernetesLine)
		if err != nil {
			return err
		}
		if *matrix {
			return printKubernetesMatrix(kubernetesEntries)
		}
		return runKubernetesMatrixTargets(*makeCommand, targets, kubernetesEntries, *dryRun)
	}
	if *matrix {
		return fmt.Errorf("-matrix is only supported for the kind release gate group")
	}

	for _, target := range targets {
		if *dryRun {
			fmt.Println(target)
			continue
		}
		fmt.Fprintf(os.Stderr, "==> %s %s\n", *makeCommand, target)
		cmd := exec.Command(*makeCommand, target)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s %s: %w", *makeCommand, target, err)
		}
	}
	return nil
}

func loadManifest(path string) (suitesManifest, error) {
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

func releaseGateTargets(manifest suitesManifest, group string) ([]string, error) {
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

	targets := make([]string, 0, len(laneIDs))
	for _, laneID := range laneIDs {
		entry, ok := lanes[laneID]
		if !ok {
			return nil, fmt.Errorf("release gate group %q references unknown lane %q", group, laneID)
		}
		if entry.Status != "active" {
			return nil, fmt.Errorf("release gate lane %q status must be active, got %q", laneID, entry.Status)
		}
		if entry.MakeTarget == "" {
			return nil, fmt.Errorf("release gate lane %q has no make target", laneID)
		}
		targets = append(targets, entry.MakeTarget)
	}
	return targets, nil
}

func loadVersions(path string) (versionsConfig, error) {
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

func runKubernetesMatrixTargets(makeCommand string, targets []string, entries []kubernetesPreviewEntry, dryRun bool) error {
	for _, entry := range entries {
		for _, target := range targets {
			if dryRun {
				fmt.Printf("kubernetes=%s version=%s image=%s target=%s\n", entry.Line, entry.ExactVersion, entry.KindNodeImage, target)
				continue
			}
			fmt.Fprintf(os.Stderr, "==> E2E_KUBERNETES_LINE=%s E2E_KIND_NODE_IMAGE=%s %s %s\n", entry.Line, entry.KindNodeImage, makeCommand, target)
			cmd := exec.Command(makeCommand, target)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			cmd.Stdin = os.Stdin
			cmd.Env = append(os.Environ(),
				"E2E_KUBERNETES_LINE="+entry.Line,
				"E2E_KUBERNETES_VERSION="+entry.ExactVersion,
				"E2E_KIND_NODE_IMAGE="+entry.KindNodeImage,
			)
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("%s %s for Kubernetes %s: %w", makeCommand, target, entry.Line, err)
			}
		}
	}
	return nil
}
