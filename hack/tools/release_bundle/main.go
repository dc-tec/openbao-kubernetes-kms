package main

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	binaryName    = "bao-kms-provider"
	kindSystemd   = "systemd"
	kindStaticPod = "static-pod"
)

type args struct {
	kind            string
	output          string
	prefix          string
	binaryPath      string
	imageRef        string
	sourceDateEpoch int64
}

type bundleEntry struct {
	name    string
	source  string
	content string
	mode    int64
}

func main() {
	cfg, err := parseArgs()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(2)
	}

	if err := writeBundle(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Wrote %s\n", cfg.output)
}

func parseArgs() (args, error) {
	cfg := args{}
	flag.StringVar(&cfg.kind, "kind", "", "bundle kind: systemd or static-pod")
	flag.StringVar(&cfg.output, "output", "", "output tar.gz path")
	flag.StringVar(&cfg.prefix, "prefix", "", "top-level archive prefix")
	flag.StringVar(&cfg.binaryPath, "binary", "", "systemd bundle binary path")
	flag.StringVar(&cfg.imageRef, "image-ref", "", "static-pod bundle image reference")
	flag.Int64Var(&cfg.sourceDateEpoch, "source-date-epoch", 0, "deterministic entry timestamp")
	flag.Parse()

	if cfg.kind != kindSystemd && cfg.kind != kindStaticPod {
		return cfg, errors.New("-kind must be systemd or static-pod")
	}
	if cfg.output == "" {
		return cfg, errors.New("-output is required")
	}
	if cfg.prefix == "" {
		return cfg, errors.New("-prefix is required")
	}
	if strings.Contains(cfg.prefix, "..") || strings.HasPrefix(cfg.prefix, "/") {
		return cfg, errors.New("-prefix must be a relative archive path")
	}
	if cfg.kind == kindSystemd && cfg.binaryPath == "" {
		return cfg, errors.New("-binary is required for systemd bundles")
	}
	if cfg.kind == kindStaticPod && cfg.imageRef == "" {
		return cfg, errors.New("-image-ref is required for static-pod bundles")
	}
	return cfg, nil
}

func writeBundle(cfg args) error {
	entries := bundleEntries(cfg)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].name < entries[j].name
	})

	// #nosec G301 -- release artifacts are intentionally world-readable.
	if err := os.MkdirAll(filepath.Dir(cfg.output), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	// #nosec G304 -- release tooling writes the workflow-provided artifact path.
	file, err := os.Create(cfg.output)
	if err != nil {
		return fmt.Errorf("create output file: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	modTime := time.Unix(cfg.sourceDateEpoch, 0).UTC()
	gzipWriter, err := gzip.NewWriterLevel(file, gzip.BestCompression)
	if err != nil {
		return fmt.Errorf("create gzip writer: %w", err)
	}
	gzipWriter.ModTime = modTime
	tarWriter := tar.NewWriter(gzipWriter)

	if err := writeDirectories(tarWriter, cfg.prefix, entries, modTime); err != nil {
		return err
	}
	for _, entry := range entries {
		if err := writeFile(tarWriter, cfg.prefix, entry, modTime); err != nil {
			return err
		}
	}
	if err := tarWriter.Close(); err != nil {
		return fmt.Errorf("close tar writer: %w", err)
	}
	if err := gzipWriter.Close(); err != nil {
		return fmt.Errorf("close gzip writer: %w", err)
	}
	return nil
}

func bundleEntries(cfg args) []bundleEntry {
	switch cfg.kind {
	case kindSystemd:
		return []bundleEntry{
			{name: "README.md", source: "deploy/package/bundles/systemd/README.md", mode: 0o644},
			{name: "LICENSE", source: "LICENSE", mode: 0o644},
			{name: "bin/" + binaryName, source: cfg.binaryPath, mode: 0o755},
			{name: "config/provider-systemd.yaml", source: "deploy/config/provider-systemd.yaml", mode: 0o644},
			{name: "kubernetes/encryption-config.yaml", source: "deploy/kubernetes/encryption-config.yaml", mode: 0o644},
			{name: "systemd/bao-kms-provider.service", source: "deploy/systemd/bao-kms-provider.service", mode: 0o644},
			{name: "sysusers.d/openbao-kms.conf", source: "deploy/package/linux/sysusers.d/openbao-kms.conf", mode: 0o644},
			{name: "tmpfiles.d/openbao-kms.conf", source: "deploy/package/linux/tmpfiles.d/openbao-kms.conf", mode: 0o644},
		}
	case kindStaticPod:
		return []bundleEntry{
			{name: "README.md", source: "deploy/package/bundles/static-pod/README.md", mode: 0o644},
			{name: "LICENSE", source: "LICENSE", mode: 0o644},
			{name: "config/provider-static-pod.yaml", source: "deploy/config/provider-static-pod.yaml", mode: 0o644},
			{name: "image-ref.txt", content: cfg.imageRef + "\n", mode: 0o644},
			{name: "kubernetes/encryption-config.yaml", source: "deploy/kubernetes/encryption-config.yaml", mode: 0o644},
			{name: "static-pod/bao-kms-provider.yaml", source: "deploy/static-pod/bao-kms-provider.yaml", mode: 0o644},
		}
	default:
		return nil
	}
}

func writeDirectories(writer *tar.Writer, prefix string, entries []bundleEntry, modTime time.Time) error {
	dirs := []string{path.Clean(prefix)}
	for _, entry := range entries {
		dir := path.Dir(path.Join(prefix, entry.name))
		for dir != "." && dir != "/" {
			dirs = append(dirs, dir)
			parent := path.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	sort.Strings(dirs)

	seen := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		if containsString(seen, dir) {
			continue
		}
		seen = append(seen, dir)
		header := tar.Header{
			Name:     dir + "/",
			Typeflag: tar.TypeDir,
			Mode:     0o755,
			Uid:      0,
			Gid:      0,
			Uname:    "root",
			Gname:    "root",
			ModTime:  modTime,
		}
		if err := writer.WriteHeader(&header); err != nil {
			return fmt.Errorf("write directory %s: %w", dir, err)
		}
	}
	return nil
}

func writeFile(writer *tar.Writer, prefix string, entry bundleEntry, modTime time.Time) error {
	archiveName := path.Join(prefix, entry.name)
	var reader io.Reader
	var size int64
	if entry.source != "" {
		// #nosec G304 -- bundle entries are fixed by this tool, except the already-built binary path.
		file, err := os.Open(entry.source)
		if err != nil {
			return fmt.Errorf("open %s: %w", entry.source, err)
		}
		defer func() {
			_ = file.Close()
		}()
		info, err := file.Stat()
		if err != nil {
			return fmt.Errorf("stat %s: %w", entry.source, err)
		}
		reader = file
		size = info.Size()
	} else {
		reader = strings.NewReader(entry.content)
		size = int64(len(entry.content))
	}

	header := tar.Header{
		Name:     archiveName,
		Typeflag: tar.TypeReg,
		Mode:     entry.mode,
		Size:     size,
		Uid:      0,
		Gid:      0,
		Uname:    "root",
		Gname:    "root",
		ModTime:  modTime,
	}
	if err := writer.WriteHeader(&header); err != nil {
		return fmt.Errorf("write header %s: %w", archiveName, err)
	}
	if _, err := io.Copy(writer, reader); err != nil {
		return fmt.Errorf("write content %s: %w", archiveName, err)
	}
	return nil
}

func containsString(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}
