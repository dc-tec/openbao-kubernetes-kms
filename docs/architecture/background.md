---
title: "Background"
description: "Kubernetes etcd encryption-at-rest, KMS v2 protocol primer, and OpenBao Transit primer that bao-kms-provider builds on."
weight: 20
---

# Background

The provider design depends on Kubernetes KMS v2 and OpenBao Transit. Operators who already understand both can continue to [Overview](/architecture/overview/).

## Kubernetes Etcd Encryption-At-Rest

Kubernetes encryption at rest is configured through an
`EncryptionConfiguration` file passed to `kube-apiserver` with
`--encryption-provider-config`. If the first configured provider for a resource
is `identity`, Kubernetes stores the resource without encryption.

Kubernetes supports envelope encryption through KMS providers. In this model,
Kubernetes uses a data encryption key internally. The remote KMS protects the
material used to unwrap or derive encryption keys. The KMS provider plugin runs
on the same host as the API server and exposes a gRPC endpoint over a Unix
domain socket.

KMS v2 is the supported default for current clusters. Kubernetes documentation states:

- KMS v2 is stable as of Kubernetes 1.29.
- KMS v1 was deprecated in 1.28.
- KMS v1 is disabled by default as of 1.29.

Kubernetes encryption applies on write. Existing resources are not automatically rewritten when the encryption configuration changes. Kubernetes documents the standard rewrite pattern using `kubectl get ... -o json | kubectl replace -f -`.

For initial enablement, Kubernetes documentation recommends including `identity: {}` as the last provider until all resources have been encrypted. After migration, remove the `identity` fallback to prevent silent plaintext writes.

## KMS v2 Protocol Behavior

The KMS v2 gRPC contract has three methods: `Status`, `Encrypt`, `Decrypt`.

Important behaviors:

- Status returns the plugin version, health, and `key_id`.
- The API server polls Status approximately once per minute when healthy and more frequently when unhealthy.
- Status reads from cached state because the API server polls it continually.
- Encrypt receives plaintext and a UID, and returns ciphertext, `key_id`, and annotations.
- Decrypt receives ciphertext, `key_id`, annotations, and a UID.
- The KMS provider plugin must verify that `key_id` is one it understands before calling the underlying KMS.
- API server startup may trigger thousands of decrypt requests.
- Kubernetes recommends target latencies under 100 ms for encrypt and under 10 ms for decrypt.

KMS v2 `key_id` semantics are central to correctness. Kubernetes treats the
`key_id` from Status as authoritative. If `EncryptResponse.key_id` does not
match the currently observed `Status.key_id`, the API server discards the
encrypt result and treats the KMS provider plugin as unhealthy. The `key_id` is
public and may appear in logs. It must be stable, must not flip-flop, and must
never be reused.

KMS v2 caches unwrapped data encryption keys inside the API server after decrypt. Cold API server startup can still create a large initial decrypt burst while caches and watch state are rebuilt. This is why the project tracks both direct provider decrypt soak and API-server cold-start behavior in release evidence.

KMS v2 annotations are plaintext metadata stored in etcd with the encrypted object. Annotation keys are fully qualified domain names, and the total annotation size is bounded. This design uses annotations only for non-secret metadata needed to validate and reconstruct additional authenticated data (AAD). See [Reference: Key ID And AAD](/reference/key-id-and-aad/).

## Kubernetes Static Pods

Static pods are managed directly by kubelet without requiring the Kubernetes API server. Kubernetes documentation states that kubelet runs static pods from a host directory of manifests and that kubelet can run them without observing them through the API server.

Static pods cannot reference Kubernetes API objects such as ServiceAccounts, ConfigMaps, or Secrets. The static-pod deployment of `bao-kms-provider` therefore mounts every required file or socket, including configuration, CA bundle, selected auth material, runtime socket directory, and optional state directory, from the host. See [Deployment: Static Pod Deployment](/deployment/static-pod/).

## OpenBao Transit

OpenBao Transit provides cryptographic operations as a service. It encrypts, decrypts, signs, verifies, generates data keys, and supports key derivation and convergent encryption. Transit does not store caller plaintext or ciphertext; the caller stores the ciphertext.

Transit ciphertexts include a version prefix such as `vault:v1:...`, where the version identifies the Transit key version used for encryption.

Transit supports key versioning. New encrypt operations use the active key version unless a specific `key_version` is requested. Older versions can remain available for decrypt. OpenBao exposes `min_encryption_version` and `min_decryption_version` to restrict which versions may be used.

The Transit key API exposes metadata including the key type, derived flag, exportable flag, plaintext-backup flag, `min_decryption_version`, `min_encryption_version`, and per-version creation timestamps.

Transit encrypt accepts:

- base64-encoded plaintext,
- optional `associated_data` for AEAD modes,
- optional explicit `key_version`,
- optional `batch_input`.

Transit decrypt accepts:

- ciphertext,
- optional matching `associated_data`,
- optional `batch_input`.

Transit supports multiple symmetric key types. The current release line validates and supports `aes256-gcm96` only. Additional AEAD key types are future candidates and need implementation, compatibility, and release evidence before they are part of the supported matrix.

Transit keys can be rotated. After rotation, new encrypt operations use the new key version; existing ciphertexts can be decrypted while old versions remain available.

Transit key deletion is catastrophic for this use case. OpenBao documentation warns that deleting a Transit key makes decrypting ciphertext impossible and that deletion requires `deletion_allowed=true`. The provider's recommended posture keeps `deletion_allowed=false`; see [Architecture: Transit Key Model](/architecture/transit-key-model/).

## Source References

- [Kubernetes KMS provider documentation](https://kubernetes.io/docs/tasks/administer-cluster/kms-provider/)
- [Kubernetes encryption at rest documentation](https://kubernetes.io/docs/tasks/administer-cluster/encrypt-data/)
- [Kubernetes static Pods](https://kubernetes.io/docs/tasks/configure-pod-container/static-pod/)
- [OpenBao Transit API](https://openbao.org/api-docs/secret/transit/)
- [OpenBao Transit documentation](https://openbao.org/docs/secrets/transit/)
- [OpenBao JSON Web Token (JWT) and OpenID Connect (OIDC) auth API](https://openbao.org/api-docs/auth/jwt/)
- [OpenBao TLS certificates auth method](https://openbao.org/docs/auth/cert/)
