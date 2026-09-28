---
title: KMS v2 contract
description: "What the API server can rely on from the provider's Status, Encrypt, and Decrypt calls, the protocol and size limits, and the conformance cases that prove it."
eyebrow: Reference · Contract
weight: 50
verifiedBy:
  - internal/kmsv2/server.go
  - internal/kmsv2/limits.go
  - internal/openbao/client.go
  - test/kmsconformance
---

The provider implements Kubernetes KMS v2, stable since Kubernetes 1.29, over
gRPC on a filesystem Unix socket (`/run/openbao-kms/kms.sock` by default). KMS
v1 is not implemented. It rejects unsafe socket paths, symlinks, regular files
at the socket path, and unsafe parent directories, and removes a stale socket
only after confirming no live listener owns it.

## Status

Status returns the plugin API version, the health state, and the active
`key_id`, always from cached state and never with a live Transit call.

- Status turns healthy only after both a metadata probe and an
  encrypt-and-decrypt deep probe succeed, and neither success clears the
  other's failure.
- It is unhealthy when `disable_upsert=true` cannot be verified or the cache is
  older than `status.statusMaxStaleness`.
- After a failed deep probe, the provider retries it following the next
  successful metadata probe, within its circuit breaker, without waiting for
  `status.deepProbeInterval`.
- `key_id` changes only when the rotation state machine promotes a new
  snapshot.

An observation-only persistence failure can keep Status healthy when fresh
metadata and the last successful deep probe still validate the published keys,
and both state files remain unchanged. The provider reports this condition
through readiness diagnostics and a metric. It publishes no new identity or
promotion until persistence succeeds. Other save failures remain unhealthy.
See [Persistence failures](/docs/architecture/rotation-model/#persistence-failures).

The provider always keeps this invariant, because Kubernetes discards any
encrypt response that breaks it and marks the provider unhealthy:

```text
EncryptResponse.key_id == most_recent_healthy_Status.key_id
```

## Encrypt

Encrypt takes plaintext and a request UID, and returns Transit ciphertext, the
active `key_id`, and annotations. Each call uses exactly one active snapshot
and passes an explicit Transit `key_version`, so a rotation between the call
and a later metadata read cannot mislabel the ciphertext.

Encrypt fails closed when no active snapshot exists or OpenBao or auth is
unavailable. It never creates or rotates a Transit key, relies on the implicit
latest version, falls back to plaintext, or returns a stale `key_id`.

## Decrypt

Decrypt takes ciphertext, the `key_id`, annotations, and a request UID, and
returns plaintext only after the local checks in
[Key ID and AAD: Decrypt validation order](/docs/reference/key-id-and-aad/#decrypt-validation-order)
pass. It never tries other keys or versions, and it never decrypts without AAD.

A well-formed unknown `key_id` triggers shared metadata discovery, bounded by
`openbao.timeout` and limited to once per `status.probeInterval`. Each caller
waits with its own request deadline; canceling one caller does not cancel
shared discovery. Provider shutdown cancels discovery. Discovery never
advances promotion. A failed lookup returns `Unavailable` with
`key_metadata_refresh_failed`; a successful lookup that does not find the
identity returns `NotFound`. Pending snapshots decrypt once validated but never
encrypt.

## Limits

| Limit | Value |
|---|---|
| `ciphertext` | non-empty, under 1024 bytes |
| `key_id` | non-empty, under 1024 bytes |
| Annotations | keys plus values under 32768 bytes, valid UTF-8, fully qualified domain-name keys |
| gRPC messages | 65536 bytes in either direction |
| Active handlers | 16 Status, 32 Encrypt, 64 Decrypt by default; configurable from 1 to 1024; excess requests fail with `ResourceExhausted` |
| OpenBao response bodies | 64 KiB for errors, 4 MiB for key metadata and batch decrypt, 256 KiB otherwise |

Oversized decrypt requests are rejected before Transit is called. Encrypt fails
closed if a Transit response would break the KMS v2 limits, and the deep probe
checks the ciphertext size and key version of a real round trip so response
drift shows up as a readiness failure. Oversized OpenBao responses fail as
`openbao_unavailable`, detected by reading one byte past the limit rather than
trusting `Content-Length`.

A Transit `403` on the recovery retry returns `PermissionDenied`. After the
token is discarded, requests during the recovery delay return `Unauthenticated`
without contacting Transit. Encryption can also return `FailedPrecondition`
once a background probe marks the provider unhealthy.

Errors returned to Kubernetes carry a stable class and never contain secrets,
plaintext, full ciphertext, or raw paths; see
[Observability: Error classes](/docs/reference/observability/#error-classes).

The provider does not micro-batch decrypt calls into Transit `batch_input`.
The direct path has met the validation thresholds, and batching would add
queueing, deadline, ordering, and fan-out concerns.

## Validation thresholds

Tests and examples use these latency targets. They are not production SLOs;
tune alerts to your OpenBao deployment and network path.

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

## Conformance

The conformance suite drives the real KMS v2 protobuf client against the Unix
socket. It blocks a release unless:

- a healthy Status returns a non-empty `key_id`, and repeated Status calls
  never reach OpenBao,
- Encrypt returns the Status `key_id` within every size limit, and Decrypt
  accepts its output,
- oversized fields and messages, unresolved unknown `key_id` values, malformed
  annotations, and AAD mismatches are rejected before Transit,
- rotation never flips `key_id` back,
- Status turns unhealthy when background probes go stale.
