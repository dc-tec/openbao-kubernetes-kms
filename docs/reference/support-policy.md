---
title: "Support Policy"
description: "v0.1 support posture for bao-kms-provider: engineering preview status, validation scope, support terms, security fix policy, and operator expectations."
weight: 90
---

# Support Policy

This page defines the support posture for `bao-kms-provider`.

## Current Status

No version is supported yet because no implementation has shipped.

## v0.1 Support Posture

v0.1 is planned as an engineering preview.

Support expectations:

- issue triage is best effort,
- production use is not recommended,
- compatibility claims are limited to tested release gates,
- operators must validate in their own staging environment,
- no long-term support window is promised.

## Initial Validation Scope

| Component | Version |
|---|---|
| OpenBao | `2.5.3` |
| Kubernetes | `1.34` release line, exact patch pinned in CI |
| KMS API | v2 |
| OS | Linux |
| Deployment modes | systemd and static pod |

Future Kubernetes release lines are not supported by virtue of being newer. They become supported only after exact-pinned CI and release-gate coverage exists. See [Reference: Compatibility](/reference/compatibility/).

## Support Terms

The following terms are used consistently across documentation:

- `Validated`: explicitly exercised in CI or a release gate.
- `Candidate`: planned for future validation; not a support claim.
- `Engineering preview`: suitable for lab and design validation; not production.
- `Production-ready`: all production-readiness gates passed.

## Security Fixes

Before a stable release line exists, security fixes apply to the latest released preview only.

Once stable releases exist, the security-fix policy is revisited and documented before any production-ready claim.

## Operator Expectations

Operators using v0.1 should:

- pin exact plugin versions,
- pin OpenBao and Kubernetes versions,
- keep etcd and OpenBao backups paired,
- validate upgrades in staging,
- run `bao-kms-provider doctor` on every control-plane node,
- avoid edge, nightly, and prerelease channels in production,
- avoid changing identity-bearing configuration fields after encryption begins; see [Configuration: Identity-Bearing Fields](/reference/configuration/#identity-bearing-fields).
