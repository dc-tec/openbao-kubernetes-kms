---
title: CI and supply chain
description: "How versions are pinned, what runs on pull requests, main, and nightly, and how the release workflows build, verify, sign, and publish artifacts."
eyebrow: Contribute
weight: 50
verifiedBy:
  - .ci/versions.yaml
  - .github/workflows/ci.yml
  - .github/workflows/release.yml
  - .github/workflows/release-tag.yml
  - .github/workflows/release-please.yml
---

The repository commits `vendor/`, and CI and release jobs run with
`GOFLAGS=-mod=vendor` except where a target refreshes module metadata on
purpose.

## Version pinning

`.ci/versions.yaml` is the single source for toolchain, action, dependency,
container, OpenBao, Kubernetes, and artifact versions. Nothing floats:
GitHub Actions are pinned by commit SHA, the Go toolchain is pinned, Go
dependencies are vendored, and the OpenBao image, Kind node images, and the
container builder and runtime base images are pinned by digest. A version
becomes part of the tested matrix only with exact pins and release evidence.

The Trivy installer downloads the pinned archive, falls back to the GitHub
release-asset API, retries a bounded number of times, and checks the archive
against `toolchain.qualityTools.trivyLinuxAmd64SHA256`. Update that checksum
together with the Trivy version.

## Local checks

devenv loads the pinned toolchain and calls the canonical make targets:

```sh
devenv tasks run kms:bootstrap
devenv tasks run kms:ci-core
```

`make ci-core` covers formatting, vetting, static analysis, vulnerability
checks, Semgrep and ast-grep rules, unit tests, race and fuzz smoke, generated
artifacts, vendor verification, license checks, KMS v2 fake conformance, key
ID and AAD golden tests, configuration validation, redaction tests, and E2E
manifest validation. The advisory `Devenv Contract` job checks the devenv shell
when toolchain files change; make targets stay authoritative. Run the relevant
[E2E lanes](/contribute/e2e-framework/) when you touch runtime, OpenBao,
Kubernetes, or deployment behavior.

## CI lanes

Every pull request runs the `ci-core` gates. Changed areas add more:

| Changed area | Added validation |
|---|---|
| `internal/kmsv2`, `internal/keyregistry`, `internal/aad` | Conformance, fuzz smoke, golden fixtures |
| `internal/openbao`, `internal/auth` | Hermetic OpenBao integration and the OpenBao E2E lane |
| `internal/socket`, deployment samples, install guides | systemd and static-pod checks, including `make systemd-install-check` |
| Status or rotation code | Rotation and failure-injection lanes |
| Packaging or Dockerfile | Image scan, SBOM smoke, reproducibility smoke |
| Docs only | `make docs-check` and `make docs-build` |

Main and nightly add the slower lanes: OpenBao, Kind smoke, convergence,
upgrade, and DR, failure injection, HA failover, rotation, provider upgrade,
load and decrypt soaks, image scan, and SBOM generation. CI builds the provider
and PKCS#11 validation images once and has the scan and E2E jobs load those
exact archives with `E2E_PROVIDER_BUILD=false`. kubeadm VM validation stays out
of public CI because it restarts VMs and API servers and stops OpenBao on
purpose.

## Releases

Three workflows split the release:

| Workflow | Does | Does not |
|---|---|---|
| release-please | Opens the release PR from Conventional Commits, updates `.release-please-manifest.json` and `CHANGELOG.md`, honors `Release-As` | Tag, publish, or build |
| release-tag | Validates the merged release PR, creates a signed annotated SemVer tag, and creates a draft GitHub Release | Build or upload assets |
| release | Builds, tests, signs, attests, and publishes from the tag | Rebuild a different subject at publication |

The release workflow builds every subject once and promotes it by digest:

```text
signed tag and draft release
  -> build image and release assets
  -> rebuild the image independently
  -> run the OpenBao and Kind preview release gates
  -> capture digests, checksums, and SBOMs
  -> verify byte reproducibility
  -> sign the image digest and checksums.txt, create provenance attestations
  -> verify signatures and attestations against the release workflow identity
  -> upload assets and provenance-index.json to the draft release
  -> publish through the release-publish environment
```

Before anything is published, the release also requires dependency review, the
license allowlist, static security scanning, `govulncheck`, and filesystem and
image vulnerability scans. The release gates pull the digest the build produced
and test exactly that image.

Signing uses keyless cosign with the workflow's OIDC identity. Release
credentials and tag-ruleset bypass are maintainer settings. Dry runs in private
user repositories cannot store GitHub attestations, so they record
`attestations.available: false` in `provenance-index.json` while still checking
everything else; public tags always run with attestations.

For what the release publishes and how users verify it, see
[Reference: Release and support lifecycle](/docs/reference/release-lifecycle/#artifacts)
and [Security: Verify release artifacts](/docs/security/verify-release-artifacts/).
