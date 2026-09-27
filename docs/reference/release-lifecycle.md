---
title: Release and support lifecycle
description: "Current release maturity, what a preview covers, release channels, versioning, security fixes, the stable release bar, and artifact names."
eyebrow: Reference · Lifecycle
weight: 80
verifiedBy:
  - .github/workflows/release.yml
  - mk/build.mk
  - hack/tools/release_bundle/main.go
---

## Current status

The current release line is a preview. Use it for labs, staging, and
evaluation, and do not protect production control planes with it. Support is
best effort, with no long-term support window and no service-level objective.

A preview release covers what its release notes and
[Compatibility](/docs/reference/compatibility/) list as tested: KMS v2 against
the tested Kubernetes and OpenBao versions, Transit `aes256-gcm96`, JWT auth,
the systemd and static-pod samples, rotation, the recovery runbooks the release
covers, and verifiable release artifacts. It does not cover production
readiness, unlisted versions or OpenBao HA topologies, or performance targets.

If you run a preview, pin exact provider, OpenBao, and Kubernetes versions,
keep etcd and OpenBao backups paired, validate upgrades in staging, and never
run `main`, nightly, or release candidate builds in production.

## Channels

| Channel | Purpose | Use |
|---|---|---|
| Pull request | Validation only | No public artifacts |
| `main` | Integration signal for merged code | Not for production |
| Nightly | Scheduled drift detection | Not for production |
| Release candidate | Soak before an intended tag | Staging or evaluation |
| Preview | Tagged release | Labs, staging, and evaluation |
| Stable | Production-ready release line | Production, under the stable support policy |

Releases are event-driven, not scheduled: security or correctness fixes,
dependency, packaging, or provenance fixes, a wider tested matrix, completed
capabilities, or documentation that changes installation, upgrade, recovery, or
security operation. Scheduled validation does not imply a scheduled release.

## Versioning

Tags follow SemVer without a leading `v`, for example `0.1.0`.

- Patch releases fix security, correctness, dependency, packaging, or release
  verification issues without changing the tested scope.
- Minor releases add validated features or widen the tested matrix.
- Major releases break configuration, `key_id`, annotations, AAD
  canonicalization, support policy, or migration behavior.

Before beta, preview releases can break those surfaces; the release notes then
describe the impact and whether migration or a fresh installation is required. From beta
on, `key_id`, annotations, and AAD follow the
[compatibility promises](/docs/reference/compatibility/#compatibility-promises).

## Security fixes

Until a stable line exists, security fixes land in the latest preview line
only. The stable line's fix and backport policy will be published with the
first stable release. Report vulnerabilities as described in the repository's
`SECURITY.md`.

## Stable release bar

A stable release needs at least:

- a tested Kubernetes and OpenBao matrix, kubeadm VM tests for systemd and
  static pods, OpenBao HA and failover tests, restore tests, and startup
  decrypt storm tests,
- a security review of `key_id` derivation, AAD canonicalization, the OpenBao
  policy, socket handling, auth material handling, and log and metric
  redaction, plus failure validation for key deletion, recreated keys, and a
  premature `min_decryption_version`,
- signed and attested artifacts with SBOMs, reproducibility reports, and a
  provenance index,
- reviewed compatibility and support documentation.

## Artifacts

Each release publishes GitHub release assets and a digest-addressed image on
GHCR for `linux/amd64` and `linux/arm64`:

```text
bao-kms-provider_${VERSION}_linux_${GOARCH}                  binary
bao-kms-provider_${VERSION}_linux_${GOARCH}.deb              systemd package
bao-kms-provider_${VERSION}_linux_${GOARCH}.rpm              systemd package
bao-kms-provider_${VERSION}_systemd_linux_${GOARCH}.tar.gz   systemd tarball
bao-kms-provider_${VERSION}_static-pod_linux_${GOARCH}.tar.gz static-pod kit
bao-kms-provider-certauth-pkcs11_${VERSION}_${GOOS}_${GOARCH} PKCS#11 host build, when published
checksums.txt                                                SHA-256 of every artifact
```

The release also publishes the signature bundle for `checksums.txt`, provenance
attestations, SBOMs, a vulnerability scan summary, a reproducibility report,
and `provenance-index.json`. See
[Security: Verify release artifacts](/docs/security/verify-release-artifacts/).
