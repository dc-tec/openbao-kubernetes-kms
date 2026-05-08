# KMS v2 Contract

This document defines the Kubernetes KMS v2 behavior the implementation must satisfy.

## Baseline

The plugin implements Kubernetes KMS v2 by default. KMS v1 is out of scope for the primary implementation.

Kubernetes KMS v2 is stable from Kubernetes 1.29. Kubernetes recommends KMS v2 for current clusters, and KMS v1 is deprecated and disabled by default in Kubernetes 1.29 and later.

## Endpoint

The plugin serves gRPC over a filesystem Unix domain socket.

Default socket:

```text
/run/openbao-kms/kms.sock
```

The implementation must reject unsafe socket paths, symlink targets, regular files, and unsafe parent directories. It may remove a stale socket only after verifying that no live listener owns it.

## Provider Name

The Kubernetes provider name is identity-bearing. It appears in `EncryptionConfiguration` and participates in key ID/AAD scope.

Once encrypted data exists, changing the provider name requires a migration plan. The plugin must fail or warn loudly if local configuration does not match the Kubernetes encryption configuration checked by `doctor`.

## Status

`Status` returns:

- plugin API version,
- health state,
- active Kubernetes `key_id`.

Required behavior:

- Status reads from cached state.
- Status does not perform live Transit encrypt/decrypt.
- Status is healthy only when auth, Transit metadata, and active key snapshot are fresh enough.
- Status becomes unhealthy when the cache exceeds configured staleness.
- Status `key_id` changes only after the rotation state machine promotes a new active snapshot.

Invariant:

```text
EncryptResponse.key_id == most_recent_healthy_Status.key_id
```

Kubernetes treats `Status.key_id` as authoritative. If encrypt returns a different `key_id`, Kubernetes discards the encrypt response and treats the plugin as unhealthy.

## Encrypt

Input:

- plaintext bytes,
- request UID.

Output:

- Transit ciphertext bytes,
- active Kubernetes `key_id`,
- optional annotations.

Required behavior:

- Use exactly one active key snapshot.
- Pass explicit Transit `key_version`.
- Return the same `key_id` as cached healthy Status.
- Return annotations when AAD is enabled.
- Never log plaintext.
- Never log full ciphertext.
- Fail closed if no active snapshot exists.
- Fail closed if OpenBao is unavailable or auth is invalid.

Encrypt must not:

- create a Transit key,
- rotate a Transit key,
- rely on implicit latest Transit version,
- fall back to plaintext or `identity`,
- return a stale `key_id`.

## Decrypt

Input:

- ciphertext bytes,
- Kubernetes `key_id`,
- annotations,
- request UID.

Output:

- plaintext bytes.

Required behavior:

- Reject empty, malformed, or unknown `key_id`.
- Reject known-disallowed stale `key_id`.
- Reject missing annotations when AAD is required.
- Reject malformed annotations.
- Reject annotation/key snapshot mismatch.
- Reconstruct AAD deterministically.
- Call Transit decrypt only after local validation succeeds.
- Never brute-force across Transit keys or key versions.
- Never log plaintext.
- Never log full ciphertext.

v0.1 decrypt requires valid AAD annotations. A future bounded compatibility mode may support known pre-AAD epochs, but it must be explicit, observable, and time-bound.

## Annotations

KMS v2 annotations are plaintext metadata stored with encrypted data. They must be non-secret and use fully qualified keys.

Allowed annotation content:

- provider marker,
- hash of Kubernetes `key_id`,
- Transit key version,
- hash of Transit mount ID,
- hash of Transit key lineage ID,
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

## Error Semantics

The plugin should map errors into stable classes for logs and metrics:

- `config_invalid`
- `socket_unavailable`
- `auth_failed`
- `auth_expired`
- `openbao_unavailable`
- `transit_key_missing`
- `transit_policy_denied`
- `key_id_unknown`
- `key_id_malformed`
- `aad_missing`
- `aad_mismatch`
- `annotation_invalid`
- `status_stale`
- `timeout`

Errors returned to Kubernetes should be specific enough for diagnosis but must not include secrets, tokens, plaintext, full ciphertext, or raw sensitive paths.

## Latency Targets

Initial targets:

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

These targets must be validated with real OpenBao and Kubernetes API server tests before any production-readiness claim.

## Conformance Tests

The implementation must include a protocol conformance suite that uses the real KMS v2 protobuf client against the Unix socket.

Blocking cases:

- healthy Status returns non-empty `key_id`,
- repeated Status calls do not call OpenBao,
- encrypt returns Status `key_id`,
- decrypt accepts encrypt output,
- decrypt rejects unknown `key_id` before Transit call,
- decrypt rejects malformed annotations,
- decrypt rejects AAD mismatch,
- rotation does not create key ID flip-flop,
- Status becomes unhealthy when background probes go stale.

## Source References

- [Kubernetes KMS provider documentation](https://kubernetes.io/docs/tasks/administer-cluster/kms-provider/)
- [Kubernetes KMS v2 Go package](https://pkg.go.dev/k8s.io/kms/apis/v2)
