# Documentation Index

This repository is in the v0.1 engineering-preview implementation track. The docs remain the implementation contract for behavior, operations, and security-sensitive decisions.

## Source Design

- [Design](design.md): Full technical design and rationale.
- [Testing strategy](testing-strategy.md): Required test layers and validation strategy.
- [E2E framework](e2e-framework.md): Ginkgo/Gomega suite layout, manifest routing, labels, and reports.
- [Research notes](research-notes.md): Upstream Kubernetes and OpenBao facts verified for the docs.

## Implementation Contracts

These documents should be treated as implementation contracts:

- [Architecture](architecture.md)
- [KMS v2 contract](kms-v2-contract.md)
- [Key ID and AAD](key-id-and-aad.md)
- [Configuration](configuration.md)
- [OpenBao setup](openbao-setup.md)
- [Code quality](code-quality.md)
- [Release gates](release-gates.md)
- [Release policy](release-policy.md)
- [Support policy](support-policy.md)
- [Implementation backlog](workstreams/implementation-backlog.md)
- [MVP board](workstreams/mvp-board.md)

## Operator Guides

- [Install](install.md)
- [Deployment artifacts](../deploy/README.md)
- [Kubernetes encryption config](kubernetes-encryption-config.md)
- [systemd deployment](deployment/systemd.md)
- [Static pod deployment](deployment/static-pod.md)
- [Linux identity model](deployment/linux-identity-model.md)
- [Rotation](operations/rotation.md)
- [Disaster recovery](operations/disaster-recovery.md)
- [Prometheus alert rules](operations/prometheus-alerts.yaml)
- [Troubleshooting](troubleshooting.md)

## Security And Maintenance

- [Threat model](security/threat-model.md)
- [WS12 implementation security review](security/reviews/ws12-implementation-security-review.md)
- [Hardening](security/hardening.md)
- [Observability](observability.md)
- [CLI reference](cli.md)
- [Development guide](development.md)
- [Code quality](code-quality.md)
- [Compatibility](compatibility.md)
- [CI and supply chain](ci-supply-chain.md)
- [Architecture decisions](adr/)

## Documentation Rules

- Do not claim production readiness until the release gates are met.
- Do not claim support for Kubernetes or OpenBao versions that are not tested in CI.
- Keep user-facing examples aligned with the configuration reference.
- Keep security and recovery warnings duplicated where operators need them, even if that repeats the design.
