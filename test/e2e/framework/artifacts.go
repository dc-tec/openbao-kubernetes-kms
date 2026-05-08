//go:build e2e

package framework

import "os"

const (
	EnvArtifactDir     = "E2E_ARTIFACT_DIR"
	DefaultArtifactDir = "artifacts/e2e"
)

func ArtifactDir() string {
	return EnvDefault(EnvArtifactDir, DefaultArtifactDir)
}

func EnsureArtifactDir() (string, error) {
	dir := ArtifactDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}
