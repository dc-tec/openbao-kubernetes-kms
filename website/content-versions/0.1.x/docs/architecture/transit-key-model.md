---
title: Transit key model
description: "Why the Transit key is owned outside the provider, why each key setting has its value, and which Transit features the design uses and avoids."
eyebrow: Architecture
weight: 30
verifiedBy:
  - internal/openbao/transit.go
  - deploy/opentofu/openbao-kubernetes-kms/main.tofu
---

Each Kubernetes cluster or trust domain gets one dedicated Transit key on a
dedicated mount. For the commands, see
[Get started: Prepare OpenBao](/docs/get-started/openbao/); for the policy
text, see [Configure: OpenBao auth and policy](/docs/configure/openbao-auth/).

## Key ownership

Platform automation or an OpenBao administrator creates, rotates, and backs up
the key. The provider token can only read metadata, encrypt, and decrypt, so a
compromised provider gains no key-management authority, and key changes stay
in existing change control and audit.

One key per cluster isolates blast radius, keeps AAD scope unambiguous, and
keeps the policy small. Sharing a key across clusters would weaken the
cross-cluster replay protection and couple the clusters operationally.

## Key settings

| Setting | Value | Why |
|---|---|---|
| `type` | `aes256-gcm96` | A conservative AEAD mode and the only tested type. |
| `derived` | `false` | Cluster isolation comes from a dedicated key and AAD, not from derivation context. |
| `convergent_encryption` | `false` | Deterministic ciphertext would reveal which objects are equal. |
| `exportable` | `false` | Export widens the blast radius, and OpenBao cannot turn it off again. |
| `allow_plaintext_backup` | `false` | Same risk as export, and also irreversible. |
| `deletion_allowed` | `false` | Deletion destroys data; this is a second layer behind the policy's missing delete capability. |
| `auto_rotate_period` | `0` | Rotation must be coordinated with `key_id` promotion and resource rewrites. |
| `disable_upsert` (mount) | `true` | A misspelled key name must fail instead of creating a key. It applies to the whole mount, which is why the mount is dedicated. |

The provider checks `disable_upsert` on every metadata probe and reports
unhealthy when it is off or unreadable.

## Transit features

| Feature | Use |
|---|---|
| `key_version` | Always sent on encrypt, so rotation cannot race the call. |
| `associated_data` | Always sent; see [Reference: Key ID and AAD](/docs/reference/key-id-and-aad/#aad-envelope). |
| `min_encryption_version`, `min_decryption_version` | Operator-managed; the provider observes them. See [Rotation model](/docs/architecture/rotation-model/). |
| `batch_input` | Not used; decrypt micro-batching needs benchmarks and failure semantics first. |
| `rewrap` | Outside the KMS path. Kubernetes detects stale data through `key_id` changes and rewrites resources itself. |
| Data key generation | Avoided; KMS v2 already defines the envelope between the API server and the plugin. |
| Convergent or derived keys | Avoided; see the settings above. |
| Provider-driven rotation | Avoided; the provider observes rotation and never performs it. |
