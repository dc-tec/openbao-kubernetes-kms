---
title: "Support Policy"
description: "Support posture for bao-kms-provider: preview status, validation scope, support terms, security fix policy, and operator expectations."
weight: 90
---

# Support Policy

This page defines the support posture for `bao-kms-provider`.

## Current Status

The current public release line is a preview line. It is suitable for
controlled validation and design review. It is not a production support claim.

## Preview Support Posture

Support expectations:

- issue triage is best effort,
- production use is not recommended,
- compatibility claims are limited to tested release evidence,
- operators must validate in their own staging environment,
- no long-term support window is promised.

## Initial Validation Scope

| Component | Version |
|---|---|
| OpenBao | `2.5.3` |
| Kubernetes | `1.34` and `1.35` release lines, exact Kind node-image pins in CI |
| KMS API | v2 |
| OS | Linux |
| Deployment modes | systemd and static pod |

Kubernetes `1.36` is the intended next validation line once a digest-pinned
Kind node image is available. Kubernetes `1.29+` KMS v2 clusters may work, but
unlisted versions are not validated in CI and are not part of the preview
support claim. Future Kubernetes release lines are not supported by virtue of
being newer; they become supported only after exact-pinned CI and release
evidence exists. See [Reference: Compatibility](/reference/compatibility/).

## What The Preview Gate Proves

A passing public-preview release gate proves only the lanes recorded for that
tag's evidence bundle:

- KMS v2 behavior against the pinned Kubernetes and OpenBao versions.
- OpenBao Transit with `aes256-gcm96`.
- JWT auth in the default build.
- PKCS#11 certificate auth only when the release includes matching opt-in
  artifacts and E2E evidence.
- systemd and static-pod deployment artifacts where release evidence includes
  them.
- OpenBao failure, HA failover, restore, rotation, upgrade/rollback, and soak
  behavior in the tested CI and validation environments.
- SBOM, signing, provenance, checksum, scan, and reproducibility evidence
  produced by the release workflow.

It does not prove production readiness, unsupported Kubernetes or OpenBao
versions, OpenBao HA topologies beyond the release lanes, SPIFFE/SPIRE user
configuration, a performance SLO, or a long-term support window.

## Support Terms

The following terms are used consistently across documentation:

- `Validated`: explicitly exercised in CI or release evidence.
- `Candidate`: planned for future validation; not a support claim.
- `Preview`: suitable for controlled validation and design review; not production.
- `Production-ready`: all production-readiness gates passed.

## Security Fixes

Before a stable release line exists, security fixes apply to the latest released
preview line only.

Once stable releases exist, the security-fix policy is revisited and documented before any production-ready claim.

## Operator Expectations

Operators using preview releases should:

- pin exact plugin versions,
- pin OpenBao and Kubernetes versions,
- keep etcd and OpenBao backups paired,
- validate upgrades in staging,
- run `bao-kms-provider doctor` on every control-plane node,
- avoid main, nightly, release candidate, and preview channels in production,
- avoid changing identity-bearing configuration fields after encryption begins; see [Configuration: Identity-Bearing Fields](/reference/configuration/#identity-bearing-fields).
