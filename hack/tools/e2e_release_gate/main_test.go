package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseGateKubernetesMatrix(t *testing.T) {
	versionsPath := writeVersionsFile(t, `
validation:
  kubernetes:
    previewMatrix:
      - line: "1.34"
        upstreamPatch: "1.34.8"
        exactVersion: "1.34.3"
        kindNodeImage: kindest/node:v1.34.3@sha256:1111
        kindNodeImageDigest: sha256:1111
        releaseGate: true
      - line: "1.35"
        upstreamPatch: "1.35.5"
        exactVersion: "1.35.0"
        kindNodeImage: kindest/node:v1.35.0@sha256:2222
        kindNodeImageDigest: sha256:2222
        releaseGate: true
      - line: "1.36"
        upstreamPatch: "1.36.1"
        exactVersion: ""
        kindNodeImage: ""
        kindNodeImageDigest: ""
        releaseGate: false
`)

	entries, err := releaseGateKubernetesMatrix(versionsPath, "")
	if err != nil {
		t.Fatalf("releaseGateKubernetesMatrix: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("matrix entries = %d, want 2", len(entries))
	}
	if entries[0].Line != "1.34" || entries[1].Line != "1.35" {
		t.Fatalf("matrix lines = %q, %q; want 1.34, 1.35", entries[0].Line, entries[1].Line)
	}

	selected, err := releaseGateKubernetesMatrix(versionsPath, "1.35")
	if err != nil {
		t.Fatalf("releaseGateKubernetesMatrix selected: %v", err)
	}
	if len(selected) != 1 || selected[0].Line != "1.35" {
		t.Fatalf("selected matrix = %#v, want only 1.35", selected)
	}
}

func TestReleaseGateKubernetesMatrixRejectsMissingDigestPin(t *testing.T) {
	versionsPath := writeVersionsFile(t, `
validation:
  kubernetes:
    previewMatrix:
      - line: "1.35"
        exactVersion: "1.35.0"
        kindNodeImage: kindest/node:v1.35.0
        kindNodeImageDigest: sha256:2222
        releaseGate: true
`)

	if _, err := releaseGateKubernetesMatrix(versionsPath, ""); err == nil {
		t.Fatal("releaseGateKubernetesMatrix succeeded without digest-pinned kindNodeImage")
	}
}

func TestReleaseGateLanesUseManifestSelectors(t *testing.T) {
	manifest := suitesManifest{
		ReleaseGate: releaseGate{
			Preview: releaseGateDefinition{
				Status: "active",
				Groups: map[string][]string{
					"openbao": {"openbao-ha-ci"},
				},
			},
		},
		Lanes: []lane{{
			ID:          "openbao-ha-ci",
			Name:        "OpenBao HA",
			LabelFilter: "openbao && kmsv2 && ha && ci",
			RunRegex:    "^TestProviderOpenBaoHAFailoverE2E$",
			Status:      "active",
		}},
	}

	lanes, err := releaseGateLanes(manifest, "openbao")
	if err != nil {
		t.Fatalf("releaseGateLanes: %v", err)
	}
	if len(lanes) != 1 || lanes[0].RunRegex != "^TestProviderOpenBaoHAFailoverE2E$" {
		t.Fatalf("lanes = %#v, want HA runRegex from manifest", lanes)
	}
}

func TestReleaseGateLanesRejectMissingSelector(t *testing.T) {
	manifest := suitesManifest{
		ReleaseGate: releaseGate{
			Preview: releaseGateDefinition{
				Status: "active",
				Groups: map[string][]string{
					"openbao": {"openbao-ha-ci"},
				},
			},
		},
		Lanes: []lane{{
			ID:     "openbao-ha-ci",
			Name:   "OpenBao HA",
			Status: "active",
		}},
	}

	if _, err := releaseGateLanes(manifest, "openbao"); err == nil {
		t.Fatal("releaseGateLanes succeeded without labelFilter or runRegex")
	}
}

func TestSelectLane(t *testing.T) {
	lanes := []lane{
		{ID: "openbao-ci"},
		{ID: "openbao-ha-ci"},
	}

	selected, err := selectLane(lanes, "openbao-ha-ci")
	if err != nil {
		t.Fatalf("selectLane: %v", err)
	}
	if len(selected) != 1 || selected[0].ID != "openbao-ha-ci" {
		t.Fatalf("selected lanes = %#v, want openbao-ha-ci", selected)
	}
	if _, err := selectLane(lanes, "missing"); err == nil {
		t.Fatal("selectLane succeeded for a lane outside the group")
	}
}

func TestBuildLaneCommandUsesRunRegex(t *testing.T) {
	t.Setenv("E2E_PROVIDER_IMAGE", "example/provider:test")
	ginkgoBinary, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	command, err := buildLaneCommand(
		ginkgoBinary,
		"artifacts/e2e",
		suiteDefaults{Package: "./test/e2e", Timeout: "30m", ParallelNodes: 1},
		"openbao",
		lane{
			ID:          "openbao-ha-ci",
			LabelFilter: "openbao && kmsv2 && ha && ci",
			RunRegex:    "^TestProviderOpenBaoHAFailoverE2E$",
			Timeout:     "10m",
			EnableEnv:   "E2E_OPENBAO_CI",
			RequiredEnv: []string{"E2E_PROVIDER_IMAGE"},
		},
		nil,
		true,
	)
	if err != nil {
		t.Fatalf("buildLaneCommand: %v", err)
	}

	joined := strings.Join(command.Args, " ")
	for _, want := range []string{
		"--tags=e2e",
		"--timeout=10m",
		"./test/e2e",
		"-test.run=^TestProviderOpenBaoHAFailoverE2E$",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("command args %q must contain %q", joined, want)
		}
	}
	if strings.Contains(joined, "--label-filter") {
		t.Fatalf("command args %q must not label-filter a runRegex lane", joined)
	}
	for _, unwanted := range []string{"--junit-report", "--json-report"} {
		if strings.Contains(joined, unwanted) {
			t.Fatalf("command args %q must not request Ginkgo reports for a runRegex lane", joined)
		}
	}
	if got := lookupEnv(command.Env, "E2E_OPENBAO_CI"); got != "true" {
		t.Fatalf("E2E_OPENBAO_CI = %q, want true", got)
	}
}

func TestBuildLaneCommandUsesGinkgoReportsForLabelLane(t *testing.T) {
	ginkgoBinary, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	command, err := buildLaneCommand(
		ginkgoBinary,
		"artifacts/e2e",
		suiteDefaults{Package: "./test/e2e", Timeout: "30m", ParallelNodes: 1},
		"openbao",
		lane{
			ID:          "openbao-ci",
			LabelFilter: "openbao && transit && ci",
			Timeout:     "6m",
			EnableEnv:   "E2E_OPENBAO_CI",
		},
		nil,
		true,
	)
	if err != nil {
		t.Fatalf("buildLaneCommand: %v", err)
	}

	joined := strings.Join(command.Args, " ")
	for _, want := range []string{
		"--output-dir=artifacts/e2e/openbao/openbao-ci",
		"--junit-report=junit.xml",
		"--json-report=ginkgo.json",
		"--label-filter=openbao && transit && ci",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("command args %q must contain %q", joined, want)
		}
	}
}

func TestBuildKindLaneCommandSetsMatrixEnvironment(t *testing.T) {
	t.Setenv("E2E_PROVIDER_IMAGE", "example/provider:test")
	ginkgoBinary, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	command, err := buildLaneCommand(
		ginkgoBinary,
		"artifacts/e2e",
		suiteDefaults{Package: "./test/e2e", Timeout: "30m", ParallelNodes: 1},
		"kind",
		lane{
			ID:          "kind-smoke",
			LabelFilter: "kind && kmsv2 && smoke",
			RunRegex:    "^TestKindKMSV2SmokeE2E$",
			RequiredEnv: []string{"E2E_PROVIDER_IMAGE", "E2E_KIND_NODE_IMAGE"},
		},
		&kubernetesPreviewEntry{
			Line:          "1.35",
			ExactVersion:  "1.35.0",
			KindNodeImage: "kindest/node:v1.35.0@sha256:2222",
		},
		true,
	)
	if err != nil {
		t.Fatalf("buildLaneCommand: %v", err)
	}
	if command.ReportDir != filepath.Join("artifacts/e2e", "kind", "1.35", "kind-smoke") {
		t.Fatalf("ReportDir = %q", command.ReportDir)
	}
	for name, want := range map[string]string{
		"E2E_KIND_CI":            "true",
		"E2E_KUBERNETES_LINE":    "1.35",
		"E2E_KUBERNETES_VERSION": "1.35.0",
		"E2E_KIND_NODE_IMAGE":    "kindest/node:v1.35.0@sha256:2222",
	} {
		if got := lookupEnv(command.Env, name); got != want {
			t.Fatalf("%s = %q, want %q", name, got, want)
		}
	}
}

func writeVersionsFile(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "versions.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write versions: %v", err)
	}
	return path
}
