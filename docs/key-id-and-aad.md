# Key ID And AAD

This document defines Kubernetes `key_id`, KMS annotations, and OpenBao Transit associated data behavior.

## Goals

- Keep Kubernetes `key_id` opaque and non-secret.
- Prevent raw OpenBao topology from leaking into etcd metadata.
- Ensure key IDs are stable across restart.
- Ensure key IDs change when active Transit key version changes.
- Ensure old key IDs remain decryptable while old Transit versions are allowed.
- Bind ciphertext to provider, cluster, key lineage, and key version through AAD.

## Kubernetes Key ID Properties

`key_id` must be:

- opaque,
- deterministic from stable non-secret inputs,
- safe to log,
- stable across plugin restarts,
- unique across provider/cluster/OpenBao/mount/key lineage/key version scope,
- never reused,
- changed when the active Transit key version changes,
- not a raw Transit key name,
- not a raw Transit mount path,
- not a raw OpenBao namespace,
- not a simple Transit version integer.

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
| `provider_name` | Config and Kubernetes `EncryptionConfiguration` | Immutable after use. |
| `cluster_id` | Config | Stable cluster or trust-domain ID. |
| `openbao_instance_id` | Config | Stable OpenBao trust-domain ID. |
| `transit_mount_id` | Config | Stable opaque mount ID, not raw path. |
| `transit_key_lineage_id` | Config or platform metadata | Changes when the key is deleted/recreated. |
| `transit_key_version` | Transit metadata | Active version used for encryption. |
| `transit_version_created_at_unix` | Transit metadata | Distinguishes historical versions and restores. |
| `key_epoch` | Optional config | Manual emergency discriminator. |

## Key Lineage

Transit key name alone is not a safe identity. If a Transit key is deleted and recreated with the same name, the new key cannot decrypt old ciphertext.

The platform must assign a `transit_key_lineage_id` when the Transit key is created. The plugin uses that value in key ID and AAD derivation. Recreating a key requires a new lineage ID and a migration plan.

## Annotations

Annotations are plaintext Kubernetes KMS metadata. They are stored with encrypted data and must never contain secrets.

Recommended annotations:

```yaml
kms.openbao.org/provider: "openbao-transit"
kms.openbao.org/key-id-hash: "<base64url-sha256-key-id>"
kms.openbao.org/transit-key-version: "2"
kms.openbao.org/transit-mount-hash: "<base64url-sha256-mount-id>"
kms.openbao.org/transit-key-hash: "<base64url-sha256-key-lineage-id>"
kms.openbao.org/plugin-version: "v0.1.0"
kms.openbao.org/aad-version: "v1"
```

Rules:

- Annotation keys must be fully qualified.
- Annotation values must be non-secret.
- Hash raw topology values before storing them.
- Reject unknown required annotation versions.
- Reject annotation/key snapshot mismatch.
- Keep annotation size small and bounded.

## AAD Envelope

For supported AEAD Transit key types, the plugin should use OpenBao Transit `associated_data` by default.

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

- Use canonical JSON or another explicitly specified canonical encoding.
- Do not include secrets.
- Do not include raw OpenBao paths.
- Do not include raw key names.
- Include enough annotation data to reconstruct the same bytes during decrypt.
- Treat missing required fields as decrypt failure.

## Compatibility Modes

| Mode | Behavior | Intended use |
|---|---|---|
| `aad.required` | Encrypt and decrypt require valid AAD metadata. | Required v0.1 MVP behavior. |
| `aad.optional-read` | Encrypt with AAD, decrypt configured old epochs without AAD. | Future migration mode only. |
| `aad.disabled` | Do not send Transit associated data. | Compatibility testing only; not a v0.1 support path. |

Compatibility modes must be:

- explicit in config,
- visible in metrics/logs,
- limited to known key epochs,
- removed after migration.

## Decrypt Validation Order

1. Parse `key_id`.
2. Find matching historical key snapshot.
3. Validate annotation keys and versions.
4. Validate annotation hashes against the snapshot.
5. Reconstruct AAD if required.
6. Call Transit decrypt.

Unknown `key_id` values must fail before step 6.

## Golden Fixtures

Implementation must keep golden fixtures for:

- key snapshot to `key_id`,
- annotations to AAD,
- historical key snapshots after rotation,
- pre-AAD compatibility objects,
- malformed annotation rejection.

Changing key ID or AAD derivation is a wire-format compatibility change and requires an ADR plus migration plan.

## Source References

- [Kubernetes KMS provider documentation](https://kubernetes.io/docs/tasks/administer-cluster/kms-provider/)
- [OpenBao Transit API](https://openbao.org/api-docs/secret/transit/)
