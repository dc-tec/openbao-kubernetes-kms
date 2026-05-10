---
title: "CI And Supply Chain"
description: "Version pinning policy, local CI parity, release automation, CI lanes, supply-chain gates, build-once promotion, and release evidence for bao-kms-provider."
weight: 50
---

# CI And Supply Chain

This page defines the CI, release, and supply-chain posture for the project.

The design follows a pattern adopted from the OpenBao Operator project where it applies:

- local parity through `make` targets,
- central version policy,
- no floating `latest` versions in CI or release gates,
- change-routed test expansion,
- vendored and deterministic Go builds,
- pinned GitHub Actions and tool versions,
- pinned Ginkgo and Gomega E2E framework versions,
- a validated `test/e2e/suites.yaml` lane manifest,
- license and vulnerability gates,
- SBOM generation,
- provenance and signature evidence,
- release reproducibility checks before promotion.

The repository commits the Go `vendor/` tree. CI and release jobs run with
`GOFLAGS=-mod=vendor` except where a target intentionally refreshes or verifies
module metadata.

## Version Pinning Policy

CI does not use floating `latest` inputs for support claims.

Version policy lives in `.ci/versions.yaml`. CI and release workflows read support-matrix inputs from that file rather than duplicating version strings in workflow definitions:

```yaml
version: 1
project:
  name: openbao-kubernetes-kms
  module: github.com/dc-tec/openbao-kubernetes-kms
  binary: bao-kms-provider
toolchain:
  go: "1.26.3"
  commandFramework:
    cliConfig: viper
  qualityTools:
    astGrep: "0.42.1"
    gofumpt: "0.9.2"
    staticcheck: "0.7.0"
    govulncheck: "1.2.0"
    semgrep: "1.157.0"
    golangciLint: "2.11.4"
    pinStatus: pinned
  githubActions:
    checkout: de0fac2e4500dabe0009e67214ff5f5447ce83dd
    createGithubAppToken: 1b10c78c7865c340bc4f6099eb2f838309f1e8c3
    releasePlease: 45996ed1f6d02564a971a2fa1b5860e934307cf7
    setupGo: 4b73464bb391d4059bd26b0524d20df3927bd417
    setupNode: 48b55a011bda9f5d6aeb4c2d9c7362e8dae4041e
  goDependencies:
    ginkgo: "2.28.3"
    gomega: "1.40.0"
  testFrameworks:
    e2e: ginkgo
    assertions: gomega
    suiteManifest: test/e2e/suites.yaml
    reportFormats:
      - ginkgo-json
      - junit
validation:
  openbao:
    primary: "2.5.3"
    image: "ghcr.io/openbao/openbao:2.5.3"
    imageDigest: "sha256:fdc6da21ca6963560c32336fd7feb9cf2d5e52668f1a1647205a4b41171f0806"
    digestStatus: pinned
  kubernetes:
    minimumLine: "1.34"
    primaryLine: "1.34"
    upstreamPatch: "1.34.7"
    exactVersion: "1.34.3"
    kindNodeImage: "kindest/node:v1.34.3@sha256:08497ee19eace7b4b5348db5c6a1591d7752b164530a36f855cb0f2bdcbadd48"
    kindNodeImageDigest: "sha256:08497ee19eace7b4b5348db5c6a1591d7752b164530a36f855cb0f2bdcbadd48"
    pinStatus: pinned-kind-node
    futureCandidates:
      - "1.35"
      - "1.36"
```

Kubernetes upstream currently lists `1.34.7` as the latest `1.34` patch. The Kind e2e lane pins `kindest/node:v1.34.3` by digest because that is the newest official Kind node image available for the `1.34` line. `1.34.7` is tracked but not validated until an exact-pinned runnable Kubernetes lane exists for that patch.

## Initial Validation Matrix

| Component | Version posture |
|---|---|
| OpenBao | `2.5.3` only. |
| Kubernetes | `1.34.3` in Kind for the initial e2e lane; `1.34.7` tracked as latest upstream patch, not validated. |
| KMS API | Kubernetes KMS v2 only. |
| OS | Linux control-plane nodes only. |

For the support claim and breaking-change rules see [Reference: Compatibility](/reference/compatibility/).

## Local Parity

Expected local entry points:

```sh
make bootstrap
make doctor
make ci-core
```

`make ci-core` covers:

- `gofmt` and `gofumpt` formatting,
- `go vet`,
- `staticcheck`,
- `govulncheck`,
- `golangci-lint` using `.golangci.yml`,
- ast-grep structural rule tests and scan using `.ast-grep/sgconfig.yml`,
- Semgrep security and API-misuse rule tests and scan using `.semgrep/rules`,
- unit tests,
- race smoke tests,
- ast-grep forbidden dynamic-Go-type rules,
- generated artifact checks,
- E2E suite manifest validation,
- vendored dependency check,
- license check,
- static security scan,
- fuzz smoke,
- KMS v2 fake conformance,
- key ID and AAD golden tests,
- configuration validation tests,
- redaction tests.

## Release PR Automation

release-please owns release PR creation, version proposals, and `CHANGELOG.md` updates. It is intentionally separated from publishing:

- release-please updates `.release-please-manifest.json` and `CHANGELOG.md`,
- release-please runs from a pinned action SHA,
- release-please uses a GitHub App token when configured,
- release-please does not create GitHub Releases,
- artifact builds, checksums, SBOMs, signatures, attestations, and provenance remain release-workflow responsibilities.

Required repository secrets for active release PR automation:

```text
OPENBAO_KMS_RELEASE_PR_APP_ID
OPENBAO_KMS_RELEASE_PR_PRIVATE_KEY
```

## CI Lanes

### Pull Requests

Required for every PR:

- core quality gate,
- strict typed-Go quality gate,
- ast-grep architecture and runtime-safety scan,
- Semgrep security and API-misuse scan,
- KMS v2 fake conformance,
- key ID and AAD golden tests,
- redaction tests,
- configuration validation tests,
- vendored dependency verification,
- license check,
- vulnerability and static security scan.

Change-routed expansions:

| Changed area | Additional validation |
|---|---|
| `internal/kmsv2`, `internal/keyregistry`, `internal/aad` | conformance, fuzz smoke, golden fixtures. |
| `internal/openbao`, `internal/auth` | hermetic OpenBao client integration lane. |
| `internal/socket`, deployment samples | systemd and static-pod smoke checks. |
| rotation or status code | rotation and failure-injection lanes. |
| packaging or Dockerfile | image scan, SBOM smoke, reproducibility smoke. |
| docs only | documentation link and build checks. |

The Dockerfile pins both the Go builder image and the distroless non-root runtime image by digest. The pinned values are recorded in `.ci/versions.yaml`; release workflows update both places together.

### Main Branch

Main runs all PR lanes plus:

- pinned OpenBao `2.5.3` CI e2e tests,
- pinned Kubernetes `1.34.3` Kind e2e,
- rotation tests,
- decrypt storm smoke,
- image scan,
- SBOM generation.

E2E lane selection comes from `test/e2e/suites.yaml`; concrete OpenBao and Kubernetes pins come from `.ci/versions.yaml`.

The OpenBao PR E2E lane uses the ephemeral CI target:

```sh
make test-e2e-openbao-ci
```

### Nightly

Nightly runs:

- the full pinned `1.34.3` Kind e2e,
- OpenBao `2.5.3` CI e2e tests,
- failure injection,
- long-running Status polling,
- decrypt storm benchmark,
- supply-chain checks,
- optional kubeadm VM smoke when infrastructure is available.

### Release Gate

The release gate runs:

- all nightly lanes,
- kubeadm VM systemd test,
- kubeadm VM static-pod test,
- OpenBao restore test,
- paired etcd and OpenBao restore test,
- upgrade and rollback test,
- image signing and provenance checks,
- byte reproducibility check,
- release evidence generation.

For the full release-gate criteria see [Release Gates](/development/release-gates/).

## Supply-Chain Gates

Required before publishing any release artifact:

- pinned GitHub Actions by commit SHA,
- vendored Go dependencies or an equivalent deterministic module policy,
- dependency review,
- license allowlist for shipped dependencies,
- static security scan,
- `govulncheck`,
- filesystem and image vulnerability scans,
- SBOM for each published binary or image,
- checksums for release assets,
- signed images and release checksums,
- provenance attestations,
- verification of attestations against the expected workflow identity,
- byte reproducibility check for release images and SBOMs,
- a release provenance index.

## Build Once, Promote By Digest

Release workflows build immutable artifacts once, capture digests, verify trust evidence, and promote by digest.

The pipeline does not rebuild a different subject during publication.

```text
tag or release ref
  -> build immutable image and binary artifacts
  -> capture digests
  -> verify provenance and reproducibility
  -> sign and attest
  -> publish release assets and provenance index
```

## Release Channels

| Channel | Use | Support expectation |
|---|---|---|
| PR | validation only | no public artifacts |
| edge | main-branch integration signal | not production |
| nightly | scheduled drift detection | not production |
| prerelease | release candidate soak | staging or evaluation |
| stable | supported release line | only after release gates pass |

For the channel definitions and the engineering-preview rule see [Reference: Release Policy](/reference/release-policy/).

## Release Evidence

Every stable release publishes or retains:

- the workflow run URL,
- the source commit,
- a signed tag,
- image digests,
- binary, package, and bundle checksums,
- SBOMs,
- a vulnerability scan result summary,
- provenance attestations,
- signature verification output,
- a reproducibility report,
- release notes,
- the compatibility matrix used for the release.

The release workflow publishes the KMS provider image by digest, the Linux
binary matrix, native `.deb` and `.rpm` packages for systemd hosts,
deterministic systemd and static-pod tarball bundles, checksums, image and
binary SPDX SBOMs, a checksum signature bundle, GitHub provenance
attestations, a byte-reproducibility report, and a provenance index.

## Backlog Implications

The CI and supply-chain work is not a final polish task. It affects repository layout, Makefile targets, dependency policy, release artifact names, and version-matrix ownership from the beginning. Treat changes to `.ci/versions.yaml` and the release workflows as wire-format-equivalent: review them with the same discipline as `key_id` derivation or AAD canonicalization changes.
