# MVP Board

This board gives the recommended implementation sequence for v0.1 engineering preview. The detailed tasks live in [Implementation backlog](implementation-backlog.md).

## Milestone M0: Repository Foundation

Goal: create a compilable project skeleton with CI-quality local checks.

Blocking tasks:

- WS00-T01 Initialize Go module and command skeleton.
- WS00-T02 Create internal package layout.
- WS00-T03 Add build/version metadata.
- WS00-T04 Add local developer commands.
- WS00-T09 Add central pinned version policy.
- WS00-T10 Add strict Go quality gates.
- WS00-T12 Add release-please release PR automation.
- WS11-T01 Add test harness layout.

Exit criteria:

- `go test ./...` passes.
- Empty `bao-kms-provider --help` works with Viper-backed config binding.
- CI can run format, vet, unit tests.
- CI does not use floating `latest` inputs.
- CI rejects forbidden dynamic Go type patterns in production code.
- Static analysis tooling is pinned or routed through the central tool policy before release gates.
- release-please owns version proposals and `CHANGELOG.md` without publishing releases.

## Milestone M1: Key And AAD Contract

Goal: lock the compatibility-sensitive local crypto metadata behavior before networking code.

Blocking tasks:

- WS02-T01 Define `KeySnapshot`.
- WS02-T02 Implement key ID derivation.
- WS02-T03 Add key ID golden fixtures.
- WS02-T04 Implement annotation builder/parser.
- WS02-T05 Implement canonical AAD builder.
- WS02-T06 Add AAD golden fixtures.
- WS02-T07 Implement key registry lookup for active and historical snapshots.
- WS02-T08 Enforce decrypt validation order.
- WS02-T09 Add fuzz tests for key ID and annotations.
- WS02-T10 Enforce AAD required mode.
- WS02-T11 Implement local key registry state file.
- WS02-T12 Add registry state recovery checks.
- WS02-T13 Add rollback/replay detection.
- WS02-T14 Add compatibility-mode rejection tests.

Exit criteria:

- Golden fixtures prove deterministic key ID and AAD output.
- Malformed annotations never panic.
- Unknown key IDs never proceed to Transit in tests.
- Registry state reloads active and historical snapshots after restart.
- Missing registry state can be rebuilt from safe metadata.
- Replayed or rolled-back registry state is rejected.

## Milestone M2: KMS v2 Fake Conformance

Goal: serve the Kubernetes KMS v2 API against fake Transit and prove protocol invariants.

Blocking tasks:

- WS05-T01 Add Kubernetes KMS v2 protobuf dependency.
- WS05-T02 Implement KMS v2 service skeleton.
- WS05-T03 Implement fake Transit-backed encrypt/decrypt.
- WS05-T04 Enforce Status/encrypt key ID consistency.
- WS05-T05 Enforce decrypt validation order.
- WS11-T04 Build KMS v2 conformance suite.

Exit criteria:

- Fake conformance suite passes.
- Status calls do not call fake Transit.
- Encrypt returns Status key ID.
- Decrypt rejects unknown key ID before fake Transit call.

## Milestone M3: Real OpenBao Integration

Goal: prove Transit, JWT auth, TLS, metadata parsing, and explicit key version behavior against real OpenBao.

Blocking tasks:

- WS03-T01 Implement OpenBao HTTP client.
- WS03-T02 Implement Transit metadata parsing.
- WS03-T03 Implement Transit encrypt with explicit key version.
- WS03-T04 Implement Transit decrypt with AAD.
- WS04-T01 Implement JWT file reader.
- WS04-T02 Implement JWT login.
- WS04-T03 Implement token lifecycle.
- WS11-T05 Extend OpenBao `2.5.3` CI e2e environment with JWT auth bootstrap.

Current status:

- WS03 client work is implemented in `internal/openbao`.
- WS04 JWT file handling, local claim validation with clock skew leeway, JWT login, in-memory token lifecycle, renewal, re-login, refresh coalescing, and retry backoff are implemented in `internal/auth` and `internal/openbao`.
- The root Ginkgo E2E suite and ephemeral OpenBao CI lane are in place.
- Remaining M3 blocker is OpenBao CI JWT auth bootstrap.

Exit criteria:

- Hermetic OpenBao client integration tests pass.
- E2E tests pass against OpenBao `2.5.3`.
- Encrypt sends explicit `key_version`.
- AAD mismatch fails.
- Policy-denied and missing-key errors are classified.

## Milestone M4: Status, Rotation, Runtime

Goal: make the provider operationally viable on a control-plane node.

Blocking tasks:

- WS06-T01 Implement background probe loop.
- WS06-T02 Implement Status cache staleness.
- WS06-T03 Implement rotation observation state machine.
- WS07-T01 Implement safe Unix socket creation.
- WS07-T02 Implement stale socket handling.
- WS09-T01 Add structured logs.
- WS09-T03 Add Prometheus metrics.

Exit criteria:

- Status remains cheap under polling.
- Status becomes unhealthy when probes go stale.
- Rotation v1 to v2 promotes once and does not flip-flop.
- Socket safety tests pass.

## Milestone M5: CLI And Deployment Preview

Goal: give operators enough tooling to install and diagnose the provider in lab clusters.

Blocking tasks:

- WS08-T01 Implement `serve`.
- WS08-T02 Implement `doctor`.
- WS08-T03 Implement `verify-key`.
- WS08-T04 Implement `benchmark` smoke mode.
- WS10-T01 Add sample systemd unit.
- WS10-T02 Add sample static pod manifest.
- WS11-T06 Add kind e2e.

Exit criteria:

- `doctor` catches bad JWT, bad socket, bad policy, and dangerous Transit key settings.
- pinned Kubernetes `1.34.x` kind e2e proves Secret create/read/restart/read.
- Deployment samples match docs.

## Milestone M6: v0.1 Engineering Preview

Goal: satisfy [Release gates](../release-gates.md) for an engineering preview.

Blocking tasks:

- All M0-M5 exit criteria.
- WS11-T07 Add failure injection tests.
- WS11-T08 Add rotation tests.
- WS12-T01 Add static security checks.
- WS12-T02 Add SBOM generation.
- WS12-T05 Add artifact signing and provenance.
- WS12-T12 Add byte reproducibility check.
- WS12-T13 Add release evidence pack and provenance index.
- WS13-T03 Finalize v0.1 release notes and known limitations.

Exit criteria:

- v0.1 release gate checklist passes.
- Documentation says engineering preview, not production-ready.
