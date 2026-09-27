package deployment_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPinnedReleaseDownload(t *testing.T) {
	for _, scenario := range []string{"valid", "corrupt", "missing-checksum", "bad-signature", "bad-attestation"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			fixture := filepath.Join(dir, "fixture")
			bin := filepath.Join(dir, "bin")
			for _, path := range []string{fixture, bin} {
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			artifact := "bao-kms-provider_0.2.0-preview.1_static-pod_linux_arm64.tar.gz"
			content := "fixture artifact"
			digest := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
			if scenario == "corrupt" {
				content = "corrupted artifact"
			}
			checksum := digest + "  " + artifact + "\n"
			if scenario == "missing-checksum" {
				checksum = digest + "  another-artifact\n"
			}
			writeDownloadFixture(t, filepath.Join(fixture, artifact), content)
			writeDownloadFixture(t, filepath.Join(fixture, "checksums.txt"), checksum)
			writeDownloadFixture(t, filepath.Join(fixture, "checksums.txt.bundle"), "signature fixture")
			writeDownloadFixture(t, filepath.Join(bin, "gh"), `#!/bin/sh
set -eu
if [ "$1" = release ]; then
  cp "$DOWNLOAD_FIXTURE/"* .
else
  printf '%s\n' "$@" > attestation-arguments
  [ "$DOWNLOAD_SCENARIO" != bad-attestation ]
fi
`)
			writeDownloadFixture(t, filepath.Join(bin, "cosign"), `#!/bin/sh
set -eu
printf '%s\n' "$@" > signature-arguments
[ "$DOWNLOAD_SCENARIO" != bad-signature ]
`)
			outDir := filepath.Join(dir, "download")
			// #nosec G204 -- fixed repository script and fixture arguments; verification tools are local test doubles.
			cmd := exec.Command("bash", repoPath("hack/install/download-release.sh"),
				"0.2.0-preview.1", "static-pod", "arm64", outDir)
			cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"),
				"DOWNLOAD_FIXTURE="+fixture, "DOWNLOAD_SCENARIO="+scenario)
			output, err := cmd.CombinedOutput()
			if scenario != "valid" {
				if err == nil || strings.Contains(string(output), "Verified ") {
					t.Fatalf("invalid download reported success: %s", output)
				}
				return
			}
			if err != nil {
				t.Fatalf("download: %v: %s", err, output)
			}
			assertDownloadScope(t, filepath.Join(outDir, "signature-arguments"),
				"https://github.com/dc-tec/openbao-kubernetes-kms/.github/workflows/release.yml@refs/tags/0.2.0-preview.1")
			assertDownloadScope(t, filepath.Join(outDir, "attestation-arguments"), "refs/tags/0.2.0-preview.1")
			// #nosec G204 -- repeat the same fixed fixture invocation to test destination protection.
			retry := exec.Command(cmd.Path, cmd.Args[1:]...)
			retry.Env = cmd.Env
			if output, err := retry.CombinedOutput(); err == nil {
				t.Fatalf("reused an existing destination: %s", output)
			}
		})
	}
}

func TestDownloadRejectsFloatingOrInvalidSelection(t *testing.T) {
	for _, selection := range [][]string{
		{"latest", "static-pod", "amd64"},
		{"main", "systemd", "arm64"},
		{"0.2.0", "unknown", "amd64"},
		{"0.2.0", "systemd", "386"},
	} {
		args := append([]string{repoPath("hack/install/download-release.sh")}, selection...)
		args = append(args, filepath.Join(t.TempDir(), "download"))
		// #nosec G204 -- fixed negative selections and a private temporary destination.
		if output, err := exec.Command("bash", args...).CombinedOutput(); err == nil {
			t.Fatalf("accepted invalid selection %v: %s", selection, output)
		}
	}
}

func writeDownloadFixture(t *testing.T, path, content string) {
	t.Helper()
	// #nosec G306 -- private temporary fixture tools must be executable.
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
}

func assertDownloadScope(t *testing.T, path, want string) {
	t.Helper()
	// #nosec G304 -- path is a tool argument recording in the test's temporary directory.
	content, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(content), want) {
		t.Fatalf("verification lost its release scope: %v: %s", err, content)
	}
}
