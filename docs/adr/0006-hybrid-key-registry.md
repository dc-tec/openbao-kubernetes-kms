# 0006: Hybrid Historical Key Registry

## Status

Accepted for design.

## Context

Historical Kubernetes key IDs need to remain decryptable after restart, node replacement, OpenBao restore, and Transit key rotation.

Key IDs can be derived from stable config and Transit metadata, but rotation promotion decisions also need restart-safe state. A purely local registry would make node replacement and recovery harder. A purely derived model makes pending rotation and rollback detection weaker.

## Decision

Use a hybrid model.

The Kubernetes `key_id` remains derivable from stable non-secret config and Transit metadata.

The provider also persists a small non-secret local registry for observed and promoted key snapshots, rotation decisions, AAD mode, schema version, and rollback detection.

Default path:

```text
/var/lib/openbao-kms/state/key-registry.json
```

## Consequences

- Local state is not cryptographic key material.
- Node replacement can recover by rebuilding historical snapshots from config and Transit metadata when required.
- Rotation promotion survives process restart.
- The state file needs permissions, corruption handling, backup guidance, and tests.
- `doctor` can explain config/metadata/registry mismatches.

