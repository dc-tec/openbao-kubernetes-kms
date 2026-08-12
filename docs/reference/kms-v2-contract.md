---
title: "KMS v2 Contract"
description: "The Kubernetes KMS v2 gRPC behavior bao-kms-provider satisfies: endpoint, provider name, Status, Encrypt, Decrypt, annotations, error semantics, and conformance tests."
weight: 30
---

# KMS v2 Contract

This page is the authoritative reference for the Kubernetes KMS v2 protocol behavior implemented by `bao-kms-provider`. The page focuses on observable contract: what the API server sees and what the provider must guarantee.

## Baseline

The provider implements Kubernetes KMS v2. KMS v1 is out of scope for the current implementation.

Kubernetes KMS v2 is stable from Kubernetes 1.29. Kubernetes recommends KMS v2 for current clusters; KMS v1 is deprecated and disabled by default in Kubernetes 1.29 and later.

## Endpoint

The provider serves gRPC over a filesystem Unix domain socket.

Default socket path:

```text
/run/openbao-kms/kms.sock
```

The implementation rejects unsafe socket paths, symlink targets, regular files at the socket path, and unsafe parent directories. It removes a stale socket only after verifying that no live listener owns it.

## Provider Name

The Kubernetes provider name is identity-bearing. It appears in the API server `EncryptionConfiguration` and participates in `key_id` and AAD scope.

Once encrypted data exists, changing the provider name requires a migration plan. The provider fails closed or warns loudly when local configuration does not match the Kubernetes encryption configuration that `doctor` validates. See [Configuration: Identity-Bearing Fields](/reference/configuration/#identity-bearing-fields).

## Status

`Status` returns:

- the plugin API version,
- the health state,
- the active Kubernetes `key_id`.

Required behavior:

- Status reads from cached state.
- Status does not perform live Transit encrypt or decrypt.
- Status is healthy only after a metadata probe and a Transit encrypt/decrypt deep probe succeed.
- Status is unhealthy when the provider cannot verify `disable_upsert=true` on the Transit mount.
- A metadata-probe success does not clear a deep-probe failure.
- A deep-probe success does not clear a metadata-probe failure.
- Background OpenBao requests use the normal token reuse, renewal, and re-login lifecycle.
- Status becomes unhealthy when the cache exceeds `status.statusMaxStaleness`.
- Status `key_id` changes only after the rotation state machine promotes a new active snapshot.

Invariant:

```text
EncryptResponse.key_id == most_recent_healthy_Status.key_id
```

Kubernetes treats `Status.key_id` as authoritative. If encrypt returns a different `key_id`, the API server discards the encrypt response and treats the plugin as unhealthy.

## Encrypt

Input:

- plaintext bytes,
- request UID.

Output:

- Transit ciphertext bytes,
- the active Kubernetes `key_id`,
- annotations.

Required behavior:

- use exactly one active key snapshot per encrypt,
- pass an explicit Transit `key_version`,
- return the same `key_id` as cached healthy Status,
- return annotations when AAD is enabled,
- never log plaintext,
- never log full ciphertext,
- fail closed when no active snapshot exists,
- fail closed when OpenBao is unavailable or auth is invalid.

Encrypt must not:

- create a Transit key,
- rotate a Transit key,
- rely on implicit latest Transit version,
- fall back to plaintext or `identity`,
- return a stale `key_id`.

The explicit `key_version` requirement avoids a race in which the Transit key rotates between encrypt and a subsequent metadata lookup.

## Decrypt

Input:

- ciphertext bytes,
- Kubernetes `key_id`,
- annotations,
- request UID.

Output:

- plaintext bytes.

Required behavior:

- reject empty, malformed, or unknown `key_id`,
- reject known-disallowed stale `key_id`,
- reject missing annotations when AAD is required,
- reject malformed annotations,
- reject annotation and key snapshot mismatch,
- reconstruct AAD deterministically,
- call Transit decrypt only after local validation succeeds,
- never brute-force across Transit keys or key versions,
- never log plaintext,
- never log full ciphertext.

The provider requires valid AAD annotations. There is no supported mode that
decrypts without AAD. See [Security: AAD And Decrypt Validation](/security/aad-and-decrypt-validation/).

## Protocol Limits

The provider enforces the Kubernetes KMS v2 field limits at the gRPC boundary:

- `ciphertext` is non-empty and less than 1024 bytes.
- `key_id` is non-empty and less than 1024 bytes.
- annotation keys plus values are less than 32768 bytes in total.
- annotation keys and values must be valid UTF-8.
- annotation keys must be fully qualified domain names.

Decrypt requests that exceed these limits are rejected before Transit decrypt is called. Encrypt fails closed if Transit returns a ciphertext or response metadata that would exceed the KMS v2 response limits.

The gRPC server also caps inbound and outbound protobuf messages at 65536 bytes. This keeps the transport envelope bounded while leaving room for protobuf overhead around the KMS v2 field limits.

The deep status probe also checks that a real non-secret Transit encrypt/decrypt
round trip returns the expected Transit key version and ciphertext within the
KMS v2 ciphertext limit. This turns backend response-shape drift into a
readiness failure before Kubernetes depends on that response shape for new
writes.

## Annotations

KMS v2 annotations are plaintext metadata stored with encrypted data. They are non-secret and use fully qualified domain-name keys.

Allowed annotation content:

- provider marker,
- hash of Kubernetes `key_id`,
- Transit key version,
- hash of Transit mount ID,
- hash of Transit key lineage ID,
- hash of OpenBao namespace when configured,
- plugin version,
- AAD version.

Disallowed annotation content:

- plaintext,
- JWTs,
- OpenBao tokens,
- raw Transit key names,
- raw Transit mount paths,
- full OpenBao namespaces,
- full ciphertext,
- high-cardinality user-controlled values.

For the full annotation schema and AAD envelope shape see [Reference: Key ID And AAD](/reference/key-id-and-aad/).

## Decrypt Micro-Batching

OpenBao Transit supports `batch_input` for encrypt and decrypt. The provider
does not implement KMS decrypt micro-batching in this release line because the
current direct decrypt path is simpler and has been sufficient in validation so
far.

Micro-batching adds request queueing, per-request deadlines, cancellation
behavior, order preservation, fairness, and failure fan-out concerns. Do not add
or enable it until benchmarks show it improves API server startup behavior
without violating the validation thresholds below.

## Error Semantics

Errors map to stable classes in logs and metrics:

- `config_invalid`
- `socket_unavailable`
- `auth_failed`
- `auth_expired`
- `openbao_rate_limited`
- `openbao_sealed`
- `openbao_unavailable`
- `panic`
- `transit_key_missing`
- `transit_policy_denied`
- `key_id_unknown`
- `key_id_malformed`
- `aad_missing`
- `aad_mismatch`
- `annotation_invalid`
- `protocol_limit`
- `status_stale`
- `timeout`
- `canceled`
- `unknown`

Errors returned to Kubernetes are specific enough for diagnosis but contain no secrets, tokens, plaintext, full ciphertext, or raw sensitive paths. See [Reference: Observability: Error Classes](/reference/observability/#error-classes).

## Validation Thresholds

Initial validation thresholds used by tests and examples:

```yaml
status:
  p99: 5ms
  externalOpenBaoCalls: 0
encrypt:
  p95: 100ms
  p99: 250ms
decrypt:
  p95: 10ms
  p99: 50ms
```

These thresholds are not production SLOs. Validate alert thresholds against the
operator's OpenBao deployment, network path, and Kubernetes API server behavior
before using them for paging.

## Conformance Tests

The implementation includes a protocol conformance suite that uses the real KMS v2 protobuf client against the Unix socket.

Blocking cases:

- healthy Status returns a non-empty `key_id`,
- repeated Status calls do not call OpenBao,
- encrypt returns the Status `key_id`,
- encrypt output stays within KMS v2 ciphertext, `key_id`, and annotation limits,
- decrypt accepts encrypt output,
- decrypt rejects oversized ciphertext, `key_id`, and annotations before Transit,
- oversized gRPC messages are rejected over the Unix socket before Transit,
- decrypt rejects unknown `key_id` before the Transit call,
- decrypt rejects malformed annotations,
- decrypt rejects AAD mismatch,
- rotation does not produce `key_id` flip-flop,
- Status becomes unhealthy when background probes go stale.

## Source References

- [Kubernetes KMS provider documentation](https://kubernetes.io/docs/tasks/administer-cluster/kms-provider/)
- [Kubernetes KMS v2 Go package](https://pkg.go.dev/k8s.io/kms/apis/v2)
