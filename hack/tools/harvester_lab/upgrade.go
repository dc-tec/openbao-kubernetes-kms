package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/scaffold"
)

// Container runtimes can report an image ID in image.image and retain the
// requested tag separately in userSpecifiedImage.
const criRequestedImageMatch = `(.image.image == $image or .image.userSpecifiedImage == $image)`

func pullProviderBaseline(ctx context.Context, cfg *labConfig) (string, error) {
	repository, digest, ok := strings.Cut(cfg.providerBaselineImage, "@")
	if !ok {
		return "", errors.New("provider upgrade baseline must be pinned by digest in .ci/versions.yaml")
	}
	// The version policy can include a human-readable tag before its digest.
	if colon := strings.LastIndex(repository, ":"); colon > strings.LastIndex(repository, "/") {
		repository = repository[:colon]
	}
	if err := scaffold.ValidateImageDigest(repository + "@" + digest); err != nil {
		return "", fmt.Errorf("provider upgrade baseline: %w", err)
	}
	output, err := outputCmd(ctx, cfg, "docker", "manifest", "inspect", cfg.providerBaselineImage)
	if err != nil {
		return "", err
	}
	image, err := baselineAMD64Image(repository, output)
	if err != nil {
		return "", err
	}
	// Pull the selected child digest so an existing arm64 image under the index
	// digest remains intact on hosts using Docker's classic image store.
	return image, runCmd(ctx, cfg, "docker", "pull", "--platform", "linux/amd64", image)
}

func extractProviderBaseline(ctx context.Context, cfg *labConfig, destination string) (resultErr error) {
	image, err := pullProviderBaseline(ctx, cfg)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.providerAssetDir, 0o750); err != nil {
		return fmt.Errorf("create provider asset directory: %w", err)
	}
	// Create only: the published provider is never started during extraction.
	output, err := outputCmd(ctx, cfg, "docker", "create", "--platform", "linux/amd64", image)
	if err != nil {
		return err
	}
	container := strings.TrimSpace(string(output))
	if container == "" || strings.ContainsAny(container, " \t\r\n") || strings.HasPrefix(container, "-") {
		return errors.New("docker create returned an invalid baseline container ID")
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if err := runCmd(cleanupCtx, cfg, "docker", "rm", container); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("remove baseline extraction container: %w", err))
		}
	}()
	if err := runCmd(ctx, cfg, "docker", "cp", container+":/bao-kms-provider", destination); err != nil {
		return fmt.Errorf("extract published provider binary: %w", err)
	}
	return nil
}

func saveProviderBaseline(ctx context.Context, cfg *labConfig, image, archive string) error {
	baseline, err := pullProviderBaseline(ctx, cfg)
	if err != nil {
		return err
	}
	if err := runCmd(ctx, cfg, "docker", "tag", baseline, image); err != nil {
		return err
	}
	return runCmd(ctx, cfg, "docker", "save", image, "-o", archive)
}

func baselineAMD64Image(repository string, data []byte) (string, error) {
	var index struct {
		Manifests []struct {
			Digest   string `json:"digest"`
			Platform struct {
				Architecture string `json:"architecture"`
				OS           string `json:"os"`
			} `json:"platform"`
		} `json:"manifests"`
	}
	if err := json.Unmarshal(data, &index); err != nil {
		return "", fmt.Errorf("decode baseline image index: %w", err)
	}
	selected := ""
	for _, manifest := range index.Manifests {
		if manifest.Platform.Architecture != "amd64" || manifest.Platform.OS != "linux" {
			continue
		}
		if selected != "" {
			return "", errors.New("baseline image index has multiple linux/amd64 manifests")
		}
		selected = repository + "@" + manifest.Digest
		if err := scaffold.ValidateImageDigest(selected); err != nil {
			return "", fmt.Errorf("baseline linux/amd64 digest: %w", err)
		}
	}
	if selected == "" {
		return "", errors.New("baseline image index has no linux/amd64 manifest")
	}
	return selected, nil
}

func waitStaticPodImage(ctx context.Context, cfg *labConfig, host, image string) error {
	command := "sudo crictl --config /dev/null --runtime-endpoint unix:///run/containerd/containerd.sock " +
		"ps --name '^bao-kms-provider$' -o json | jq -e --arg image " + shellQuote(image) +
		" '.containers | any(" + criRequestedImageMatch + " and .state == \"CONTAINER_RUNNING\")' >/dev/null"
	return waitRemoteCommand(ctx, cfg, host, "selected provider image", 3*time.Minute, command)
}

func requireDifferentBinaries(baseline, candidate string) error {
	// #nosec G304 -- paths are local upgrade artifacts selected by the lab harness.
	oldBytes, err := os.ReadFile(baseline)
	if err != nil {
		return err
	}
	// #nosec G304 -- paths are local upgrade artifacts selected by the lab harness.
	newBytes, err := os.ReadFile(candidate)
	if err != nil {
		return err
	}
	if len(oldBytes) == 0 || len(newBytes) == 0 || sha256.Sum256(oldBytes) == sha256.Sum256(newBytes) {
		return errors.New("upgrade requires distinct, non-empty baseline and candidate binaries")
	}
	return nil
}

func requireDifferentImages(ctx context.Context, cfg *labConfig, baseline, candidate string) error {
	output, err := outputCmd(ctx, cfg, "docker", "image", "inspect", "--format", "{{.Id}}", baseline, candidate)
	if err != nil {
		return err
	}
	ids := strings.Fields(string(output))
	if len(ids) != 2 || ids[0] == ids[1] {
		return errors.New("upgrade requires distinct baseline and candidate images")
	}
	return nil
}

// verifyRejectedDowngrade leaves the candidate installed, including on failure.
func verifyRejectedDowngrade(
	ctx context.Context, cfg *labConfig, check kubeadmCheck,
	installBaseline, restoreCandidate, waitRejection func(context.Context) error,
) (resultErr error) {
	secret, cleanup, err := createTrackedSecret(ctx, cfg, check, "downgrade-"+check.suffix)
	if err != nil {
		return err
	}
	defer cleanup()
	if err := verifyRemoteSecretEnvelope(ctx, cfg, check.host, secret.name, secret.path); err != nil {
		return err
	}
	defer func() {
		recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Minute)
		defer cancel()
		if err := restoreCandidate(recoveryCtx); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("restore candidate after rejected downgrade: %w", err))
			return
		}
		if err := verifyRemoteSecretEnvelope(recoveryCtx, cfg, check.host, secret.name, secret.path); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("cold read after restoring candidate: %w", err))
		}
	}()
	// Quiesce the writer so any file change is attributable to the old release.
	if err := stopProviderForRestore(ctx, cfg, check); err != nil {
		return err
	}
	before, err := remotePersistenceHashes(ctx, cfg, check.host)
	if err != nil {
		return err
	}
	if err := installBaseline(ctx); err != nil {
		return err
	}
	if err := waitRejection(ctx); err != nil {
		return err
	}
	after, err := remotePersistenceHashes(ctx, cfg, check.host)
	if err != nil {
		return err
	}
	if before != after {
		return errors.New("rejected downgrade changed the registry or checkpoint")
	}
	fmt.Printf("published baseline rejected bound state without changing either file on %s\n", check.host)
	return nil
}

func remotePersistenceHashes(ctx context.Context, cfg *labConfig, host string) (string, error) {
	output, err := sshLabOutput(ctx, cfg, host, "sudo sha256sum /var/lib/openbao-kms/state/key-registry.json "+
		"/var/lib/openbao-kms/state/key-registry.json.checkpoint")
	if err != nil {
		return "", err
	}
	if len(strings.Fields(string(output))) != 4 {
		return "", errors.New("registry/checkpoint hashes were not returned")
	}
	return string(output), nil
}

func waitSystemdStateRejection(ctx context.Context, cfg *labConfig, host string) error {
	// systemctl restart creates a new invocation. Scope the journal to that
	// invocation so an earlier failed deployment cannot satisfy this assertion.
	command := `id=$(sudo systemctl show bao-kms-provider.service -p InvocationID --value); ` +
		`test -n "$id" && sudo journalctl --no-pager -o cat _SYSTEMD_INVOCATION_ID="$id" | ` +
		`grep -F 'unknown field' | grep -F 'identityFingerprint' >/dev/null`
	return waitRemoteCommand(ctx, cfg, host, "published baseline state rejection", time.Minute,
		"sh -c "+shellQuote(command))
}

func waitStaticPodStateRejection(ctx context.Context, cfg *labConfig, host, image string) error {
	crictl := "sudo crictl --config /dev/null --runtime-endpoint unix:///run/containerd/containerd.sock"
	command := "id=$(" + crictl + " ps -a --name '^bao-kms-provider$' -o json | jq -r --arg image " +
		shellQuote(image) + ` '.containers | map(select(` + criRequestedImageMatch + ` and .state == "CONTAINER_EXITED"))` +
		` | sort_by(.createdAt) | last | .id // empty'); ` +
		`test -n "$id" && ` + crictl + ` logs "$id" 2>&1 | ` +
		`grep -F 'unknown field' | grep -F 'identityFingerprint' >/dev/null`
	return waitRemoteCommand(ctx, cfg, host, "published baseline state rejection", 3*time.Minute,
		"sh -c "+shellQuote(command))
}
