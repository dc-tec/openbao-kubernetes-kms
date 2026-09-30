---
title: Background
description: "The Kubernetes KMS v2 and OpenBao Transit behavior the provider's design depends on."
eyebrow: Architecture
weight: 20
verifiedBy:
  - internal/kmsv2
  - internal/openbao/transit.go
---

This page lists only the upstream behavior that shapes the provider. For the
full picture, see
[Kubernetes encryption at rest](https://kubernetes.io/docs/tasks/administer-cluster/encrypt-data/),
the [Kubernetes KMS provider docs](https://kubernetes.io/docs/tasks/administer-cluster/kms-provider/),
and [OpenBao Transit](https://openbao.org/docs/secrets/transit/).

## Why a provider is needed

OpenBao Transit encrypts and decrypts caller-supplied data, but OpenBao does
not implement the Kubernetes KMS gRPC protocol. The API server calls a local
KMS plugin over a Unix domain socket and never calls Transit directly.
`bao-kms-provider` adapts the two protocols and adds the Kubernetes-specific
rules for `key_id` stability, additional authenticated data (AAD) binding,
decrypt validation, and rotation.

## Kubernetes KMS v2

- The API server encrypts resources listed in its `EncryptionConfiguration`
  with data encryption keys, and asks a local KMS plugin over a Unix socket to
  protect those keys. KMS v2 is stable since 1.29, where KMS v1 is disabled by
  default.
- Encryption applies on write. Existing objects stay as they are until they are
  rewritten, which is why migration and the `identity` fallback exist.
- The API server polls `Status` about once a minute when healthy, and more
  often when not. `Status` must therefore answer from cached state.
- `Status.key_id` is authoritative. An encrypt response with a different
  `key_id` is discarded and the plugin marked unhealthy. A `key_id` change
  tells Kubernetes that data under the old one is stale, so `key_id` must be
  stable, never flip-flop, and never be reused. It is public and may be logged.
- The plugin must recognize a `key_id` before calling its backend.
- The API server caches decrypted data keys, but a cold start can still send
  thousands of decrypt calls at once. Kubernetes suggests encrypt under 100 ms
  and decrypt under 10 ms.
- Annotations are plaintext metadata stored next to the ciphertext, with
  fully qualified domain-name keys and a bounded size.
- Static pods run from host manifests without the API server, so they cannot
  use ConfigMaps, Secrets, or ServiceAccounts.

## OpenBao Transit

- Transit encrypts and decrypts caller data without storing it; the caller
  stores the ciphertext, which carries a `vault:v<N>:` version prefix.
- Keys are versioned. Encryption uses the latest version unless `key_version`
  is given, and older versions keep decrypting unless `min_decryption_version`
  or `min_encryption_version` restricts them.
- Key metadata exposes the type, derived, exportable, and plaintext-backup
  flags, the minimum versions, and a creation time per version.
- AEAD key types such as `aes256-gcm96` accept `associated_data`, which
  decryption must match.
- Deleting a key makes its ciphertext permanently undecryptable, and is
  possible only with `deletion_allowed=true`.
