---
title: "Support Policy"
description: "Current preview support scope, tested versions, security fix expectations, and operator responsibilities for bao-kms-provider."
weight: 90
---

# Support Policy

This page explains which configurations are currently tested and what operators
should expect from the preview release line.

## Current Status

The current public release line is preview. Use it for labs, staging, and
evaluation of the deployment model. Do not use preview releases for production
control planes.

Preview support is best effort. There is no long-term support window, no
production service-level objective, and no guarantee that adjacent Kubernetes,
OpenBao, operating-system, auth, or deployment variants will work unless they
are listed as tested.

## Tested Preview Scope

| Component | Version |
|---|---|
| OpenBao | `2.5.4` |
| Kubernetes | `1.34` and `1.35` release lines, exact Kind node-image pins in CI |
| KMS API | v2 |
| OS | Linux |
| Deployment modes | systemd and static pod |

Kubernetes `1.36` is the intended next validation line once a digest-pinned
Kind node image is available. Kubernetes `1.29+` KMS v2 clusters may work, but
unlisted versions are not part of the tested preview scope. See
[Reference: Compatibility](/reference/compatibility/) for the detailed matrix.

## What Preview Covers

A preview tag covers the versions, artifacts, and deployment models listed in
that release's notes and compatibility table. In the default path, this means:

- KMS v2 behavior against the tested Kubernetes and OpenBao versions.
- OpenBao Transit with `aes256-gcm96`.
- JWT auth in the default build.
- systemd and static-pod deployment samples.
- Release artifacts with checksums, SBOMs, signatures, and provenance
  attestations.

Optional PKCS#11 certificate-auth artifacts are covered only when a release
publishes those artifacts and marks the PKCS#11 path as tested.

Preview releases do not cover production readiness, unlisted Kubernetes or
OpenBao versions, unlisted OpenBao HA topologies, SPIFFE/SPIRE user
configuration, performance SLOs, or long-term maintenance windows.

## Security Fixes

Before a stable release line exists, security fixes apply to the latest released
preview line only.

Once stable releases exist, this page will document the stable-line security
fix and backport policy.

## Operator Expectations

Operators using preview releases should:

- pin exact plugin versions,
- pin OpenBao and Kubernetes versions,
- keep etcd and OpenBao backups paired,
- validate upgrades in staging,
- run `bao-kms-provider doctor` on every control-plane node,
- avoid main, nightly, release candidate, and preview channels in production,
- avoid changing identity-bearing configuration fields after encryption begins; see [Configuration: Identity-Bearing Fields](/reference/configuration/#identity-bearing-fields).
