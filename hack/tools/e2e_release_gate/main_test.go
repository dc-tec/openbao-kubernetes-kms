package main

import (
	"os"
	"path/filepath"
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

func writeVersionsFile(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "versions.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write versions: %v", err)
	}
	return path
}
