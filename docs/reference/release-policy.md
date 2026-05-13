---
title: "Release Policy"
description: "Event-driven release policy, validation cadence, release channels, versioning, automation, and artifact evidence for bao-kms-provider."
weight: 100
---

# Release Policy

This page defines when `bao-kms-provider` publishes releases, what each channel means, and which evidence every public release carries.

## Public Release Status

Public releases are published from SemVer tags through the release workflow.
The workflow publishes GitHub Release assets and a GHCR image, then signs,
attests, verifies, and indexes the release evidence.

release-please opens release PRs and maintains the changelog. Release
publishing is implemented as a separate tag workflow and remains gated by the
release criteria.

The current public release line is a preview line unless the release notes and
[Support Policy](/reference/support-policy/) explicitly say otherwise.

## Cadence

The project does not use a fixed feature-release cadence. Public releases are
event-driven and are cut only when there is a validated reason to publish.

Release reasons:

- security fixes,
- correctness fixes,
- dependency or base-image fixes,
- packaging, signing, attestation, or provenance fixes,
- support-matrix expansion,
- completed release-validated capabilities,
- documentation changes that materially affect installation, upgrade, recovery,
  or security operation.

Validation can run on a schedule. Scheduled validation does not imply scheduled
publication.

## Channels

| Channel | Use | Support expectation |
|---|---|---|
| PR | validation only | no public artifacts |
| main | integration signal for merged code | no production support |
| nightly | scheduled drift detection | no production support |
| release candidate | pre-release soak for an intended tag | staging or evaluation only |
| preview | tagged release for controlled validation | not production |
| stable | production-ready release line | only after production-readiness gates pass |

## Preview Release Rule

Preview releases ship only after the release workflow completes the required
test, supply-chain, signing, attestation, and evidence steps. See
[Development: CI And Supply Chain](/development/ci-supply-chain/) for the
pipeline and evidence requirements.

A preview release is suitable for engineering evaluation and controlled validation of:

- KMS v2 protocol behavior,
- exact-pinned OpenBao CI e2e validation,
- exact-pinned Kubernetes Kind e2e validation,
- rotation behavior,
- bootstrap and recovery runbooks,
- CI and supply-chain evidence.

## Stable Release Rule

A stable production-ready release requires:

- an exact-pinned Kubernetes and OpenBao validation matrix,
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

Release artifacts use SemVer tags without a leading `v`, for example `0.1.0`.

Patch releases are used for security, correctness, dependency, packaging, or
release-evidence fixes that do not change the support envelope.

Minor releases are used for validated feature additions or support-matrix
expansion.

Major releases are reserved for breaking changes to configuration, `key_id`,
annotations, AAD canonicalization, operational support policy, or migration
behavior.

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

Publishing is a separate release workflow concern. Release workflows consume the release-please version, run validation, build the release artifacts, generate checksums, create SBOMs, sign, attest, verify byte reproducibility, and publish evidence.

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

Systemd hosts also receive native Linux packages and tarball fallbacks:

```text
bao-kms-provider_${VERSION}_linux_${GOARCH}.deb
bao-kms-provider_${VERSION}_linux_${GOARCH}.rpm
bao-kms-provider_${VERSION}_systemd_linux_${GOARCH}.tar.gz
```

Static-pod deployments receive a host-filesystem bundle:

```text
bao-kms-provider_${VERSION}_static-pod.tar.gz
```

Checksums are written to:

```text
dist/release/checksums.txt
```

The published release asset is `checksums.txt`. The checksum file uses SHA-256 and contains one line per published release artifact. `release-artifacts` builds the default JWT-capable Linux binary matrix, `release-packages` builds `.deb` and `.rpm` packages for systemd hosts, and `release-bundles` builds deterministic systemd and static-pod tarballs.

Certificate-auth artifacts are explicit opt-in builds. PKCS#11 cert-auth
variants are host CGO artifact builds through
`release-artifact-certauth-pkcs11-host`. A release must not claim PKCS#11
cert-auth source support unless the release evidence includes the relevant
artifact lane and the SoftHSM provider source E2E result. SPIFFE source wiring
remains in tree for local verification and upstream OpenBao alignment work, but
release artifacts must not claim `auth.cert.source: spiffe` support until the
supported OpenBao version can derive cert-auth identity aliases from URI SANs
and release evidence includes successful OpenBao cert-auth login with the
selected SPIFFE issuer profile.

## Release Evidence

Every public release publishes or retains:

- the source commit,
- the release tag,
- the workflow run reference,
- the OpenBao and Kubernetes version matrix,
- image and binary digests,
- systemd packages and static-pod bundle digests,
- `checksums.txt`,
- `checksums.txt.bundle`,
- SBOMs for published binaries and images,
- a vulnerability scan summary,
- image signature verification output,
- checksum signature verification output,
- GitHub provenance attestations,
- release artifact attestation verification output,
- a reproducibility report,
- `provenance-index.json`,
- release notes.

For the full supply-chain controls see [Development: CI And Supply Chain](/development/ci-supply-chain/).
