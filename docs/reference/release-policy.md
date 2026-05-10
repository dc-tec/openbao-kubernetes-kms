---
title: "Release Policy"
description: "Release channels, engineering preview rule, stable release rule, versioning, automation, and binary artifact policy for bao-kms-provider."
weight: 100
---

# Release Policy

This page defines the release-channel policy for `bao-kms-provider`.

## Current Status

The project is pre-implementation. No published release channel exists yet.

release-please is configured to open release PRs and maintain the changelog. Release publishing is disabled until release gates and publishing workflows are implemented.

The first release ships as a v0.1 engineering preview unless every production-readiness gate passes.

## Channels

| Channel | Use | Support expectation |
|---|---|---|
| PR | validation only | no public artifacts |
| edge | main-branch integration signal | not production |
| nightly | scheduled drift detection | not production |
| prerelease | release candidate soak | staging or evaluation only |
| stable | supported release line | only after release gates pass |

## Engineering Preview Rule

v0.1 ships only after [Development: Release Gates](/development/release-gates/) are satisfied.

v0.1 must not claim production readiness. It is suitable for engineering evaluation and lab validation of:

- KMS v2 protocol behavior,
- OpenBao `2.5.3` CI e2e validation,
- Kubernetes `1.34.3` Kind e2e for the initial release-line lane,
- rotation behavior,
- bootstrap and recovery runbooks,
- CI and supply-chain evidence.

## Stable Release Rule

A stable production-ready release requires:

- an exact-pinned Kubernetes and OpenBao release-gate matrix,
- kubeadm VM systemd and static pod tests,
- OpenBao HA and failover tests,
- disaster recovery restore tests,
- startup decrypt storm tests,
- a security review,
- signed and attested artifacts,
- SBOMs,
- byte reproducibility evidence,
- a release provenance index,
- reviewed compatibility and support documentation.

## Versioning

Semantic versioning applies once release artifacts exist.

Before beta:

- breaking changes may occur between minor releases,
- release notes call out migration impact,
- wire-format changes require a documented migration.

After beta:

- `key_id`, annotation, and AAD compatibility are treated as stable API surfaces. See [Reference: Compatibility: Compatibility Promises](/reference/compatibility/#compatibility-promises).

## Release Automation

release-please is the source of truth for release PRs, version proposals, and `CHANGELOG.md`.

The release-please workflow is PR-only:

- it opens or updates a release PR from Conventional Commits,
- it updates `.release-please-manifest.json`,
- it updates `CHANGELOG.md`,
- it supports manual `Release-As` overrides through `workflow_dispatch`,
- it does not create tags,
- it does not publish GitHub Releases,
- it does not build, sign, attest, or upload artifacts.

Publishing is a separate release workflow concern. Release workflows consume the release-please version, run release gates, build the `dist/release` artifacts, generate checksums, create SBOMs, sign, attest, and publish evidence.

The workflow expects a GitHub App token so release PR updates can trigger normal PR checks. Configure these repository secrets:

```text
OPENBAO_KMS_RELEASE_PR_APP_ID
OPENBAO_KMS_RELEASE_PR_PRIVATE_KEY
```

If the secrets are absent, the workflow exits as a no-op with a notice.

## Binary Artifacts

Release binaries use this naming pattern:

```text
bao-kms-provider_${VERSION}_${GOOS}_${GOARCH}
```

Initial Linux artifact matrix:

| GOOS | GOARCH |
|---|---|
| linux | amd64 |
| linux | arm64 |

Checksums are written to:

```text
dist/release/checksums.txt
```

The checksum file uses SHA-256 and contains one line per binary artifact. The Makefile target `release-artifacts` builds the Linux matrix and regenerates the checksum file.

## Release Evidence

Every stable release retains:

- the source commit,
- a signed tag,
- the workflow run reference,
- the OpenBao and Kubernetes version matrix,
- image and binary digests,
- checksums,
- SBOMs,
- a vulnerability scan summary,
- signatures,
- attestations,
- a reproducibility report,
- a provenance index,
- release notes.

For the full supply-chain controls see [Development: CI And Supply Chain](/development/ci-supply-chain/).
