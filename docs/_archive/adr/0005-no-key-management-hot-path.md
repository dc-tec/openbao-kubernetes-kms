# 0005: No Key Management In Hot Path

## Status

Accepted for design.

## Context

The plugin runs in the Kubernetes API server persistence path. Granting it key management permissions would increase blast radius if the plugin or host is compromised.

OpenBao Transit key deletion, key recreation, export, plaintext backup, and premature version restrictions can make Kubernetes data unrecoverable or expose key material.

## Decision

The hot-path plugin will not create, rotate, delete, export, import, or back up Transit keys by default.

Transit key lifecycle is an operator/platform responsibility.

## Consequences

- OpenBao policy is narrow.
- Rotation is initiated outside the plugin.
- The plugin observes and safely promotes new versions.
- `doctor` verifies dangerous key settings.
- Operational runbooks are required for rotation and disaster recovery.

