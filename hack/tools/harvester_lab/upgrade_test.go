package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const testUpgradeBaseline = "ghcr.io/dc-tec/bao-kms-provider:preview@sha256:" +
	"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

const testUpgradeAMD64 = "ghcr.io/dc-tec/bao-kms-provider@sha256:" +
	"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

const testUpgradeManifest = `{"digest":"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
"platform":{"os":"linux","architecture":"amd64"}}`

func TestCRIRequestedImageSelection(t *testing.T) {
	for _, tc := range []struct {
		name, imageJSON string
		want            bool
	}{
		{"tag in image", `{"image":"repo:candidate"}`, true},
		{"resolved ID and requested tag", `{"image":"sha256:abc","userSpecifiedImage":"repo:candidate"}`, true},
		{"different requested tag", `{"image":"sha256:abc","userSpecifiedImage":"repo:old"}`, false},
		{"ID without requested tag", `{"image":"sha256:abc"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			command := exec.Command("jq", "-e", "--arg", "image", "repo:candidate", criRequestedImageMatch)
			command.Stdin = strings.NewReader(`{"image":` + tc.imageJSON + `}`)
			output, err := command.CombinedOutput()
			if (err == nil) != tc.want || strings.TrimSpace(string(output)) != fmt.Sprint(tc.want) {
				t.Fatalf("image match = %s, error = %v; want %v", output, err, tc.want)
			}
		})
	}
}

func TestUpgradeModeRejectsInvalidSelectionBeforeAccess(t *testing.T) {
	for _, args := range [][]string{{"other"}, {"systemd", "static-pod"}} {
		if err := labVerifyUpgradeRollback(context.Background(), nil, args); err == nil {
			t.Fatalf("accepted invalid mode arguments %v", args)
		}
	}
}

func TestExtractPublishedBaseline(t *testing.T) {
	for _, scenario := range []string{"success", "pull-failed", "copy-failed", "cleanup-failed"} {
		t.Run(scenario, func(t *testing.T) {
			cfg, logPath := stubUpgradeDocker(t)
			t.Setenv("KMS_TEST_DOCKER_SCENARIO", scenario)
			destination := filepath.Join(cfg.providerAssetDir, "baseline")
			err := extractProviderBaseline(context.Background(), cfg, destination)
			if (err == nil) != (scenario == "success") {
				t.Fatalf("extract baseline: %v", err)
			}
			log := readUpgradeTestFile(t, logPath)
			if !strings.Contains(log, "pull --platform linux/amd64 "+testUpgradeAMD64+"\n") {
				t.Fatalf("baseline was not pulled by the selected digest: %s", log)
			}
			if scenario == "pull-failed" {
				if strings.Contains(log, "create") || strings.Contains(log, "cp") {
					t.Fatalf("continued after failed pull: %s", log)
				}
				return
			}
			if !strings.Contains(log, "cp fixture-container:/bao-kms-provider "+destination+"\n") ||
				!strings.HasSuffix(log, "rm fixture-container\n") {
				t.Fatalf("did not extract the binary and clean up only its container: %s", log)
			}
			if strings.Contains(log, "start") || strings.Contains(log, "run ") || strings.Contains(log, "prune") {
				t.Fatalf("extraction started a container or pruned resources: %s", log)
			}
			if scenario == "success" && readUpgradeTestFile(t, destination) != "published binary" {
				t.Fatal("did not use the extracted binary")
			}
		})
	}
}

func TestBaselineRequiresDigest(t *testing.T) {
	for _, ref := range []string{"", "ghcr.io/dc-tec/bao-kms-provider:preview", "repo@sha256:bad"} {
		t.Run(ref, func(t *testing.T) {
			cfg, logPath := stubUpgradeDocker(t)
			cfg.providerBaselineImage = ref
			if _, err := pullProviderBaseline(context.Background(), cfg); err == nil {
				t.Fatal("accepted an unpinned or malformed baseline")
			}
			if _, err := os.Stat(logPath); !os.IsNotExist(err) {
				t.Fatal("invoked Docker before validating the baseline")
			}
		})
	}
	versions, err := loadVersions(filepath.Join("..", "..", "..", ".ci", "versions.yaml"))
	if err != nil || versions.Validation.Provider.UpgradeBaselineImage == "" {
		t.Fatalf("baseline missing from version policy: %v", err)
	}
}

func TestBaselineSelectsOnlyLinuxAMD64(t *testing.T) {
	for _, data := range []string{
		"invalid", `{}`, `{"manifests":[]}`,
		`{"manifests":[` + strings.ReplaceAll(testUpgradeManifest, "amd64", "arm64") + `]}`,
		`{"manifests":[` + testUpgradeManifest + `,` + testUpgradeManifest + `]}`,
		`{"manifests":[` + strings.ReplaceAll(testUpgradeManifest, "sha256:", "bad:") + `]}`,
	} {
		if _, err := baselineAMD64Image("ghcr.io/dc-tec/bao-kms-provider", []byte(data)); err == nil {
			t.Fatalf("accepted invalid baseline index: %s", data)
		}
	}
	data := `{"manifests":[` + testUpgradeManifest + `,` +
		strings.ReplaceAll(testUpgradeManifest, "amd64", "arm64") + `]}`
	image, err := baselineAMD64Image("ghcr.io/dc-tec/bao-kms-provider", []byte(data))
	if err != nil || image != testUpgradeAMD64 {
		t.Fatalf("selected baseline = %q, error = %v", image, err)
	}
}

func TestUpgradeRejectsIdenticalArtifacts(t *testing.T) {
	cfg, _ := stubUpgradeDocker(t)
	baseline, candidate := filepath.Join(cfg.root, "old"), filepath.Join(cfg.root, "new")
	mustWriteFile(t, baseline, "published binary")
	for _, content := range []string{"", "published binary", "candidate binary"} {
		mustWriteFile(t, candidate, content)
		err := requireDifferentBinaries(baseline, candidate)
		if (err == nil) != (content == "candidate binary") {
			t.Fatalf("binary distinction for %q: %v", content, err)
		}
	}
	for _, ids := range []string{"", "sha256:a\nsha256:a", "sha256:a\nsha256:b"} {
		t.Setenv("KMS_TEST_IMAGE_IDS", ids)
		err := requireDifferentImages(context.Background(), cfg, "baseline", "candidate")
		if (err == nil) != strings.Contains(ids, "sha256:b") {
			t.Fatalf("image distinction for %q: %v", ids, err)
		}
	}
}

func stubUpgradeDocker(t *testing.T) (*labConfig, string) {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "docker.log")
	t.Setenv("KMS_TEST_DOCKER_LOG", logPath)
	t.Setenv("KMS_TEST_IMAGE_INDEX", `{"manifests":[`+testUpgradeManifest+`]}`)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	script := `#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$KMS_TEST_DOCKER_LOG"
case "$1" in
  manifest) printf '%s\n' "$KMS_TEST_IMAGE_INDEX" ;;
  pull) test "${KMS_TEST_DOCKER_SCENARIO:-}" != pull-failed ;;
  create) printf 'fixture-container\n' ;;
  cp)
    test "${KMS_TEST_DOCKER_SCENARIO:-}" != copy-failed
    printf 'published binary' > "$3"
    ;;
  rm) test "${KMS_TEST_DOCKER_SCENARIO:-}" != cleanup-failed ;;
  image) printf '%s\n' "$KMS_TEST_IMAGE_IDS" ;;
  *) exit 99 ;;
esac
`
	// #nosec G306 -- executable Docker stub in an isolated test directory.
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return &labConfig{
		root: dir, providerAssetDir: filepath.Join(dir, "assets"), providerBaselineImage: testUpgradeBaseline,
	}, logPath
}

func readUpgradeTestFile(t *testing.T, path string) string {
	t.Helper()
	// #nosec G304 -- test reads its own temporary artifacts.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestRejectedDowngradeRestoresCandidateAfterCancellation(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"ssh", "scp", "kubectl"} {
		script := "#!/bin/sh\nexit 0\n"
		if name == "ssh" {
			script = "#!/bin/sh\ncase \"$*\" in *sha256sum*) printf 'a state\\nb checkpoint\\n';; esac\n"
		}
		// #nosec G306 -- isolated command stubs; this test never accesses a VM.
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	restored := false
	err := verifyRejectedDowngrade(ctx, &labConfig{root: dir}, kubeadmCheck{providerMode: providerModeSystemd},
		func(context.Context) error {
			cancel()
			return context.Canceled
		},
		func(recoveryCtx context.Context) error {
			if err := recoveryCtx.Err(); err != nil {
				t.Fatalf("candidate restoration inherited canceled context: %v", err)
			}
			restored = true
			return nil
		},
		func(context.Context) error { t.Fatal("continued after failed install"); return nil },
	)
	if !errors.Is(err, context.Canceled) || !restored {
		t.Fatalf("error = %v, candidate restored = %v", err, restored)
	}
}
