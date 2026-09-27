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
		" '.containers | any(.image.image == $image and .state == \"CONTAINER_RUNNING\")' >/dev/null"
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
