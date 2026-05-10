---
title: "Security"
description: "Threat model, hardening, JWT-first authentication and decrypt validation."
weight: 50
browse:
  - "/security/threat-model"
  - "/security/hardening"
  - "/security/auth-model"
  - "/security/aad-and-decrypt-validation"
---

# Security

These pages cover trust boundaries, authentication, hardening, and the security view of decrypt validation. Use them when the question is about scope, trust, or a sensitive failure mode rather than a workflow step.

## Topics

- [Threat Model](/security/threat-model/) for in-scope and out-of-scope threats, attacker capabilities, and mitigations.
- [Hardening](/security/hardening/) for runtime hardening of the provider process under systemd and as a static pod.
- [Auth Model](/security/auth-model/) for the JWT-first authentication design, token lifecycle, and the rationale for avoiding a Kubernetes API circular dependency.
- [AAD And Decrypt Validation](/security/aad-and-decrypt-validation/) for the security view of how the provider rejects stale, unknown, or annotation-inconsistent ciphertexts.

## Use Another Section If

- the question is about CLI, config, or KMS v2 protocol behavior: go to [Reference](/reference/).
- the question is about an operational runbook or incident response: go to [Operations](/operations/).
- the question is about how the design satisfies these properties: go to [Architecture](/architecture/).
