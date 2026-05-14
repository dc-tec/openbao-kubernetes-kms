package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type args struct {
	indexPath          string
	repo               string
	owner              string
	version            string
	sourceDateEpoch    int64
	binaryName         string
	image              string
	imageDigest        string
	imageRebuildDigest string
	releaseSourceRef   string
	releaseWorkflow    string
	checksumsPath      string
	checksumsBundle    string
	sbomGlob           string
	attestations       attestationsArgs
}

type attestationsArgs struct {
	available         bool
	unavailableReason string
}

type index struct {
	SchemaVersion int             `json:"schema_version"`
	Project       projectInfo     `json:"project"`
	Release       releaseInfo     `json:"release"`
	Image         imageInfo       `json:"image"`
	Checksums     checksumsInfo   `json:"checksums"`
	Assets        []assetInfo     `json:"assets"`
	SBOMs         []sbomInfo      `json:"sboms"`
	Attestations  attestationInfo `json:"attestations"`
	Reproducible  reproducibility `json:"reproducibility"`
}

type projectInfo struct {
	Repository string `json:"repository"`
	Owner      string `json:"owner"`
	Binary     string `json:"binary"`
}

type releaseInfo struct {
	Version            string `json:"version"`
	SourceRef          string `json:"source_ref"`
	SourceDateEpoch    int64  `json:"source_date_epoch"`
	GeneratedAtUTC     string `json:"generated_at_utc"`
	ReleaseWorkflow    string `json:"release_workflow"`
	ExpectedOIDCIssuer string `json:"expected_oidc_issuer"`
}

type imageInfo struct {
	Ref           string `json:"ref"`
	Digest        string `json:"digest"`
	RebuildDigest string `json:"rebuild_digest,omitempty"`
}

type checksumsInfo struct {
	Path                  string `json:"path"`
	Digest                string `json:"digest"`
	SignatureBundlePath   string `json:"signature_bundle_path"`
	SignatureBundleDigest string `json:"signature_bundle_digest,omitempty"`
}

type assetInfo struct {
	Name           string `json:"name"`
	Path           string `json:"path"`
	SHA256         string `json:"sha256"`
	SizeBytes      int64  `json:"size_bytes"`
	RecordedSHA256 string `json:"recorded_sha256"`
	IncludedInSums bool   `json:"included_in_checksums_txt"`
}

type sbomInfo struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Digest    string `json:"digest"`
	SizeBytes int64  `json:"size_bytes"`
}

type attestationInfo struct {
	Available               bool   `json:"available"`
	UnavailableReason       string `json:"unavailable_reason,omitempty"`
	ImageAttestationSubject string `json:"image_attestation_subject,omitempty"`
	AssetSignerWorkflow     string `json:"asset_signer_workflow,omitempty"`
}

type reproducibility struct {
	ImageDigestMatch bool `json:"image_digest_match"`
}

func main() {
	cfg, err := parseArgs()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(2)
	}

	idx, err := buildIndex(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// #nosec G301 -- release evidence is intentionally published as a world-readable artifact.
	if err := os.MkdirAll(filepath.Dir(cfg.indexPath), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "error: create index directory: %v\n", err)
		os.Exit(1)
	}

	out, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: marshal index: %v\n", err)
		os.Exit(1)
	}
	out = append(out, '\n')

	// #nosec G306 -- release evidence is intentionally published as a world-readable artifact.
	if err := os.WriteFile(cfg.indexPath, out, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "error: write index: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Wrote %s\n", cfg.indexPath)
}

func parseArgs() (args, error) {
	cfg := args{}

	flag.StringVar(&cfg.indexPath, "index-path", "dist/provenance-index.json", "output index path")
	flag.StringVar(&cfg.repo, "repo", "", "repository in owner/repo form")
	flag.StringVar(&cfg.owner, "owner", "", "repository owner")
	flag.StringVar(&cfg.version, "version", "", "release version")
	flag.Int64Var(&cfg.sourceDateEpoch, "source-date-epoch", 0, "unix timestamp for deterministic generated_at_utc")
	flag.StringVar(&cfg.binaryName, "binary-name", "bao-kms-provider", "binary name")
	flag.StringVar(&cfg.image, "image", "", "image repository")
	flag.StringVar(&cfg.imageDigest, "image-digest", "", "image digest")
	flag.StringVar(&cfg.imageRebuildDigest, "image-rebuild-digest", "", "independent rebuild image digest")
	flag.StringVar(&cfg.releaseSourceRef, "release-source-ref", "", "release source ref")
	flag.StringVar(&cfg.releaseWorkflow, "release-workflow", "", "release workflow identity")
	flag.StringVar(&cfg.checksumsPath, "checksums-path", "dist/checksums.txt", "checksums path")
	flag.StringVar(
		&cfg.checksumsBundle,
		"checksums-bundle-path",
		"dist/checksums.txt.bundle",
		"checksums signature bundle path",
	)
	flag.StringVar(&cfg.sbomGlob, "sbom-glob", "dist/sbom-*.spdx.json", "SBOM glob")
	flag.BoolVar(
		&cfg.attestations.available,
		"attestations-available",
		true,
		"whether GitHub provenance attestations are available for this release",
	)
	flag.StringVar(
		&cfg.attestations.unavailableReason,
		"attestations-unavailable-reason",
		"",
		"reason GitHub provenance attestations are unavailable",
	)
	flag.Parse()

	required := map[string]string{
		"-repo":         cfg.repo,
		"-owner":        cfg.owner,
		"-version":      cfg.version,
		"-binary-name":  cfg.binaryName,
		"-image":        cfg.image,
		"-image-digest": cfg.imageDigest,
	}
	for name, value := range required {
		if value == "" {
			return cfg, fmt.Errorf("%s is required", name)
		}
	}
	if cfg.releaseSourceRef == "" {
		cfg.releaseSourceRef = "refs/tags/" + cfg.version
	}
	if cfg.releaseWorkflow == "" {
		cfg.releaseWorkflow = cfg.repo + "/.github/workflows/release.yml"
	}
	return cfg, nil
}

func buildIndex(cfg args) (index, error) {
	checksumSubjects, err := parseChecksumsFile(cfg.checksumsPath)
	if err != nil {
		return index{}, fmt.Errorf("parse checksums file: %w", err)
	}

	assets, err := buildAssets(filepath.Dir(cfg.checksumsPath), checksumSubjects)
	if err != nil {
		return index{}, err
	}
	sboms, err := buildSBOMs(cfg.sbomGlob)
	if err != nil {
		return index{}, err
	}

	checksumsDigest, err := maybeSHA256WithPrefix(cfg.checksumsPath)
	if err != nil {
		return index{}, fmt.Errorf("hash checksums path: %w", err)
	}
	bundleDigest, err := maybeSHA256WithPrefix(cfg.checksumsBundle)
	if err != nil {
		return index{}, fmt.Errorf("hash checksums bundle: %w", err)
	}

	attestations := attestationInfo{Available: cfg.attestations.available}
	if cfg.attestations.available {
		attestations.ImageAttestationSubject = "oci://" + cfg.image + "@" + cfg.imageDigest
		attestations.AssetSignerWorkflow = cfg.releaseWorkflow
	} else {
		attestations.UnavailableReason = cfg.attestations.unavailableReason
	}

	generatedAt := time.Unix(cfg.sourceDateEpoch, 0).UTC().Format(time.RFC3339)
	return index{
		SchemaVersion: 1,
		Project: projectInfo{
			Repository: cfg.repo,
			Owner:      cfg.owner,
			Binary:     cfg.binaryName,
		},
		Release: releaseInfo{
			Version:            cfg.version,
			SourceRef:          cfg.releaseSourceRef,
			SourceDateEpoch:    cfg.sourceDateEpoch,
			GeneratedAtUTC:     generatedAt,
			ReleaseWorkflow:    cfg.releaseWorkflow,
			ExpectedOIDCIssuer: "https://token.actions.githubusercontent.com",
		},
		Image: imageInfo{
			Ref:           cfg.image,
			Digest:        cfg.imageDigest,
			RebuildDigest: cfg.imageRebuildDigest,
		},
		Checksums: checksumsInfo{
			Path:                  cfg.checksumsPath,
			Digest:                checksumsDigest,
			SignatureBundlePath:   cfg.checksumsBundle,
			SignatureBundleDigest: bundleDigest,
		},
		Assets:       assets,
		SBOMs:        sboms,
		Attestations: attestations,
		Reproducible: reproducibility{
			ImageDigestMatch: cfg.imageRebuildDigest != "" && cfg.imageDigest == cfg.imageRebuildDigest,
		},
	}, nil
}

func parseChecksumsFile(path string) (map[string]string, error) {
	// #nosec G304 -- release tooling reads the workflow-provided checksums path.
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = file.Close()
	}()

	out := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		out[fields[1]] = fields[0]
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, errors.New("checksums file did not contain any subjects")
	}
	return out, nil
}

func buildAssets(dir string, checksumSubjects map[string]string) ([]assetInfo, error) {
	names := make([]string, 0, len(checksumSubjects))
	for name := range checksumSubjects {
		names = append(names, name)
	}
	sort.Strings(names)

	assets := make([]assetInfo, 0, len(names))
	for _, name := range names {
		path := filepath.Join(dir, name)
		sha, size, err := fileSHA256(path)
		if err != nil {
			return nil, fmt.Errorf("hash asset %s: %w", path, err)
		}
		assets = append(assets, assetInfo{
			Name:           name,
			Path:           path,
			SHA256:         sha,
			SizeBytes:      size,
			RecordedSHA256: checksumSubjects[name],
			IncludedInSums: true,
		})
	}
	return assets, nil
}

func buildSBOMs(pattern string) ([]sbomInfo, error) {
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)

	sboms := make([]sbomInfo, 0, len(paths))
	for _, path := range paths {
		sha, size, err := fileSHA256(path)
		if err != nil {
			return nil, fmt.Errorf("hash sbom %s: %w", path, err)
		}
		sboms = append(sboms, sbomInfo{
			Name:      filepath.Base(path),
			Path:      path,
			Digest:    "sha256:" + sha,
			SizeBytes: size,
		})
	}
	return sboms, nil
}

func maybeSHA256WithPrefix(path string) (string, error) {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	sha, _, err := fileSHA256(path)
	if err != nil {
		return "", err
	}
	return "sha256:" + sha, nil
}

func fileSHA256(path string) (string, int64, error) {
	// #nosec G304 -- release tooling hashes workflow-provided release artifact paths.
	data, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), int64(len(data)), nil
}
