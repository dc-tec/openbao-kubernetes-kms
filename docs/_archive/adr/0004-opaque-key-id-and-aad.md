# 0004: Opaque Key ID And AAD

## Status

Accepted for design.

## Context

Kubernetes KMS v2 stores `key_id` and annotations as metadata. They may appear in logs and etcd. Raw Transit key names, mount paths, namespaces, or simple version numbers can leak topology and increase collision risk after restore or key recreation.

OpenBao Transit supports associated data for AEAD key types.

## Decision

The plugin uses opaque, deterministic Kubernetes key IDs derived from stable non-secret scope inputs. It uses non-secret annotations and Transit associated data by default for new deployments.

## Consequences

- Key ID and AAD derivation become compatibility surfaces.
- Golden fixtures are required.
- Raw OpenBao topology is not stored in annotations.
- Decrypt validates key ID and annotations before Transit calls.
- Compatibility mode is required if AAD is introduced after old objects exist.

