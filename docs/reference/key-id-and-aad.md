---
title: Key ID and AAD
description: "The key_id format and derivation, KMS v2 annotations, the AAD envelope, decrypt validation order, and the invariants of local registry state."
eyebrow: Reference · Contract
weight: 60
verifiedBy:
  - internal/keyregistry/keyregistry.go
  - internal/aad/aad.go
  - internal/keyregistry/retirement.go
---

This page defines the wire contract between the provider, Kubernetes, and
OpenBao. Changing any of it is a breaking change; see
[Compatibility](/docs/reference/compatibility/#breaking-changes). For what the
contract protects, see [Security: AAD and decrypt validation](/docs/security/aad-and-decrypt-validation/).

## key_id format

Kubernetes treats `key_id` as public, stable, never reused, and never
flip-flopping. The provider's `key_id` is an opaque hash that changes with
every Transit key version and never exposes a key name, mount path, namespace,
or bare version number:

```text
obk2.<base64url-sha256>

sha256(
  "openbao-kubernetes-kms/key-id/v1" || 0x00 ||
  provider_name || 0x00 ||
  cluster_id || 0x00 ||
  openbao_instance_id || 0x00 ||
  openbao_namespace || 0x00 ||        # only when configured
  transit_mount_id || 0x00 ||
  transit_key_lineage_id || 0x00 ||
  transit_key_version || 0x00 ||
  transit_version_created_at_unix
)
```

The first six inputs come from the provider configuration and must never
change; see [Configuration: Identity-bearing fields](/docs/reference/configuration/#identity-bearing-fields).
`transit_mount_id` is a label you choose, never the OpenBao mount accessor,
which changes on remount or restore. The lineage ID changes only when the key
is deleted and recreated, so a recreated key never matches old ciphertext.
The version and its creation time come from Transit metadata.

The creation time is normalized to Unix seconds before derivation, storage, or
comparison. Sub-second representation changes are tolerated. A different
second for an active or retained version, typically after a restore or import,
makes the provider fail closed, because old `key_id` values might no longer
describe a decryptable key.

## Annotations

Annotations are stored in plaintext with the encrypted object, so they hold
only non-secret values, and raw topology is hashed:

```yaml
provider.kms.openbao.org: "openbao-transit"
key-id-hash.kms.openbao.org: "<base64url-sha256-key-id>"
transit-key-version.kms.openbao.org: "2"
transit-mount-hash.kms.openbao.org: "<base64url-sha256-mount-id>"
transit-key-hash.kms.openbao.org: "<base64url-sha256-key-lineage-id>"
openbao-namespace-hash.kms.openbao.org: "<base64url-sha256-namespace>"   # only when configured
plugin-version.kms.openbao.org: "0.1.0"
aad-version.kms.openbao.org: "v1"
```

Keys are fully qualified domain names. Unknown required annotation versions,
annotations that disagree with the key snapshot, and oversized annotations are
rejected. OpenBao request IDs are never stored in annotations.

## AAD envelope

The provider sends this canonical JSON, base64-encoded, as Transit
`associated_data` on every encryption and decryption:

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

The envelope holds no secrets, raw paths, or raw key names; the namespace hash
appears only when a namespace is configured. The annotations carry enough to
rebuild the same bytes on decryption, and a missing required field fails the
decryption. AAD is always required.

## Decrypt validation order

1. Parse the `key_id`.
2. Find the matching active, pending, or retired snapshot. For an unknown
   `key_id`, run one rate-limited metadata lookup, and validate and save any
   new identity before accepting it.
3. Validate annotation keys and versions.
4. Validate annotation hashes against the snapshot.
5. Rebuild the AAD.
6. Call Transit decrypt.

Every failure in steps 1 to 5 happens before OpenBao is called.

## Local registry state

The registry at `state.path` is a non-secret JSON file with a schema version, a
monotonic generation, the previous and current state hashes, the active
`key_id`, and every observed and promoted key snapshot. It keeps rotation
decisions across restarts and lets decryption find historical snapshots
without asking Transit. An adjacent checkpoint records the last accepted
generation and hash. Neither file holds key material, plaintext, credentials,
raw key names, or raw mount paths.

The provider checks on load that:

- the file is a regular file, not a symlink, without group write, execute
  bits, or world access, in a directory that is not group or world writable,
- the JSON has no unknown fields and its current hash matches the body, with
  well-formed hashes and no duplicate `key_id` records,
- the checkpoint accepts the generation and hash,
- the state matches the configured provider, cluster, OpenBao instance,
  namespace, mount, lineage, key name, and AAD mode,
- the creation time of every active, pending, and retired version matches
  Transit metadata to the second,
- `min_available_version` and `min_decryption_version` block no retained
  version.

During operation:

- pending snapshots decrypt once their metadata is validated; rejected and
  removed snapshots never do,
- normal rotation keeps every accepted identity; only operator retirement
  removes decryption, and its `removed` records never change afterwards,
- the active version never moves backwards,
- skipped Transit versions are kept as decrypt-only snapshots, and missing
  creation metadata for one fails closed,
- `min_encryption_version` blocking the active version makes Status and
  Encrypt unhealthy while observation continues.

When both files are missing, startup creates state only for an unrotated key:
`latest_version` is `1`, and neither `min_available_version` nor
`min_decryption_version` excludes it. In every other case, startup fails closed
until the state and checkpoint are restored; see
[Disaster recovery: Local registry state](/docs/operate/disaster-recovery/#local-registry-state).
A checkpoint without its state file, or with an older one, also fails startup.

`serve` and `retire-versions --apply` take the same persistent
`<state.path>.lock` before writing, which needs a local filesystem with working
advisory locks. The state directory must be owned by the process's OS user.

The checkpoint detects corruption, unsafe restores, and replayed older state.
It does not stop a host administrator who replaces both files with a
consistent pair; environments that need that add host controls such as
immutable backups, measured boot, or an external generation record.
