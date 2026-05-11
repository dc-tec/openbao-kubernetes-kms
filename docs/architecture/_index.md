---
title: "Architecture"
description: "Maintainer-facing design rationale: components, KMS v2 and Transit background, key model, rotation model, failure modes, and prior art."
weight: 60
browse:
  - "/architecture/overview"
  - "/architecture/background"
  - "/architecture/transit-key-model"
  - "/architecture/rotation-model"
  - "/architecture/failure-modes"
  - "/architecture/related-work"
---

# Architecture

These pages explain why the provider is shaped the way it is. They are maintainer-facing and assume operator-side context from Start Here, Deployment, and Operations.

## Topics

- [Overview](/architecture/overview/) for the component model, data flow, trust boundaries, and deployment shape.
- [Background](/architecture/background/) for the Kubernetes etcd encryption and KMS v2 protocol primer, plus the OpenBao Transit primer.
- [Transit Key Model](/architecture/transit-key-model/) for the OpenBao Transit key, policy, and isolation design.
- [Rotation Model](/architecture/rotation-model/) for the rotation invariants the provider enforces against the Transit key version.
- [Failure Modes](/architecture/failure-modes/) for the catalog of failure scenarios, observability signals, and design responses.
- [Related Work](/architecture/related-work/) for existing Vault Transit KMS plugin work and the design influences this project carries forward.

## Use Another Section If

- the question is about how to install, wire, or operate the provider: go to [Start Here](/getting-started/) or [Operations](/operations/).
- the question is about exact behavior or contract detail: go to [Reference](/reference/).
- the question is about contributing or local development: go to [Development](/development/).
