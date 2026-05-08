# 0007: Pinned CI Version Matrix

## Status

Accepted for design.

## Context

Floating `latest` CI inputs make support claims hard to reproduce and weaken supply-chain evidence. The OpenBao Operator CI model uses central version policy and explicit validation lanes.

The KMS provider is a control-plane critical component, so its CI matrix should be reproducible and release evidence should identify exact tested versions.

## Decision

CI and release gates must use exact-pinned versions, not floating `latest`.

Initial v0.1 matrix:

- OpenBao `2.5.3`.
- Kubernetes `1.34` release line, with exact patch and Kind node image digest pinned in the future version policy file.

Kubernetes `1.35+` may be added only after exact-pinned release-gate lanes exist.

## Consequences

- Compatibility docs distinguish validated versions from future candidates.
- CI needs a central version manifest.
- Workflows must not repeat concrete versions.
- Release evidence must include the exact versions and image digests used.

