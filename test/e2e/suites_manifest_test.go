package e2e

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const e2eSuitesManifestPath = "suites.yaml"

type e2eSuitesManifest struct {
	Version       int              `yaml:"version"`
	VersionPolicy e2eVersionPolicy `yaml:"versionPolicy"`
	Defaults      e2eSuiteDefaults `yaml:"defaults"`
	Lanes         []e2eLane        `yaml:"lanes"`
}

type e2eVersionPolicy struct {
	Source     string `yaml:"source"`
	OpenBao    string `yaml:"openbao"`
	Kubernetes string `yaml:"kubernetes"`
}

type e2eSuiteDefaults struct {
	Package       string `yaml:"package"`
	ArtifactDir   string `yaml:"artifactDir"`
	Timeout       string `yaml:"timeout"`
	ParallelNodes int    `yaml:"parallelNodes"`
}

type e2eLane struct {
	ID            string   `yaml:"id"`
	Name          string   `yaml:"name"`
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

func TestE2EManifest(t *testing.T) {
	raw, err := os.ReadFile(e2eSuitesManifestPath)
	if err != nil {
		t.Fatalf("read %s: %v", e2eSuitesManifestPath, err)
	}
	if strings.Contains(strings.ToLower(string(raw)), "latest") {
		t.Fatalf("%s must not contain floating latest references", e2eSuitesManifestPath)
	}

	var manifest e2eSuitesManifest
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&manifest); err != nil {
		t.Fatalf("decode %s: %v", e2eSuitesManifestPath, err)
	}

	validateE2EManifest(t, manifest)
}

func validateE2EManifest(t *testing.T, manifest e2eSuitesManifest) {
	t.Helper()

	if manifest.Version != 1 {
		t.Fatalf("manifest version = %d, want 1", manifest.Version)
	}
	if manifest.VersionPolicy.Source != ".ci/versions.yaml" {
		t.Fatalf("version policy source = %q, want .ci/versions.yaml", manifest.VersionPolicy.Source)
	}
	if manifest.VersionPolicy.OpenBao == "" || manifest.VersionPolicy.Kubernetes == "" {
		t.Fatalf("version policy must reference OpenBao and Kubernetes version fields")
	}
	if manifest.Defaults.Package == "" {
		t.Fatalf("defaults.package is required")
	}
	if manifest.Defaults.ArtifactDir == "" {
		t.Fatalf("defaults.artifactDir is required")
	}
	if manifest.Defaults.Timeout == "" {
		t.Fatalf("defaults.timeout is required")
	}
	if manifest.Defaults.ParallelNodes < 1 {
		t.Fatalf("defaults.parallelNodes must be at least 1")
	}
	if len(manifest.Lanes) == 0 {
		t.Fatalf("at least one e2e lane is required")
	}

	seenIDs := make(map[string]struct{}, len(manifest.Lanes))
	for _, lane := range manifest.Lanes {
		validateE2ELane(t, lane, seenIDs)
	}
}

func validateE2ELane(t *testing.T, lane e2eLane, seenIDs map[string]struct{}) {
	t.Helper()

	if lane.ID == "" {
		t.Fatalf("lane id is required")
	}
	if _, ok := seenIDs[lane.ID]; ok {
		t.Fatalf("duplicate lane id %q", lane.ID)
	}
	seenIDs[lane.ID] = struct{}{}

	if lane.Name == "" {
		t.Fatalf("lane %q name is required", lane.ID)
	}
	if lane.Package == "" {
		t.Fatalf("lane %q package is required", lane.ID)
	}
	if lane.LabelFilter == "" {
		t.Fatalf("lane %q labelFilter is required", lane.ID)
	}
	if lane.PRScope == "" {
		t.Fatalf("lane %q prScope is required", lane.ID)
	}
	if lane.Environment == "" {
		t.Fatalf("lane %q environment is required", lane.ID)
	}
	if lane.Isolation == "" {
		t.Fatalf("lane %q isolation is required", lane.ID)
	}
	if lane.Timeout == "" {
		t.Fatalf("lane %q timeout is required", lane.ID)
	}
	if lane.ParallelNodes < 1 {
		t.Fatalf("lane %q parallelNodes must be at least 1", lane.ID)
	}
	if lane.Status != "active" && lane.Status != "planned" {
		t.Fatalf("lane %q status = %q, want active or planned", lane.ID, lane.Status)
	}
}
