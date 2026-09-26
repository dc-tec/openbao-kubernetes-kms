---
title: Support policy
description: "Current preview support scope, tested versions, security fix expectations, and operator responsibilities for bao-kms-provider."
eyebrow: Reference · Lifecycle
weight: 90
---

This policy defines the tested configurations and operator expectations for the
preview release line.

## Current status

The current public release line is preview. Use it for labs, staging, and
evaluation of the deployment model. Do not use preview releases for production
control planes.

Preview support is best effort. There is no long-term support window, no
production service-level objective, and no guarantee that adjacent Kubernetes,
OpenBao, operating-system, auth, or deployment variants will work unless they
are listed as tested.

## Tested preview scope

| Component | Version |
|---|---|
| OpenBao | `2.6.0` |
| Kubernetes | `1.34` and `1.35` release lines, exact Kind node-image pins in CI |
| KMS API | v2 |
| OS | Linux |
| Deployment modes | systemd and static pod |

Kubernetes `1.36` is the intended next validation line once a digest-pinned
Kind node image is available. Kubernetes `1.29+` KMS v2 clusters may work, but
unlisted versions are not part of the tested preview scope. See
[Reference: Compatibility](/docs/reference/compatibility/) for the detailed matrix.

## What preview covers

A preview tag covers the versions, artifacts, and deployment models listed in
that release's notes and compatibility table. In the default path, this means:

- KMS v2 behavior against the tested Kubernetes and OpenBao versions.
- OpenBao Transit with `aes256-gcm96`.
- JSON Web Token (JWT) auth in the default build.
- systemd and static-pod deployment samples.
- Release artifacts with checksums, software bills of materials (SBOMs),
  signatures, and provenance attestations.

Optional certificate-auth artifacts backed by a PKCS#11 hardware or software
token are covered only when a release publishes those artifacts and marks the
PKCS#11 path as tested.

Preview releases do not cover production readiness, unlisted Kubernetes or
OpenBao versions, unlisted OpenBao HA topologies, SPIFFE/SPIRE workload identity
configuration, performance SLOs, or long-term maintenance windows.

## Security fixes

Before a stable release line exists, security fixes apply to the latest released
preview line only.

Once stable releases exist, this policy will document the stable-line security
fix and backport policy.

## Operator expectations

Operators using preview releases should:

- pin exact provider versions,
- pin OpenBao and Kubernetes versions,
- keep etcd and OpenBao backups paired,
- validate upgrades in staging,
- run `bao-kms-provider doctor` on every control-plane node,
- avoid main, nightly, release candidate, and preview channels in production,
- avoid changing identity-bearing configuration fields after encryption begins; see [Configuration: Identity-bearing fields](/docs/reference/configuration/#identity-bearing-fields).
