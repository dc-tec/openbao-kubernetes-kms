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

release-please opens release PRs and maintains the changelog. A separate
release-tag workflow creates the signed tag and draft GitHub Release. The
tag-triggered release workflow then builds, validates, signs, uploads, and
publishes the release evidence.

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
- JWT auth in the default build,
- PKCS#11 certificate auth only when matching opt-in artifacts and E2E evidence
  are present,
- rotation behavior,
- bootstrap and recovery runbooks covered by the release lanes,
- CI and supply-chain evidence.

SPIFFE/SPIRE source wiring may appear in implementation evidence, but preview
release artifacts and docs must not present `auth.cert.source: spiffe` as a
supported user configuration until the supported OpenBao version can derive
cert-auth identity aliases from URI SANs and the release evidence covers that
path.

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

Release tag creation is a separate workflow concern. After the release PR is
merged, the release-tag workflow validates the merged release-please PR, creates
a signed annotated tag at that merge commit, and creates or refreshes a draft
GitHub Release with the release notes.

The tag-triggered release workflow consumes that draft release, runs validation,
builds the release artifacts, generates checksums, creates SBOMs, signs,
attests, verifies byte reproducibility, uploads assets to the draft release, and
publishes the release evidence through the maintainer-controlled
`release-publish` GitHub Environment. It fails early if the draft GitHub Release
is missing or already published.

Release credentials, signing keys, and tag-ruleset bypass are maintainer
configuration, not user-facing deployment inputs.

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

The published release asset is `checksums.txt`. The checksum file uses SHA-256 and contains one line per published release artifact. `release-artifacts` builds the default JWT-only Linux binary matrix, `release-packages` builds `.deb` and `.rpm` packages for systemd hosts, and `release-bundles` builds deterministic systemd and static-pod tarballs.

Release evidence must distinguish artifact families:

| Artifact family | Build target | Public preview claim |
|---|---|---|
| Default `bao-kms-provider_${VERSION}_linux_${GOARCH}` | `release-artifacts` | JWT auth only. |
| `bao-kms-provider-certauth-pkcs11_${VERSION}_${GOOS}_${GOARCH}` | `release-artifact-certauth-pkcs11-host` | PKCS#11 certificate auth only when the release evidence includes this artifact lane and the SoftHSM provider source E2E result. |
| SPIFFE or combined cert-auth validation artifacts | `release-artifacts-certauth-spiffe`, `release-artifact-certauth-combined-host` | No public preview support claim. These artifacts are for local verification and upstream OpenBao alignment work. |

Release artifacts and docs must not claim `auth.cert.source: spiffe` support
until the supported OpenBao version can derive cert-auth identity aliases from
URI SANs and release evidence includes successful OpenBao cert-auth login with
the selected SPIFFE issuer profile.

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
