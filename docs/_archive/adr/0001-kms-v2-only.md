# 0001: KMS v2 Only By Default

## Status

Accepted for design.

## Context

Kubernetes KMS v2 is stable from Kubernetes 1.29. KMS v1 is deprecated and disabled by default in Kubernetes 1.29 and later.

Supporting KMS v1 in the primary binary would add protocol, test, and operational complexity for a legacy path.

## Decision

The primary implementation will support Kubernetes KMS v2 only.

KMS v1 compatibility, if ever needed, must be a separate legacy mode, build, branch, or explicit non-default feature.

## Consequences

- Kubernetes 1.34 release line is the initial v0.1 validation target, even though KMS v2 exists earlier.
- Tests focus on KMS v2 Status, key ID, annotations, and decrypt validation.
- Documentation does not include KMS v1 setup.
- Users with older clusters need a different solution or a future legacy build.
