---
title: Architecture
description: "Maintainer-facing design rationale: components, KMS v2 and Transit background, key model, rotation model, failure modes, and prior art."
eyebrow: Architecture
weight: 60
---

These pages explain why the provider is shaped the way it is. They are maintainer-facing and assume operator-side context from Start Here, Deployment, and Operations.

## Topics

- [Components and data flow](/docs/architecture/overview/) for the component model, data flow, trust boundaries, and deployment shape.
- [Background](/docs/architecture/background/) for the Kubernetes etcd encryption and KMS v2 protocol primer, plus the OpenBao Transit primer.
- [Transit key model](/docs/architecture/transit-key-model/) for the OpenBao Transit key, policy, and isolation design.
- [Rotation model](/docs/architecture/rotation-model/) for the rotation invariants the provider enforces against the Transit key version.
- [Failure modes](/docs/architecture/failure-modes/) for the catalog of failure scenarios, observability signals, and design responses.
- [Related work](/docs/architecture/related-work/) for existing Vault Transit KMS plugin work and the design influences this project carries forward.

## Use another section if

- the question is about how to install, wire, or operate the provider: go to [Get started](/docs/get-started/) or [Operate](/docs/operate/).
- the question is about exact behavior or contract detail: go to [Reference](/docs/reference/).
- the question is about contributing or local development: go to [Contribute](/contribute/).
