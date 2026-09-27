---
title: Testing
description: "What the test suite must prove, the test layers that prove it, the install regression check, and how to run longer fuzz campaigns."
eyebrow: Contribute
weight: 30
verifiedBy:
  - mk/checks.mk
  - mk/deployment.mk
  - test/kmsconformance
  - test/deployment/systemd-install.sh
---

A provider failure can stop the API server from starting or make cluster data
unreadable, so tests favor negative paths, rotation, and recovery over a single
happy-path round trip. For the runnable lanes, see
[E2E framework](/contribute/e2e-framework/); for CI stages and release
evidence, see [CI and supply chain](/contribute/ci-supply-chain/).

## What tests must prove

| Priority | Proof |
|---|---|
| KMS v2 correctness | Kubernetes accepts Status, Encrypt, and Decrypt, and `Status.key_id == EncryptResponse.key_id` always holds. |
| `key_id` stability | `key_id` values are deterministic, never reused, and never roll back. |
| Decrypt compatibility | Data stays readable across restarts, upgrades, rollbacks, and rotations. |
| Fail-closed behavior | OpenBao, auth, socket, policy, and key failures never expose plaintext or fall back. |
| Startup and deployment | The provider is ready for API server restarts, with the distinct boot and socket behavior of systemd and static pods. |
| Recovery | Restores keep data readable only with a matching backup pair. |
| Redaction | Logs, metrics, reports, and artifacts never contain plaintext, JWTs, tokens, full ciphertext, or key material. |

Every negative test asserts both the error behavior and the absence of
sensitive values in logs, metrics, and artifacts. Negative cases cover OpenBao
outages, failover, and old restores, expired, rotated, or mismatched
credentials, missing, recreated, or restricted keys, unknown or rolled-back
`key_id` values, tampered or foreign AAD, unsafe sockets, and host failures
during restarts.

## Test layers

| Layer | Covers | Where |
|---|---|---|
| Unit and golden | Registry decisions, AAD, configuration, sockets, redaction, wire-format fixtures | `go test ./...`, `testdata/` |
| KMS v2 conformance | The real socket server with fake OpenBao and KMS v2 protobuf requests | `test/kmsconformance`, every pull request |
| OpenBao integration | Transit, auth, TLS, policy diagnostics, error handling | Hermetic integration tests and the OpenBao lane |
| CLI | Diagnostics and hardening failures against real OpenBao | Provider CLI lane |
| API server | Real encryption, raw etcd envelopes, restarts, multi-node convergence | Kind lanes and kubeadm VM validation |
| Rotation and compatibility | Promotion, old readback, retirement, missing state, rollback rejection | Rotation and upgrade lanes |
| Failure injection | Outages, sealing, failover, revoked tokens, bad policy or auth, missing keys, stale sockets | Failure and HA lanes |
| Performance | Status, Encrypt, and Decrypt latency, soak, startup decrypt, resource growth | Load lanes; targets in [KMS v2 contract](/docs/reference/kms-v2-contract/#validation-thresholds) |
| Security and supply chain | Redaction, fuzzing, static analysis, vulnerability and license scans, SBOM, vendor verification | `make ci-core`, security CI, release workflow |
| Recovery | Raft restore, state rehydration, etcd pairing, readback after replacement | Kind DR, restore lane, VM validation |

The Kind smoke lane uses the generated static-pod manifest. It retires the JWT
signing key, revokes provider tokens, checks authentication failure, and then
atomically replaces the host JWT. Recovery must decrypt the existing KMS
sample and encrypt new data without restarting the provider. The lane also
restarts the API server and reads the existing encrypted Secret.

## Install regression check

`make systemd-install-check` builds a systemd bundle and runs the shell blocks
from [Run with systemd](/docs/get-started/systemd/) in a disposable Linux
container, using the pinned Go builder image with Debian systemd tools. It
checks service-user file access, socket group separation, unit syntax, and that
re-running the install keeps configuration and state. Building the test image
needs network access; the check itself does not.

The Deployment Samples CI job runs it when installation docs, packaging, the
bundle builder, or deployment tests change. It does not start systemd, log in
to OpenBao, or test Kubernetes boot; the VM and E2E lanes do.

`make static-pod-install-check` runs the host preparation commands from the
static-pod kit's README, then generates files for both JWT sources with the
bundled binary. It checks UID/GID `65532` access, socket-group isolation,
matching identity on a second node, and preservation on reinstall. It does
not start kubelet or claim cluster activation. The Deployment Samples job
runs this check too.

Set `IMAGE_PLATFORM=linux/amd64` or `linux/arm64` to choose the Linux test
architecture. Docker needs native support or emulation for that platform.
To test an existing static-pod kit, set `BUNDLE_ARCHIVE` to its path relative
to the repository root. The container runs without network access.

## Fuzzing

`make ci-core` runs short fuzz smoke campaigns (`FUZZTIME=10s`). To run longer
campaigns locally:

```sh
FUZZTIME=1m make fuzz
go test ./internal/keyregistry -run '^$' -fuzz '^FuzzStateFileDecode$' -fuzztime=5m
go test ./internal/aad -run '^$' -fuzz '^FuzzPrepareDecrypt$' -fuzztime=5m
```
