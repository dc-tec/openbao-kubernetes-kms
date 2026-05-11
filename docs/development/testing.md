---
title: "Testing"
description: "Layered testing strategy for bao-kms-provider: unit, conformance, OpenBao integration, Kubernetes E2E, rotation, failure injection, performance, security, and disaster recovery layers."
weight: 30
---

# Testing

A KMS plugin failure can prevent the API server from starting or make encrypted Kubernetes resources unreadable. The testing strategy treats negative paths, rotation, and recovery as first-class, not as additions to a happy-path suite.

For the E2E framework specifics see [E2E Framework](/development/e2e-framework/). For the v0.1 release gates that consume these tests see [Release Gates](/development/release-gates/).

## Test Priorities

| Priority | Why it matters |
|---|---|
| KMS v2 protocol correctness | If this is wrong, Kubernetes rejects encrypt responses, marks the plugin unhealthy, or fails decrypts. |
| `key_id` invariants | Rotation and decrypt safety depend on stable, non-reused, understood `key_id` values. |
| Decrypt compatibility | Old encrypted data must remain readable after restart, upgrade, and rotation. |
| OpenBao Transit behavior | The design relies on explicit `key_version`, AAD, metadata, `min_decryption_version`, and `disable_upsert`. |
| API server startup behavior | Kubernetes may perform thousands of decrypt calls on startup and recommends decrypt latency under 10 ms. |
| Runtime deployment | systemd and static-pod behavior differ materially. |
| Disaster recovery | Loss of Transit key state or configuration mismatch can make data unrecoverable. |
| Observability and redaction | The component handles sensitive data and must not leak plaintext, tokens, or full ciphertext. |

Negative tests are more important than the happy path. Encrypt and decrypt working once is easy. The hard part is proving the plugin fails safely when OpenBao is sealed, the JWT expires, annotations are malformed, the key version changes mid-flight, the socket is stale, or an old `key_id` appears after rotation.

## Test Layers

### Layer 1: Unit Tests

Fast tests that run on every PR.

Focus areas: `internal/keyregistry`, `internal/aad`, `internal/kmsv2`, `internal/auth/jwt`, `internal/socket`, `internal/config`, `internal/logging`.

Important groups:

- **keyregistry**: same inputs produce the same `key_id`; different identity-bearing inputs produce different `key_id`; previous `key_id` is never reused after rollback; active snapshot does not flip-flop; pending snapshot requires stable observation count; version rollback is rejected outside DR mode; old snapshots remain lookupable for decrypt.
- **aad**: canonical AAD serialization is stable; annotation order does not affect reconstructed AAD; missing required annotations fail; provider, cluster, mount, and key hashes mismatch fails; AAD never includes raw key name, mount path, JWT, token, or plaintext.
- **kmsv2**: Status returns cached active `key_id`; Status does not call Transit; Encrypt uses active snapshot; Encrypt returns Status `key_id`; Encrypt fails when no active snapshot; Decrypt rejects unknown, malformed, missing-annotation, and AAD-mismatch cases; Decrypt never tries fallback across keys.
- **auth/jwt**: missing or unreadable JWT files fail closed; expired JWT fails closed; JWT below minimum remaining TTL fails closed; JWT file is re-read before re-login; OpenBao token is stored only in memory; token refresh schedules before expiry; wrong issuer, audience, or subject is detected where locally checkable.
- **socket**: creates socket with configured mode and group; rejects unsafe parent directory; rejects symlink at socket path; rejects regular file at socket path; removes only verified-dead Unix sockets; does not use abstract sockets by default.
- **config**: safe configuration passes; world-writable configuration fails; missing identity-bearing fields fail; AAD enabled without required scope fails; dangerous timeout values fail; invalid socket path fails.
- **logging**: plaintext, JWT, OpenBao token, full ciphertext, raw Transit paths, and raw key names never appear in logs or command output.

### Layer 2: KMS v2 Protocol Conformance

A dedicated test suite that starts the plugin with fake OpenBao and Transit and connects to the Unix socket using the real Kubernetes KMS v2 protobuf client.

Core invariant:

```text
for i in 1..N:
  statusKeyID := Status().key_id
  enc := Encrypt(randomPlaintext)
  assert enc.key_id == statusKeyID
```

Other conformance cases:

- Status returns `version=v2`, `healthz=ok`, non-empty `key_id`,
- Encrypt returns valid ciphertext, the Status `key_id`, and valid annotations,
- Decrypt accepts encrypt output round-trip,
- Decrypt rejects unknown `key_id`, malformed `key_id`, missing or modified annotations, and ciphertext from a different provider name or cluster ID,
- Status remains cheap under repeated polling,
- Status becomes unhealthy after staleness and recovers after a successful background probe.

### Layer 3: OpenBao Client Integration

Hermetic tests against in-process HTTPS fakes that model OpenBao Transit response shapes, TLS behavior, error bodies, and policy capability responses. They do not require external OpenBao credentials.

Important cases:

- JWT login succeeds with the configured role,
- JWT login fails with the wrong audience or subject,
- the token can read Transit key metadata, encrypt, and decrypt,
- the token cannot rotate, export, back up, or delete keys,
- explicit `key_version` is sent on encrypt,
- `associated_data` decrypt succeeds with matching AAD and fails with changed AAD,
- batch decrypt preserves per-item `associated_data` and reference values,
- `min_encryption_version` blocks old encrypt versions,
- `min_decryption_version` blocks old decrypt versions,
- `disable_upsert` prevents accidental key creation,
- `sys/capabilities-self` results parse for `doctor` policy diagnostics.

Exact-version OpenBao validation belongs in the e2e lane (Layer 4).

### Layer 4: Kubernetes API Server E2E

Two e2e tracks:

| Track | Purpose |
|---|---|
| Kind-based | Fast CI feedback, API server integration, encryption configuration behavior. |
| kubeadm VM-based | Realistic static-pod, host filesystem, systemd, socket, and boot behavior. |

Kind suites cover the encrypt and decrypt path, etcd ciphertext verification, restart behavior, and Transit rotation observation. kubeadm VM suites cover host boot ordering, plugin availability before API server, stale socket cases, control-plane node replacement, plugin package upgrade, and API server restart decrypt storms.

For static-pod mode, tests verify the manifest does not rely on ServiceAccount, ConfigMap, or Secret references because Kubernetes static pods cannot refer to API objects.

### Layer 5: Rotation Tests

Rotation deserves its own suite. Cases include:

- initial Transit version 1 produces a stable `key_id`,
- rotating to version 2 does not activate immediately,
- after the stable observation count plus activation delay, Status reports the new `key_id` and Encrypt uses the new Transit version,
- old ciphertext continues to decrypt with the old `key_id`,
- new ciphertext decrypts with the new `key_id`.

Negative cases:

- Transit latest version briefly appears as 2 then reverts to 1: the plugin does not flip-flop,
- the plugin restarts during pending rotation: the active key decision remains stable,
- OpenBao metadata read fails during rotation: the plugin does not promote,
- Transit key recreated with the same name: lineage mismatch fails closed,
- `min_decryption_version` raised too early: old decrypt fails and `doctor` reports the destructive condition,
- Status `key_id` changes but Encrypt still returns the old value: conformance fails.

### Layer 6: Failure Injection

An explicit failure-mode test matrix covering the full catalog in [Architecture: Failure Modes](/architecture/failure-modes/). Automated where possible; some systemd and static-pod cases live in nightly VM tests rather than every PR.

Minimum coverage:

- OpenBao down, sealed, leader failover,
- TLS certificate expired,
- DNS failure,
- JWT expired or rotated,
- Transit key missing, soft-deleted, or recreated,
- AAD mismatch, unknown `key_id`, key flip-flop prevention,
- API server startup decrypt storm,
- stale socket, wrong socket group, broken SELinux or AppArmor policy,
- plugin crash loop, static-pod image missing.

### Layer 7: Performance And Load

Performance targets reflect Kubernetes behavior:

- Encrypt: aim under 100 ms,
- Decrypt: aim under 10 ms,
- Startup may involve thousands of decrypt requests,
- Status is polled continually and must be optimized.

Performance test cases:

- single encrypt and decrypt latency p50/p95/p99,
- concurrent decrypt latency,
- API server startup decrypt storm simulation,
- one-hour Status polling soak,
- OpenBao HA failover during load,
- token renewal and JWT re-login during load,
- Transit metadata probe during load.

Initial SLOs:

```yaml
performanceTargets:
  status:
    p99: 5ms
    externalOpenBaoCalls: 0
  encrypt:
    p95: 100ms
    p99: 250ms
  decrypt:
    p95: 10ms
    p99: 50ms
  startupStorm:
    noDeadlock: true
    boundedMemory: true
    boundedGoroutines: true
```

p99 values may be adjusted based on OpenBao and network reality. The test surfaces the trade-off explicitly.

Decrypt micro-batching remains disabled and configuration-rejected for v0.1 unless sustained direct decrypt soak and the local-only Harvester kubeadm decrypt-warmup workload show a release-blocking need for a production-grade KMS coalescer. The direct-path soak prepares a fixed encrypted sample corpus, sustains concurrent decrypts through the real provider/OpenBao path, and enforces error, p95-latency, memory-growth, and goroutine/PID-growth bounds. The Harvester workload creates or reuses a larger corpus of real Kubernetes Secrets, restarts kube-apiserver, then repeatedly lists the full Secret corpus through kube-apiserver so Kubernetes drives the encrypted Secret read path and cold KMS decrypt warmup. A separate Harvester cold-start command reuses the same corpus, restarts kube-apiserver, performs one full list through every selected API server, and records provider decrypt counter deltas to separate Kubernetes object-read load from real KMS decrypt RPC load. Captured evidence is recorded in [Benchmark Results](/development/benchmark-results/).

### Layer 8: Security Tests

Automated:

- `gosec`,
- `govulncheck`,
- `staticcheck`,
- the race detector,
- fuzz tests for annotations and `key_id` parsing,
- dependency license scan,
- container image vulnerability scan,
- SBOM generation,
- signature and provenance verification.

Security-specific functional tests:

- logs do not contain plaintext, JWTs, or OpenBao tokens,
- metrics do not expose raw `key_id` values, OpenBao paths, or key names,
- `doctor` does not print secrets,
- panic output redacts sensitive configuration,
- debug endpoints disabled by default,
- admin endpoint bound only to localhost when enabled.

Fuzz targets:

- `DecryptRequest` annotations,
- `key_id` parser,
- AAD envelope parser,
- OpenBao error parser,
- `EncryptionConfiguration` parser,
- JWT parser wrapper,
- configuration loader.

A fuzz target shape:

```text
Given arbitrary annotations and key_id:
  Decrypt either rejects safely
  or produces exactly the expected AAD for a known valid object.
It never panics.
It never calls Transit for an unknown key_id.
```

### Layer 9: Disaster Recovery

DR tests:

- restore OpenBao backup and read existing Kubernetes Secret,
- restore etcd backup with matching, too-new, and too-old OpenBao state,
- plugin configuration lost and restored,
- plugin configuration changed incorrectly,
- JWT issuer unavailable,
- control-plane node replaced,
- single-node and multi-node control-plane recovery.

Critical destructive tests run only in isolated environments:

- delete the Transit key and prove data is unrecoverable without backup,
- raise `min_decryption_version` too early and prove old data fails,
- recreate the Transit key with the same name and prove old data fails,
- change the provider name and prove decrypt and AAD behavior,
- change the cluster ID and prove AAD behavior.

These tests validate the plugin and the runbooks together; see [Operations: Disaster Recovery](/operations/disaster-recovery/).

## Suggested CI Structure

| Tier | Scope |
|---|---|
| Every PR | Layer 1 unit, Layer 2 conformance, configuration validation, redaction tests, key_id and AAD golden tests, gofmt/go vet/staticcheck, race tests where feasible, fuzz smoke tests. Target under 10 minutes. |
| Main branch and nightly | Layer 3 hermetic OpenBao integration, Kind e2e (Layer 4), rotation (Layer 5), failure injection (Layer 6), performance smoke (Layer 7), image scan, SBOM. |
| Release candidate | Exact-pinned Kubernetes and OpenBao matrix, kubeadm VM tests, OpenBao HA failover, DR tests (Layer 9), startup decrypt storm (Layer 7), upgrade and downgrade tests. |

## Version Matrix

```yaml
kubernetes:
  required:
    - "1.34.3"
  futureCandidates:
    - "1.34.7"
    - "1.35.x"
    - "1.36.x"
openbao:
  required:
    - "2.5.3"
deployment:
  - kind
  - kubeadm-systemd
  - kubeadm-static-pod
auth:
  - jwt-local-public-key
  - jwt-jwks
  - jwt-oidc-discovery
transit:
  - aes256-gcm96
  - xchacha20-poly1305 optional
  - aad-enabled
  - decrypt-microbatching-disabled-default
  - decrypt-microbatching-future-benchmark
```

The exact Kubernetes patch version and Kind node image digest live in `.ci/versions.yaml`. Additional Kubernetes or OpenBao versions remain future candidates until exact-pinned release-gate lanes exist; see [Reference: Compatibility](/reference/compatibility/).

## Golden Fixtures

The implementation maintains golden fixtures for:

- `KeySnapshot` to `key_id`,
- `KeySnapshot` and annotations to AAD,
- Transit metadata to parsed key profile,
- `EncryptionConfiguration` to parsed provider configuration,
- JWT claims to validation result,
- OpenBao errors to error class.

Fixture layout:

```text
testdata/
├── keyid/
│   ├── snapshot-v1.yaml
│   └── expected-keyid.txt
├── aad/
│   ├── annotations-v1.yaml
│   └── expected-aad-base64.txt
├── transit/
│   ├── key-metadata-v1.json
│   ├── key-metadata-rotated.json
│   └── key-metadata-dangerous-min-decrypt.json
├── encryptionconfig/
│   ├── valid-kms-v2.yaml
│   ├── identity-fallback.yaml
│   └── wrong-provider-name.yaml
└── jwt/
    ├── valid.claims.json
    ├── expired.claims.json
    └── wrong-audience.claims.json
```

Golden tests catch accidental changes to `key_id` or AAD derivation. Those are wire-format compatibility guarantees; see [Reference: Compatibility: Compatibility Promises](/reference/compatibility/#compatibility-promises).
