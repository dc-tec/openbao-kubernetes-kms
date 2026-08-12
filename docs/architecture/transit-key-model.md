---
title: "Transit Key Model"
description: "OpenBao Transit key, policy, and isolation design rationale: key ownership, recommended profile, optional key types, features to use and avoid."
weight: 30
---

# Transit Key Model

This page is the maintainer-facing rationale for the OpenBao Transit key, policy, and isolation choices. For the operator commands that provision the key see [Getting Started: OpenBao Setup](/getting-started/openbao-setup/). For the canonical policy and Transit key configuration examples see [Reference: Transit Policy Examples](/reference/transit-policy-examples/).

## Key Ownership

The Transit key is created and managed outside the plugin by:

- platform automation,
- OpenTofu or Terraform,
- an OpenBao operator,
- an administrative workflow,
- or a management-plane controller.

The plugin token has no permission to create, delete, rotate, export, back up, or restore the key. The rationale is least-privilege: a plugin compromise does not give an attacker key-management authority. Key ownership stays in operator-controlled tooling, which already has the audit trail and change-control around it.

## Recommended Transit Key Profile

Recommended values and why each one is the way it is:

| Setting | Recommendation | Rationale |
|---|---|---|
| `type` | `aes256-gcm96` | Default compatibility choice. AES-GCM is the conservative AEAD mode and aligns with most regulated environments. |
| `derived` | `false` | Avoid treating derivation context as the primary cluster isolation boundary. The plugin uses configured cluster scope and AAD instead. |
| `convergent_encryption` | `false` | Kubernetes encryption-at-rest does not need deterministic ciphertext. Convergent encryption can leak equality relationships between objects. |
| `exportable` | `false` | Key export increases blast radius. Once enabled, OpenBao does not allow the flag to be turned off again. |
| `allow_plaintext_backup` | `false` | Plaintext backup increases blast radius. Once enabled, OpenBao does not allow the flag to be turned off again. |
| `deletion_allowed` | `false` | Key deletion can make data unrecoverable. The plugin token also lacks delete capability; this flag is a second layer of defense at the key level. |
| `auto_rotate_period` | `0` | Manual or platform-driven rotation is easier to coordinate with Kubernetes storage migration. See [Architecture: Rotation Model](/architecture/rotation-model/). |
| `disable_upsert` (mount-level) | `true` | Prevents typo-driven accidental key creation through a misspelled encrypt path. |

`disable_upsert` is configured at the Transit mount level. The provider verifies this setting during each metadata probe. Status is unhealthy when the setting is false or unreadable. If the mount is shared with other applications, enabling it would affect those callers. A dedicated Transit mount for Kubernetes KMS keys is therefore recommended.

## Future Key Types

Other AEAD Transit key types may be evaluated in a future release. They must not replace `aes256-gcm96` in the supported matrix until the provider validation, OpenBao integration tests, compatibility policy, diagnostics, and release evidence explicitly cover them.

## Recommended Policy Surface

The plugin token receives only the capabilities needed for the encrypt and decrypt path:

- Transit metadata read on the configured key path,
- Transit encrypt update on the configured key path,
- Transit decrypt update on the configured key path,
- inspection of `transit/config/keys` to verify `disable_upsert`,
- `sys/capabilities-self` for `doctor`'s self-capability check.

Capabilities the plugin must not have:

- Transit key rotation (operator action),
- Transit key delete (a destructive operation that the operator handles deliberately),
- Transit key export read,
- Transit plaintext backup read,
- broad `sudo` or admin authority.

OpenBao policies are path-based and deny by default; capabilities are only what is explicitly granted. For the canonical policy text and the renderable CLI command see [Reference: Transit Policy Examples](/reference/transit-policy-examples/).

## Features To Use

| Transit feature | Use in this design |
|---|---|
| `key_version` | Required on every encrypt. Avoids implicit-latest races during rotation. |
| `associated_data` | Required AAD binding for supported AEAD key types; see [Reference: Key ID And AAD](/reference/key-id-and-aad/). |
| `min_encryption_version` | Useful as a guard after rotation to prevent encryption with retired versions. Operator-driven, not plugin-driven. |
| `min_decryption_version` | Dangerous if raised too early; only after independent migration evidence and backup-retention evidence. `verify-rotation` alone is not enough. See [Operations: Rotation: min_decryption_version](/operations/rotation/#min_decryption_version). |
| `disable_upsert` | Enabled at the mount level. |
| `batch_input` | Outside the provider runtime for this release line; future decrypt coalescing must be introduced with explicit benchmarks and failure semantics. |
| `rewrap` | Operational tool outside the Kubernetes KMS hot path. |

## Features To Avoid

| Transit feature | Reason |
|---|---|
| Transit datakey generation | Kubernetes KMS v2 already defines the API server and plugin envelope interaction. Datakey generation would complicate semantics without clear benefit. |
| Convergent encryption | Deterministic ciphertext is not needed and can reveal equality relationships. |
| Derived keys as primary cluster isolation | Increases complexity and makes context management security-critical. The design prefers one key per cluster or trust domain instead. |
| Plugin-managed key rotation | Rotation must be coordinated with Kubernetes `key_id` observation and resource migration. The plugin observes rotation but does not perform it. |

## One Key Per Cluster Or Trust Domain

The design recommends one named Transit key per Kubernetes cluster or trust domain. This:

- isolates blast radius across clusters,
- makes AAD scope unambiguous,
- simplifies the OpenBao policy surface,
- aligns with the [Threat Model](/security/threat-model/) "Ciphertext replay across clusters" control.

Sharing a single Transit key across clusters is rejected at the design level because it weakens AAD's cross-cluster replay guarantee and forces operational coupling between clusters.
