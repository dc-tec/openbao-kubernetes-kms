# Release Policy

This document defines the planned release-channel policy for the KMS provider.

## Current Status

The project is pre-implementation. No published release channel exists yet.

release-please is configured to open release PRs and maintain the changelog, but release publishing remains disabled until release gates and publishing workflows are implemented.

The first release must be described as a v0.1 engineering preview unless all production-readiness gates pass.

## Channels

| Channel | Use | Support expectation |
|---|---|---|
| PR | validation only | no public artifacts |
| edge | main-branch integration signal | not production |
| nightly | scheduled drift detection | not production |
| prerelease | release candidate soak | staging/evaluation only |
| stable | supported release line | only after release gates pass |

## Engineering Preview Rule

v0.1 can ship only after [Release gates](release-gates.md) are satisfied.

v0.1 must not claim production readiness. It should be suitable for engineering evaluation and lab validation of:

- KMS v2 protocol behavior,
- OpenBao `2.5.3` CI e2e validation,
- Kubernetes `1.34.3` Kind e2e for the initial release-line lane,
- rotation behavior,
- bootstrap and recovery runbooks,
- CI/supply-chain evidence.

## Stable Release Rule

A stable production-ready release requires:

- exact-pinned Kubernetes and OpenBao release-gate matrix,
- kubeadm VM systemd and static pod tests,
- OpenBao HA/failover tests,
- disaster recovery restore tests,
- startup decrypt storm tests,
- security review,
- signed and attested artifacts,
- SBOMs,
- byte reproducibility evidence,
- release provenance index,
- reviewed compatibility and support docs.

## Versioning

Use semantic versioning once release artifacts exist.

Before beta:

- breaking changes may occur between minor releases,
- release notes must call out migration impact,
- wire-format changes require ADR and migration docs.

After beta:

- key ID, annotation, and AAD compatibility should be treated as stable API surfaces.

## Release Automation

release-please is the source of truth for release PRs, version proposals, and `CHANGELOG.md`.

The release-please workflow is PR-only during M0:

- it opens or updates a release PR from Conventional Commits,
- it updates `.release-please-manifest.json`,
- it updates `CHANGELOG.md`,
- it supports manual `Release-As` overrides through `workflow_dispatch`,
- it does not create tags,
- it does not publish GitHub Releases,
- it does not build, sign, attest, or upload artifacts.

Publishing remains a separate release workflow concern. Future release workflows must consume the release-please version, run release gates, build the `dist/release` artifacts, generate checksums, create SBOMs, sign, attest, and publish evidence.

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

The initial Linux artifact matrix is:

| GOOS | GOARCH |
|---|---|
| linux | amd64 |
| linux | arm64 |

Checksums are written to:

```text
dist/release/checksums.txt
```

The checksum file uses SHA-256 and contains one line per binary artifact. The Makefile target `release-artifacts` builds the Linux matrix and then regenerates the checksum file.

## Release Evidence

Every release should retain:

- source commit,
- signed tag,
- workflow run,
- OpenBao/Kubernetes version matrix,
- image and binary digests,
- checksums,
- SBOMs,
- vulnerability scan summary,
- signatures,
- attestations,
- reproducibility report,
- provenance index,
- release notes.
