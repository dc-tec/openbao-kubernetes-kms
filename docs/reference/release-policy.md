---
title: "Release Policy"
description: "Release channels, versioning, artifact families, and verification materials for bao-kms-provider."
weight: 100
---

# Release Policy

This policy defines when `bao-kms-provider` publishes releases, what each
channel means, and which artifacts users must verify before deployment.

## Public Release Status

Public releases use SemVer tags without a leading `v`, for example `0.1.0`.
Each public release publishes GitHub Release assets and a GHCR image, plus the
checksums, signatures, software bills of materials (SBOMs), and provenance
attestations needed to verify them.

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
| preview | tagged release for labs, staging, and evaluation | not production |
| stable | production-ready release line | covered by the stable support policy |

## Preview Release Rule

Preview releases ship only after the required test, packaging, signing, and
verification steps pass. See
[Development: CI And Supply Chain](/development/ci-supply-chain/) for the
maintainer-side workflow details.

A preview release is suitable for validating:

- KMS v2 protocol behavior,
- the tested OpenBao and Kubernetes matrix,
- JSON Web Token (JWT) auth in the default build,
- certificate auth backed by a PKCS#11 hardware or software token only when
  matching opt-in artifacts are published and marked as tested,
- rotation behavior,
- bootstrap and recovery runbooks covered by the release,
- artifact verification.

The SPIFFE/SPIRE certificate source is not part of the preview user-facing
configuration. It will be documented only after the required OpenBao cert-auth
behavior and release validation are in place.

## Stable Release Rule

A stable production-ready release requires, at minimum:

- a tested Kubernetes and OpenBao compatibility matrix,
- kubeadm VM systemd and static pod tests,
- OpenBao HA and failover tests,
- disaster recovery restore tests,
- startup decrypt storm tests,
- a security review,
- signed and attested artifacts,
- SBOMs,
- reproducibility reports,
- a release provenance index,
- reviewed compatibility and support documentation.

## Versioning

Patch releases are used for security, correctness, dependency, packaging, or
release-verification fixes that do not change the tested support scope.

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

The published checksum asset is `checksums.txt`. It uses SHA-256 and contains
one line per published release artifact.

Release artifacts are separated by supported auth path:

| Artifact family | Preview support |
|---|---|
| Default `bao-kms-provider_${VERSION}_linux_${GOARCH}` | JWT auth only. |
| `bao-kms-provider-certauth-pkcs11_${VERSION}_${GOOS}_${GOARCH}` | PKCS#11 certificate auth only when the release publishes this artifact and marks the PKCS#11 path as tested. |
| SPIFFE or combined cert-auth local verification artifacts | Not a supported preview user configuration. |

SPIFFE artifacts, when present, are for local verification and upstream OpenBao
alignment work until SPIFFE is listed as supported in the release notes.

## Verification Materials

Every public release publishes or retains enough material to verify the source,
image, binary, and package artifacts:

- the source commit,
- the release tag,
- the release workflow reference,
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
