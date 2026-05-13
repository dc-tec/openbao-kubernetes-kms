---
title: "Key ID And AAD"
description: "Authoritative reference for the Kubernetes key_id format, KMS v2 annotations, AAD envelope shape, decrypt validation order, and local registry state."
weight: 40
---

# Key ID And AAD

This page is the authoritative reference for the Kubernetes `key_id` format, KMS v2 annotations, the AAD envelope shape, decrypt validation order, and the local registry state. For the security framing of these mechanisms see [Security: AAD And Decrypt Validation](/security/aad-and-decrypt-validation/).

## Goals

- Keep Kubernetes `key_id` opaque and non-secret.
- Prevent raw OpenBao topology from leaking into etcd metadata.
- Ensure `key_id` values are stable across plugin restart.
- Ensure `key_id` changes when the active Transit key version changes.
- Keep old `key_id` values decryptable while old Transit versions are allowed.
- Bind ciphertext to provider, cluster, OpenBao namespace, key lineage, and key version through AAD.

## Kubernetes `key_id` Properties

`key_id` must be:

- opaque,
- deterministic from stable non-secret inputs,
- safe to log,
- stable across plugin restarts,
- unique across provider, cluster, OpenBao namespace, Transit mount, key lineage, and key version scope,
- never reused,
- changed when the active Transit key version changes,
- not a raw Transit key name,
- not a raw Transit mount path,
- not a raw OpenBao namespace,
- not a simple Transit version integer.

Kubernetes documentation states that `key_id` is public, may be logged, must remain stable, must not flip-flop, and must not be reused.

## Recommended Format

Conceptual format:

```text
obk2.<base64url-sha256>
```

Conceptual derivation:

```text
sha256(
  "openbao-kubernetes-kms/key-id/v1" || 0x00 ||
  provider_name || 0x00 ||
  cluster_id || 0x00 ||
  openbao_instance_id || 0x00 ||
  openbao_namespace || 0x00 || # only when configured
  transit_mount_id || 0x00 ||
  transit_key_lineage_id || 0x00 ||
  transit_key_version || 0x00 ||
  transit_version_created_at_unix
)
```

Inputs:

| Input | Source | Requirement |
|---|---|---|
| `provider_name` | Plugin configuration and Kubernetes `EncryptionConfiguration` | Immutable after use. |
| `cluster_id` | Plugin configuration | Stable cluster or trust-domain ID. |
| `openbao_instance_id` | Plugin configuration | Stable OpenBao trust-domain ID. |
| `openbao_namespace` | Optional plugin configuration | Stable namespace routing scope. Empty for the root namespace. |
| `transit_mount_id` | Plugin configuration | Stable opaque mount ID, not the raw path. |
| `transit_key_lineage_id` | Plugin configuration or platform metadata | Changes when the key is deleted and recreated. |
| `transit_key_version` | Transit metadata | Active version used for encryption. |
| `transit_version_created_at_unix` | Transit metadata | Canonical Unix-second creation time for the Transit version. |

## Transit Version Creation Time

Transit version creation time is part of the long-lived `key_id` contract. The
provider normalizes this value to Unix seconds before deriving `key_id` values,
persisting local state, or comparing live OpenBao metadata with retained
snapshots.

This normalization tolerates representation changes that keep the same Unix
second, such as a future metadata reader exposing sub-second precision. It does
not treat a different Unix second as equivalent. If OpenBao restore, import, or
manual metadata changes report a different creation second for an active or
retained historical Transit version, the provider fails closed because old
Kubernetes `key_id` values may no longer describe the same decryptable key
epoch.

Backup and restore procedures must preserve:

- OpenBao Transit key material,
- Transit version numbers,
- Transit version creation timestamps at Unix-second precision,
- the configured Transit key lineage ID,
- the provider local registry state file and checkpoint.

If any of these are lost or changed after rotation, do not synthesize a
replacement state file by hand. Restore the matching OpenBao and provider state
evidence, or keep the provider stopped until a controlled recovery workflow is
available for the release line.

## Mount Accessor Vs Configured Mount ID

OpenBao mount accessors can disclose topology and may change during remount or restore operations. The provider prefers a configured stable mount ID generated and managed by platform automation.

If a mount accessor is used:

- hash it before inclusion,
- never expose it directly,
- treat remount or accessor changes as planned migrations,
- document disaster recovery behavior.

## Key Lineage

The Transit key name alone is not a safe identity. If a Transit key is deleted and recreated with the same name, the new key cannot decrypt old ciphertext.

The platform assigns a `transit_key_lineage_id` when the Transit key is created. The plugin uses that value in `key_id` and AAD derivation. Recreating a key requires a new lineage ID and a documented migration plan.

The plugin refuses to operate when the configured lineage does not match expected administrative metadata where such metadata is available.

## Annotations

Annotations are plaintext Kubernetes KMS metadata. They are stored with encrypted data and must never contain secrets.

Recommended annotations:

```yaml
provider.kms.openbao.org: "openbao-transit"
key-id-hash.kms.openbao.org: "<base64url-sha256-key-id>"
transit-key-version.kms.openbao.org: "2"
transit-mount-hash.kms.openbao.org: "<base64url-sha256-mount-id>"
transit-key-hash.kms.openbao.org: "<base64url-sha256-key-lineage-id>"
openbao-namespace-hash.kms.openbao.org: "<base64url-sha256-namespace>" # only when configured
plugin-version.kms.openbao.org: "0.1.0"
aad-version.kms.openbao.org: "v1"
```

Rules:

- annotation keys are fully qualified domain names, not Kubernetes annotation `domain/name` keys,
- annotation values are non-secret,
- raw topology values are hashed before storage,
- unknown required annotation versions are rejected,
- annotation and key snapshot mismatch is rejected,
- annotation size is small and bounded.

## OpenBao Request IDs

OpenBao request IDs can be useful for correlating plugin logs and OpenBao audit logs. They are not stored in KMS annotations by default because they add noise, increase metadata size, and may expose operational correlation details.

The provider:

- logs OpenBao request IDs in plugin logs only when available and safe,
- does not include request IDs in annotations by default,
- supports a debug-only correlation mode for controlled incident response. See [Reference: Observability: Correlation With OpenBao](/reference/observability/#correlation-with-openbao).

## AAD Envelope

For supported AEAD Transit key types, the provider uses OpenBao Transit `associated_data` by default.

Canonical AAD payload before base64 encoding:

```json
{
  "aad_version": "v1",
  "purpose": "kubernetes-etcd-kms-v2",
  "provider": "openbao-transit",
  "provider_name": "openbao-kms-workload-a",
  "cluster_id_hash": "base64url-sha256(cluster-id)",
  "openbao_instance_hash": "base64url-sha256(openbao-instance-id)",
  "openbao_namespace_hash": "base64url-sha256(openbao-namespace)",
  "transit_mount_hash": "base64url-sha256(transit-mount-id)",
  "transit_key_hash": "base64url-sha256(transit-key-lineage-id)",
  "key_id_hash": "base64url-sha256(kubernetes-key-id)",
  "key_version": "3"
}
```

Serialization rules:

- use canonical JSON or another explicitly specified canonical encoding,
- do not include secrets,
- do not include raw OpenBao paths,
- do not include raw key names,
- include only a namespace hash when `openbao.namespace` is configured,
- include enough annotation data to reconstruct the same bytes during decrypt,
- treat missing required fields as decrypt failure.

## AAD Mode

| Mode | Behavior | Intended use |
|---|---|---|
| `aad.required` | Encrypt and decrypt require valid AAD metadata. | Required mode. |

The current release only recognizes `aad.required`. There is no configuration
switch to disable AAD or select compatibility read modes.

## Decrypt Validation Order

1. Parse the `key_id`.
2. Look up the matching historical key snapshot.
3. Validate annotation keys and versions.
4. Validate annotation hashes against the snapshot.
5. Reconstruct AAD if required.
6. Call OpenBao Transit decrypt.

Unknown `key_id` values fail before step 6.

The implementation exposes a decrypt preflight helper that returns the resolved snapshot, parsed annotations, canonical AAD bytes, and Transit `associated_data` only after steps 1 through 5 have passed.

Snapshots use `aad.required`. Any other AAD mode in local state is rejected
during state validation.

## Local Registry State

The local registry is a non-secret JSON file that records:

- schema version,
- monotonic generation,
- previous and current state hashes,
- active Kubernetes `key_id`,
- observed and promoted key snapshots.

The file preserves rotation decisions across restart and keeps historical snapshots lookupable before Transit decrypt is attempted. A small adjacent checkpoint file records the last accepted generation and hash so a replayed older state file is rejected when the checkpoint survives. Neither file contains key material, plaintext, JWTs, tokens, raw Transit key names, or raw OpenBao mount paths. When `openbao.namespace` is configured, the namespace is persisted as non-secret identity scope so namespace drift fails closed during state validation.

The state hash and checkpoint are local integrity and replay guards, not
hardware-backed tamper evidence. They detect corruption, unsafe restore, missing
state with a surviving checkpoint, older generations, and same-generation hash
mismatches. They do not stop a privileged host-level attacker who can replace
both the state file and checkpoint with a self-consistent pair. Environments
that need stronger rollback resistance must add host controls such as protected
state directories, immutable backups, measured boot, TPM-sealed anchors, or an
external write-protected generation record.

State-file invariants enforced at load:

- the file must be regular and must not be a symlink,
- the file mode must not allow group write, execute bits, or world access,
- the parent directory must not be group or world writable,
- JSON is decoded with unknown-field rejection,
- the current hash must match the typed state body,
- malformed previous or current hashes are rejected,
- duplicate persisted `key_id` records are rejected,
- pending and rejected snapshots are retained in state but excluded from decrypt lookup,
- the checkpoint rejects older generations and same-generation hash mismatches,
- the active Transit version must not move backwards during normal promotion,
- loaded state must match the current provider, cluster, OpenBao instance, OpenBao namespace, Transit mount, lineage, key name, and AAD mode,
- active and retained historical Transit version creation times must match current Transit metadata after Unix-second normalization,
- `min_available_version` and `min_decryption_version` must not block active or retained historical versions,
- `min_encryption_version` must not block the active version.

If both the state file and checkpoint are missing, normal startup auto-bootstraps
only from initial Transit metadata: `latest_version` must be `1`,
`min_available_version` must not exclude version `1`, and
`min_decryption_version` must not exclude version `1`. This allows first install
without a preexisting state file but fails closed for brownfield import,
replacement, or recovery after Transit rotation. Recovery after rotation must
restore the state/checkpoint pair from backup or a known-good peer with matching
identity scope. A controlled `recover-state` command is deferred for the preview
line; operators must not synthesize replacement state by hand. If the checkpoint
exists but the state file is missing or older than the checkpoint, startup fails
closed.

## Golden Fixtures

The implementation maintains golden fixtures for:

- key snapshot to `key_id` derivation,
- annotations to AAD reconstruction,
- historical key snapshots after rotation,
- malformed annotation rejection.

Changing `key_id` or AAD derivation is a wire-format compatibility change. See [Reference: Compatibility: Breaking Changes](/reference/compatibility/#breaking-changes).

## Source References

- [Kubernetes KMS provider documentation](https://kubernetes.io/docs/tasks/administer-cluster/kms-provider/)
- [OpenBao Transit API](https://openbao.org/api-docs/secret/transit/)
