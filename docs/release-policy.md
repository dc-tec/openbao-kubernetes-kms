# Release Policy

This document defines the planned release-channel policy for the KMS provider.

## Current Status

The project is pre-implementation. No release channel exists yet.

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
- OpenBao `2.5.3` integration,
- Kubernetes `1.34` release-line e2e,
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

