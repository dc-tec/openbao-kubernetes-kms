package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallationKitsContainMatchingInputs(t *testing.T) {
	t.Chdir("../../..")
	for _, kind := range []string{kindSystemd, kindStaticPod} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			binary := filepath.Join(dir, binaryName)
			if err := os.WriteFile(binary, []byte("matching architecture binary"), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg := args{
				kind: kind, prefix: "kit", binaryPath: binary, output: filepath.Join(dir, "first.tar.gz"),
				imageRef: "ghcr.io/dc-tec/bao-kms-provider@sha256:" + strings.Repeat("a", 64), sourceDateEpoch: 1234,
			}
			if err := validateArgs(cfg); err != nil {
				t.Fatal(err)
			}
			if err := writeBundle(cfg); err != nil {
				t.Fatal(err)
			}
			first := readTestFile(t, cfg.output)
			entries := readTestArchive(t, first)
			for _, name := range []string{"README.md", "config/init-values-file.yaml", "config/init-values-oauth2.yaml"} {
				if len(entries["kit/"+name]) == 0 {
					t.Fatalf("missing installation input %s", name)
				}
			}
			if string(entries["kit/bin/"+binaryName]) != "matching architecture binary" {
				t.Fatal("kit does not contain the selected binary")
			}
			if kind == kindStaticPod && string(entries["kit/image-ref.txt"]) != cfg.imageRef+"\n" {
				t.Fatal("kit lost the selected image digest")
			}
			cfg.output = filepath.Join(dir, "second.tar.gz")
			if err := writeBundle(cfg); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(first, readTestFile(t, cfg.output)) {
				t.Fatal("bundle bytes are not reproducible")
			}
		})
	}
}

func TestStaticPodKitRequiresBinaryAndImageDigest(t *testing.T) {
	cfg := args{kind: kindStaticPod, prefix: "kit", output: "kit.tar.gz", binaryPath: "binary"}
	for _, ref := range []string{
		"", "ghcr.io/dc-tec/bao-kms-provider:dev", "ghcr.io/dc-tec/bao-kms-provider@sha256:short",
	} {
		cfg.imageRef = ref
		if err := validateArgs(cfg); err == nil {
			t.Fatalf("accepted unpinned image %q", ref)
		}
	}
	cfg.imageRef = "ghcr.io/dc-tec/bao-kms-provider@sha256:" + strings.Repeat("a", 64)
	cfg.binaryPath = ""
	if err := validateArgs(cfg); err == nil {
		t.Fatal("accepted a kit without a diagnostic binary")
	}
}

func readTestArchive(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	z, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = z.Close() })
	r := tar.NewReader(z)
	entries := make(map[string][]byte)
	for {
		header, readErr := r.Next()
		if errors.Is(readErr, io.EOF) {
			return entries
		}
		if readErr != nil {
			t.Fatal(readErr)
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		if header.Name == "kit/bin/"+binaryName && header.Mode != 0o755 {
			t.Fatal("diagnostic binary is not executable")
		}
		content, contentErr := io.ReadAll(r)
		if contentErr != nil {
			t.Fatal(contentErr)
		}
		entries[header.Name] = content
	}
}

func readTestFile(t *testing.T, path string) []byte {
	t.Helper()
	// #nosec G304 -- path is a generated archive inside the test's temporary directory.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
