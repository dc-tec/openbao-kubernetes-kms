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
- Bind ciphertext to provider, cluster, key lineage, and key version through AAD.

## Kubernetes `key_id` Properties

`key_id` must be:

- opaque,
- deterministic from stable non-secret inputs,
- safe to log,
- stable across plugin restarts,
- unique across provider, cluster, OpenBao, Transit mount, key lineage, and key version scope,
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
  transit_mount_id || 0x00 ||
  transit_key_lineage_id || 0x00 ||
  transit_key_version || 0x00 ||
  transit_version_created_at_unix || 0x00 ||
  key_epoch
)
```

Inputs:

| Input | Source | Requirement |
|---|---|---|
| `provider_name` | Plugin configuration and Kubernetes `EncryptionConfiguration` | Immutable after use. |
| `cluster_id` | Plugin configuration | Stable cluster or trust-domain ID. |
| `openbao_instance_id` | Plugin configuration | Stable OpenBao trust-domain ID. |
| `transit_mount_id` | Plugin configuration | Stable opaque mount ID, not the raw path. |
| `transit_key_lineage_id` | Plugin configuration or platform metadata | Changes when the key is deleted and recreated. |
| `transit_key_version` | Transit metadata | Active version used for encryption. |
| `transit_version_created_at_unix` | Transit metadata | Distinguishes historical versions and restored lineages. |
| `key_epoch` | Optional configuration | Manual emergency discriminator. |

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
- include enough annotation data to reconstruct the same bytes during decrypt,
- treat missing required fields as decrypt failure.

## Compatibility Modes

| Mode | Behavior | Intended use |
|---|---|---|
| `aad.required` | Encrypt and decrypt require valid AAD metadata. | Required mode. |
| `aad.optional-read` | Encrypt with AAD; decrypt configured pre-AAD epochs without AAD. | Future migration mode only. |
| `aad.disabled` | Send no Transit associated data. | Compatibility testing only; not a supported mode. |

Compatibility modes:

- are explicit in configuration,
- are visible in metrics and logs,
- are limited to known key epochs,
- are removed after migration.

## Decrypt Validation Order

1. Parse the `key_id`.
2. Look up the matching historical key snapshot.
3. Validate annotation keys and versions.
4. Validate annotation hashes against the snapshot.
5. Reconstruct AAD if required.
6. Call OpenBao Transit decrypt.

Unknown `key_id` values fail before step 6.

The implementation exposes a decrypt preflight helper that returns the resolved snapshot, parsed annotations, canonical AAD bytes, and Transit `associated_data` only after steps 1 through 5 have passed.

Snapshots use `aad.required`. `aad.optional-read` and `aad.disabled` are modeled as future compatibility modes; encrypt and decrypt preparation reject them.

## Local Registry State

The local registry is a non-secret JSON file that records:

- schema version,
- monotonic generation,
- previous and current state hashes,
- active Kubernetes `key_id`,
- observed and promoted key snapshots.

The file preserves rotation decisions across restart and keeps historical snapshots lookupable before Transit decrypt is attempted. It does not contain key material, plaintext, JWTs, tokens, raw Transit key names, or raw OpenBao mount paths.

State-file invariants enforced at load:

- the file must be regular and must not be a symlink,
- the file mode must not allow group write, execute bits, or world access,
- the parent directory must not be group or world writable,
- JSON is decoded with unknown-field rejection,
- the current hash must match the typed state body,
- generation and hash expectations can reject replayed state,
- the active Transit version must not move backwards during normal promotion.

If the state file is missing, it can be rebuilt from trusted configuration plus current and historical Transit metadata when the caller can prove the metadata set is complete enough for the recovery operation.

## Golden Fixtures

The implementation maintains golden fixtures for:

- key snapshot to `key_id` derivation,
- annotations to AAD reconstruction,
- historical key snapshots after rotation,
- pre-AAD compatibility objects,
- malformed annotation rejection.

Changing `key_id` or AAD derivation is a wire-format compatibility change. See [Reference: Compatibility: Breaking Changes](/reference/compatibility/#breaking-changes).

## Source References

- [Kubernetes KMS provider documentation](https://kubernetes.io/docs/tasks/administer-cluster/kms-provider/)
- [OpenBao Transit API](https://openbao.org/api-docs/secret/transit/)
