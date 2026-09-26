---
title: Security
description: "Threat model, hardening, authentication and decrypt validation."
eyebrow: Security
weight: 50
---

These pages cover trust boundaries, authentication, hardening, and the security view of decrypt validation. Use them when the question is about scope, trust, or a sensitive failure mode rather than a workflow step.

The security section documents the intended hardened posture for
`bao-kms-provider`. For the current maturity statement see
[Reference: Support policy](/docs/reference/support-policy/), and for artifact
verification see [Get started: Install the provider](/docs/get-started/install/#verify-release-artifacts).

## Topics

- [Threat model](/docs/security/threat-model/) for in-scope and out-of-scope threats, attacker capabilities, and mitigations.
- [Hardening](/docs/security/hardening/) for runtime hardening of the provider process under systemd and as a static pod.
- [Auth model](/docs/security/auth-model/) for JSON Web Token (JWT) and certificate authentication, token lifecycle, and the rationale for avoiding a Kubernetes API circular dependency.
- [AAD and decrypt validation](/docs/security/aad-and-decrypt-validation/) for the security view of how additional authenticated data (AAD) helps the provider reject stale, unknown, or annotation-inconsistent ciphertexts.

## Use another section if

- the question is about CLI, config, or KMS v2 protocol behavior: go to [Reference](/docs/reference/).
- the question is about an operational runbook or incident response: go to [Operate](/docs/operate/).
- the question is about how the design satisfies these properties: go to [Architecture](/docs/architecture/).
