# Support Policy

This document defines the planned support posture for the KMS provider.

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

Initial v0.1 validation target:

| Component | Version |
|---|---|
| OpenBao | `2.5.3` |
| Kubernetes | `1.34` release line, exact patch pinned in CI |
| KMS API | v2 |
| OS | Linux |
| Deployment modes | systemd and static pod |

Future Kubernetes release lines are not supported just because they are newer. They become supported only after exact-pinned CI and release-gate coverage exists.

## Support Terms

Use these terms consistently:

- `Validated`: explicitly exercised in CI or release gate.
- `Candidate`: planned for future validation, not a support claim.
- `Engineering preview`: suitable for lab and design validation, not production.
- `Production-ready`: all production readiness gates passed.

## Security Fixes

Before a stable release line exists, security fixes apply to the latest released preview only.

After stable releases exist, security fix policy should be revisited and documented before production-ready claims.

## Operator Expectations

Operators should:

- pin exact plugin versions,
- pin OpenBao and Kubernetes versions,
- keep etcd and OpenBao backups paired,
- validate upgrades in staging,
- run `doctor` on every control-plane node,
- avoid edge, nightly, and prerelease channels in production,
- avoid changing identity-bearing config fields after encryption begins.

