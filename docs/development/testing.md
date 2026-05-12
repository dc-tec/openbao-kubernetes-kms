---
title: "Testing"
description: "Testing strategy for bao-kms-provider: protocol correctness, fail-closed behavior, rotation, recovery, performance, and release evidence."
weight: 30
---

# Testing

A KMS plugin failure can prevent the API server from starting or make encrypted
Kubernetes resources unreadable. The test strategy therefore prioritizes
negative paths, rotation, and recovery over a single happy-path encrypt and
decrypt check.

This page explains what the test system must prove. For runnable E2E lanes and
Make targets, see [E2E Framework](/development/e2e-framework/). For CI lanes
and release evidence requirements, see [CI And Supply Chain](/development/ci-supply-chain/).
For captured load and cold-start evidence, see [Performance Evidence](/development/benchmark-results/).

## Test Priorities

| Priority | What must be proven |
|---|---|
| KMS v2 protocol correctness | Kubernetes accepts `Status`, `Encrypt`, and `Decrypt` responses and the provider preserves the `Status.key_id == EncryptResponse.key_id` invariant. |
| `key_id` stability | Rotation, rollback rejection, and decrypt lookup depend on deterministic, non-reused `key_id` values. |
| Decrypt compatibility | Data written before restart, upgrade, rollback, and Transit rotation remains readable. |
| Fail-closed behavior | OpenBao, JWT, socket, policy, and Transit key failures do not lead to plaintext exposure or unsafe fallback. |
| API server startup | The provider is available early enough for kube-apiserver restart and recovery paths. |
| Deployment behavior | systemd and static-pod modes have different boot, socket, filesystem, and rollback failure modes. |
| Disaster recovery | OpenBao, provider state, and etcd restore procedures preserve decryptability only when the correct backup pair is restored. |
| Observability and redaction | Logs, metrics, reports, and diagnostics never expose plaintext, JWTs, OpenBao tokens, full ciphertext, or raw Transit key material. |

## Test Layers

| Layer | Purpose | Canonical location |
|---|---|---|
| Unit and golden tests | Validate key registry decisions, AAD construction, config validation, socket handling, logging redaction, and stable wire-format fixtures. | `go test ./...`, golden fixtures under `testdata/` |
| KMS v2 conformance | Start the real Unix-socket server path with fake OpenBao behavior and exercise Kubernetes KMS v2 protobuf requests. | fast local and PR checks |
| OpenBao client integration | Verify Transit, JWT auth, TLS, policy diagnostics, and OpenBao error handling without external credentials. | hermetic integration tests and OpenBao CI E2E |
| Kubernetes API server E2E | Prove real API server encryption, raw etcd envelope storage, restart readback, and multi-control-plane convergence. | Kind lanes and local kubeadm VM validation |
| Rotation and compatibility | Prove Transit version promotion, old ciphertext readback, new ciphertext write path, historical decryptability guards, missing-state fail-closed behavior, and rollback rejection. | OpenBao rotation E2E |
| Failure injection | Exercise OpenBao outage, sealed state, failover, bad policy, expired or identity-drifted JWT, missing Transit key, stale socket, and startup failures. | provider failure and HA E2E lanes |
| Performance and load | Bound Status, Encrypt, Decrypt, direct decrypt soak, startup decrypt, and resource growth behavior. | provider load lanes and performance evidence |
| Security and supply chain | Run redaction checks, fuzz targets, static analysis, vulnerability scan, license check, SBOM, and vendor verification. | `make ci-core`, security CI, release workflow |
| Disaster recovery | Validate OpenBao raft restore, provider state rehydration, etcd restore pairing, and Kubernetes readback after replacement. | Kind DR, OpenBao restore, and local VM validation |

## Negative Path Bias

Encrypt and decrypt working once is not enough. The test suite must prove that
the provider fails safely when:

- OpenBao is down, sealed, unavailable during failover, or restored from an old backend,
- the JWT expires, rotates, has the wrong claims, or cannot be read,
- the Transit key is missing, soft-deleted, recreated under the same name, or has unsafe version bounds,
- `key_id` values are unknown, malformed, rolled back, or associated with changed identity scope,
- AAD annotations are missing, modified, or from another provider or cluster,
- the Unix socket is stale, has the wrong ownership, or points at an unsafe filesystem object,
- systemd, kubelet, static pods, images, and host paths fail during API server restart or node recovery.

Every negative test should assert both the returned error behavior and the
absence of sensitive values in logs, metrics, and artifacts.

## Performance Model

Performance targets reflect Kubernetes behavior and the provider's role in API
server startup:

| Path | Initial target | Reason |
|---|---:|---|
| `Status` | p99 under 5 ms and no OpenBao call | kube-apiserver polls Status frequently. |
| `Encrypt` | p95 under 100 ms, p99 under 250 ms | writes call OpenBao Transit and must tolerate network reality. |
| `Decrypt` | p95 under 10 ms, p99 under 50 ms | API server startup can perform many decrypts. |
| Startup storm | bounded memory and goroutines | recovery paths must not deadlock or leak resources. |

These are test targets, not published service-level guarantees. The release
evidence can adjust thresholds when OpenBao or network behavior justifies it,
but the trade-off must remain explicit.

Decrypt micro-batching remains disabled and configuration-rejected.
The current direct-path evidence did not show provider or OpenBao decrypt fan-out
proportional to Kubernetes object count. A production coalescer should only move
into the release scope if sustained direct decrypt soak or local kubeadm VM
cold-start evidence shows a release-blocking need.

## Release Evidence

Release evidence is assembled from the test layers above and the supply-chain
controls documented in [CI And Supply Chain](/development/ci-supply-chain/).
In summary:

- every PR should prove deterministic logic, conformance, redaction, formatting,
  static analysis, vendor integrity, and fast parser fuzz smoke;
- main and nightly lanes should add OpenBao, Kind, rotation, failure injection,
  restore, load, and supply-chain checks;
- release candidates should add exact-pinned version matrices, local kubeadm VM
  validation, recovery evidence, and release artifact evidence.

The exact Kubernetes patch version, Kind node image, OpenBao image, and tool
versions live in `.ci/versions.yaml`. Additional Kubernetes or OpenBao versions
remain future candidates until exact-pinned lanes and release evidence exist. See
[Reference: Compatibility](/reference/compatibility/) for the support boundary.
