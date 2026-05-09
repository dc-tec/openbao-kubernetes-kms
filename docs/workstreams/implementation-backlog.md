# Implementation Backlog

This is the canonical implementation backlog for the OpenBao Kubernetes KMS provider.

Status values:

- `planned`: not started.
- `in-progress`: actively being worked.
- `blocked`: cannot proceed until dependency or decision is resolved.
- `done`: merged and verified.

Priority values:

- `P0`: required for v0.1 engineering preview.
- `P1`: required before production-ready claims.
- `P2`: useful follow-up or hardening.

## Cross-Workstream Definition Of Done

Every implementation task must satisfy the relevant subset of:

- Code is covered by unit or integration tests.
- Errors are classified and redacted.
- Logs do not contain plaintext, JWTs, OpenBao tokens, or full ciphertext.
- Metrics avoid raw key IDs, raw OpenBao paths, raw key names, and unbounded labels.
- Production code follows [Code quality](../code-quality.md).
- Production code does not use `map[string]any`, `map[string]interface{}`, or broad `any` outside reviewed boundary adapters.
- Config changes update [Configuration](../configuration.md).
- KMS behavior changes update [KMS v2 contract](../kms-v2-contract.md).
- Key ID, annotation, or AAD changes update [Key ID and AAD](../key-id-and-aad.md) and ADRs.
- Operator behavior changes update deployment or runbook docs.
- Backward compatibility impact is documented in [Compatibility](../compatibility.md).
- CI, E2E, and release work uses exact-pinned versions from the central version policy.

## WS00: Repository And Project Foundation

Goal: create a maintainable project skeleton that can support production-quality implementation and tests.

| ID | Priority | Status | Task | Dependencies |
|---|---|---|---|---|
| WS00-T01 | P0 | done | Initialize Go module `github.com/dc-tec/openbao-kubernetes-kms` and `bao-kms-provider` root command. | None |
| WS00-T02 | P0 | done | Create internal package layout. | WS00-T01 |
| WS00-T03 | P0 | done | Add build, version, and commit metadata. | WS00-T01 |
| WS00-T04 | P0 | done | Add local developer commands through Makefile. | WS00-T01 |
| WS00-T05 | P0 | done | Add baseline CI for format, vet, unit tests, and race smoke. | WS00-T04 |
| WS00-T06 | P0 | done | Add dependency update policy and module hygiene checks. | WS00-T05 |
| WS00-T07 | P1 | done | Add release artifact naming and checksums. | WS00-T03 |
| WS00-T08 | P1 | done | Add cross-compilation targets for Linux architectures. | WS00-T07 |
| WS00-T09 | P0 | done | Add central pinned version policy for OpenBao, Kubernetes, Kind node image, and release-gate rows. | WS00-T04 |
| WS00-T10 | P0 | done | Validate strict Go quality gates for gofumpt, staticcheck, govulncheck, `.golangci.yml`, ast-grep structural rules, and Semgrep security/API-misuse rules. | WS00-T05 |
| WS00-T11 | P0 | done | Add typed-boundary policy tests or lint exceptions for any unavoidable `any` usage. | WS00-T10 |
| WS00-T12 | P1 | done | Add release-please release PR automation and changelog ownership. | WS00-T07 |

Acceptance criteria:

- `go test ./...` passes from a clean checkout.
- `bao-kms-provider --help` prints Viper-backed command usage.
- Version output includes semantic version, commit, build date, and dirty state.
- CI fails on unformatted code.
- CI does not use floating `latest` inputs for validated versions.
- ast-grep rejects `map[string]any`, `map[string]interface{}`, and broad `any` in production packages.
- gofumpt, staticcheck, govulncheck, golangci-lint, ast-grep, and Semgrep run in the core quality gate once the module exists.
- release-please owns version proposals and `CHANGELOG.md` while publishing remains a separate gated workflow.

Implementation notes:

- Use Go `1.26.3`.
- Use Viper for CLI configuration loading and command environment binding.
- Keep Viper out of runtime packages after typed config construction.
- Prefer typed structs and strict decoders over dynamic maps.
- Prefer small internal packages with explicit interfaces.
- Do not introduce OpenBao or Kubernetes integration complexity before key ID/AAD golden tests exist.

## WS01: Configuration And Validation

Goal: implement strict config loading, identity-bearing field handling, and local file permission validation.

| ID | Priority | Status | Task | Dependencies |
|---|---|---|---|---|
| WS01-T01 | P0 | done | Define config structs matching [Configuration](../configuration.md). | WS00-T01 |
| WS01-T02 | P0 | done | Implement YAML config loader with duration parsing. | WS01-T01 |
| WS01-T03 | P0 | done | Implement required field validation. | WS01-T02 |
| WS01-T04 | P0 | done | Implement identity-bearing field validation and config fingerprinting. | WS01-T03 |
| WS01-T05 | P0 | done | Validate socket path, mode, group, and parent directory policy. | WS01-T03 |
| WS01-T06 | P0 | done | Validate config file, JWT file, and CA bundle permissions. | WS01-T03 |
| WS01-T07 | P0 | done | Implement Kubernetes `EncryptionConfiguration` parser for `doctor`. | WS01-T02 |
| WS01-T08 | P0 | done | Validate provider name, endpoint, API version, and identity fallback state. | WS01-T07 |
| WS01-T09 | P1 | done | Add config schema export for documentation and tooling. | WS01-T01 |
| WS01-T10 | P1 | done | Add config compatibility tests for older config versions. | WS01-T01 |

Acceptance criteria:

- Unsafe config fails closed with actionable redacted errors.
- Missing identity-bearing fields fail startup.
- `doctor` detects provider name mismatch with `EncryptionConfiguration`.
- Unit tests cover unsafe file permissions, invalid durations, bad socket paths, missing fields, and unsupported compatibility modes.

Test requirements:

- Table-driven validation tests.
- Golden valid config fixture.
- Golden invalid config fixtures.
- Permission tests on Unix platforms.

## WS02: Key Registry, Key ID, Annotations, And AAD

Goal: implement the compatibility-sensitive metadata layer before any real KMS traffic exists.

| ID | Priority | Status | Task | Dependencies |
|---|---|---|---|---|
| WS02-T01 | P0 | done | Define `KeySnapshot` model. | WS01-T01 |
| WS02-T02 | P0 | done | Implement opaque Kubernetes key ID derivation. | WS02-T01 |
| WS02-T03 | P0 | done | Add key ID golden fixtures. | WS02-T02 |
| WS02-T04 | P0 | done | Implement annotation builder and parser. | WS02-T01 |
| WS02-T05 | P0 | done | Implement canonical AAD builder. | WS02-T04 |
| WS02-T06 | P0 | done | Add AAD golden fixtures. | WS02-T05 |
| WS02-T07 | P0 | done | Implement key registry lookup for active and historical snapshots. | WS02-T01 |
| WS02-T08 | P0 | done | Enforce decrypt validation order. | WS02-T04, WS02-T07 |
| WS02-T09 | P0 | done | Add fuzz tests for key ID and annotations. | WS02-T02, WS02-T04 |
| WS02-T10 | P0 | done | Enforce AAD required mode for v0.1. | WS02-T05 |
| WS02-T11 | P0 | done | Implement hybrid local key registry state file for observed/promoted snapshots. | WS02-T07, WS06-T03 |
| WS02-T12 | P0 | done | Add registry permissions, corruption handling, and rebuild-from-metadata behavior. | WS02-T11 |
| WS02-T13 | P0 | done | Add rollback/replay detection for registry state. | WS02-T12 |
| WS02-T14 | P1 | done | Add future compatibility-mode tests for annotation/AAD schema versions. | WS02-T10 |

Acceptance criteria:

- Same snapshot produces same key ID across process restarts.
- Any identity-bearing field change changes key ID.
- Different Transit version changes key ID.
- Unknown key IDs are rejected before Transit calls.
- AAD output is byte-for-byte stable from golden fixtures.
- Annotation order does not affect reconstructed AAD.
- Malformed annotations never panic.
- Registry state is non-secret and restart-safe.
- Missing registry state can be diagnosed and rebuilt from config plus Transit metadata when safe.

Test requirements:

- Property-style tests for key ID uniqueness over identity fields.
- Golden tests for key ID and AAD.
- Fuzz tests for annotation parser and key ID parser.
- Tests proving no raw OpenBao paths or key names appear in annotations.

## WS03: OpenBao Transit Client

Goal: provide a minimal, testable OpenBao client for Transit metadata, encrypt, decrypt, and diagnostic checks.

| ID | Priority | Status | Task | Dependencies |
|---|---|---|---|---|
| WS03-T01 | P0 | done | Define OpenBao client interfaces. | WS00-T02 |
| WS03-T02 | P0 | done | Implement TLS configuration with CA and server name validation. | WS01-T01 |
| WS03-T03 | P0 | done | Implement Transit key metadata read. | WS03-T01 |
| WS03-T04 | P0 | done | Parse Transit key metadata into internal key profile. | WS03-T03 |
| WS03-T05 | P0 | done | Implement Transit encrypt with explicit `key_version`. | WS03-T01, WS02-T05 |
| WS03-T06 | P0 | done | Implement Transit decrypt with required v0.1 AAD and future compatibility hooks. | WS03-T01, WS02-T05 |
| WS03-T07 | P0 | done | Implement OpenBao error classification. | WS03-T03 |
| WS03-T08 | P0 | done | Ensure HTTP client redacts request/response logging. | WS03-T01 |
| WS03-T09 | P0 | done | Implement probe encrypt/decrypt with non-secret random data. | WS03-T05, WS03-T06 |
| WS03-T10 | P0 | done | Implement `disable_upsert` read for `doctor`. | WS03-T03 |
| WS03-T11 | P1 | done | Implement capability checks for policy diagnostics. | WS03-T01 |
| WS03-T12 | P0 | done | Add decrypt micro-batching support behind disabled-by-default config. | WS03-T06 |
| WS03-T13 | P2 | done | Add namespace support tests. | WS03-T01 |

Acceptance criteria:

- Encrypt always sends explicit `key_version`.
- AAD mismatch fails in hermetic OpenBao client integration tests.
- Metadata parser detects exportable, plaintext backup, deletion allowed, derived, convergent, and version restrictions.
- Errors are mapped to stable classes without leaking tokens or payloads.
- TLS server name mismatch fails.
- Batched decrypt preserves per-item AAD, key ID validation, cancellation, and error mapping semantics.

Test requirements:

- Unit tests with fake HTTP server.
- Hermetic integration tests with in-process OpenBao-compatible HTTPS fakes.
- Redaction tests for request failures.

Implementation notes:

- `internal/openbao` owns the typed Transit HTTP client, TLS construction, error classes, key profile parsing, encrypt/decrypt, batch decrypt, capability checks, `disable_upsert`, and probe behavior.
- Unit tests cover explicit key versions, required AAD, namespace headers, redacted errors, metadata findings, and batch decrypt request/response shape.
- A build-tagged integration test exercises the Transit shape without external OpenBao credentials; ephemeral OpenBao validation is isolated under the e2e test tag.

## WS04: JWT Authentication And Token Lifecycle

Goal: authenticate to OpenBao through JWT and maintain an in-memory OpenBao token safely.

| ID | Priority | Status | Task | Dependencies |
|---|---|---|---|---|
| WS04-T01 | P0 | done | Implement JWT file reader. | WS01-T06 |
| WS04-T02 | P0 | done | Parse JWT claims locally for expiry and diagnostics. | WS04-T01 |
| WS04-T03 | P0 | done | Validate JWT minimum remaining TTL before login. | WS04-T02 |
| WS04-T04 | P0 | done | Implement OpenBao JWT login. | WS03-T01 |
| WS04-T05 | P0 | done | Store OpenBao token only in memory. | WS04-T04 |
| WS04-T06 | P0 | done | Implement token renewal where allowed. | WS04-T05 |
| WS04-T07 | P0 | done | Implement re-login before token expiry. | WS04-T05 |
| WS04-T08 | P0 | done | Re-read JWT file before re-login. | WS04-T07 |
| WS04-T09 | P0 | done | Expose auth state for Status cache and readiness. | WS04-T05 |
| WS04-T10 | P0 | done | Add redaction tests for JWT and OpenBao token. | WS04-T01 |
| WS04-T11 | P1 | done | Add support for non-renewable tokens. | WS04-T07 |
| WS04-T12 | P1 | done | Add clock skew diagnostics. | WS04-T02 |
| WS04-T13 | P2 | planned | Add certificate auth as non-default alternative. | WS03-T02 |

Acceptance criteria:

- Missing, unreadable, expired, or near-expiry JWT fails closed.
- Wrong role or policy failure is classified.
- JWT is re-read before re-login.
- OpenBao token never touches disk.
- Logs and command output never include JWT or OpenBao token.
- JWT `nbf`, `iat`, and `exp` handling respects configured clock skew leeway.
- Auth refreshes are coalesced and do not hold the manager state lock during OpenBao I/O.

Test requirements:

- JWT fixture tests.
- Fake OpenBao auth server tests.
- Integration tests for successful and failed JWT login.
- Token lifecycle tests with fake clocks.

## WS05: KMS v2 Protocol Server

Goal: implement Kubernetes KMS v2 gRPC behavior exactly enough for kube-apiserver interoperability.

| ID | Priority | Status | Task | Dependencies |
|---|---|---|---|---|
| WS05-T01 | P0 | done | Add Kubernetes KMS v2 protobuf dependency. | WS00-T01 |
| WS05-T02 | P0 | done | Implement gRPC server skeleton. | WS05-T01 |
| WS05-T03 | P0 | done | Implement `Status` from cached status. | WS06-T02 |
| WS05-T04 | P0 | done | Implement `Encrypt`. | WS02-T07, WS03-T05 |
| WS05-T05 | P0 | done | Implement `Decrypt`. | WS02-T08, WS03-T06 |
| WS05-T06 | P0 | done | Enforce Status/encrypt key ID invariant. | WS05-T03, WS05-T04 |
| WS05-T07 | P0 | done | Propagate request UID only to safe logs/traces. | WS09-T01 |
| WS05-T08 | P0 | done | Add timeout and context cancellation handling. | WS03-T01 |
| WS05-T09 | P0 | done | Add panic recovery with redacted errors. | WS05-T02 |
| WS05-T10 | P1 | done | Add graceful shutdown behavior. | WS07-T04 |

Acceptance criteria:

- Real KMS v2 client can call Status, Encrypt, and Decrypt over Unix socket.
- Status does not call Transit.
- Encrypt returns cached Status key ID.
- Decrypt rejects unknown key ID before Transit.
- Decrypt rejects malformed annotations and AAD mismatch.
- Full plaintext/ciphertext never appears in logs.

Test requirements:

- Fake Transit conformance tests.
- Concurrent request tests.
- Timeout/cancellation tests.
- Startup with no active snapshot fails closed.

Implementation notes:

- `internal/kmsv2` owns the protocol server and depends on narrow `StatusCache` and `Transit` interfaces.
- WS06 still owns the production background status cache implementation behind the `StatusCache` interface.
- WS05-T10 is covered by a runtime-backed KMS RPC drain test: the WS07 runtime uses gRPC graceful shutdown while KMS v2 handlers propagate request contexts into cached Status and Transit operations.
- WS07 owns production Unix socket lifecycle and process shutdown primitives.
- Request UIDs are propagated only as hashes in structured KMS request logs, never as raw log fields or metric labels.

## WS06: Status Cache, Health, And Rotation Watcher

Goal: keep Kubernetes Status cheap while background probes maintain OpenBao and key state.

| ID | Priority | Status | Task | Dependencies |
|---|---|---|---|---|
| WS06-T01 | P0 | done | Implement background probe scheduler. | WS03-T03, WS04-T09 |
| WS06-T02 | P0 | done | Implement Status cache with staleness policy. | WS06-T01 |
| WS06-T03 | P0 | done | Implement rotation observation state machine. | WS02-T07, WS03-T04 |
| WS06-T04 | P0 | done | Enforce stable observation count. | WS06-T03 |
| WS06-T05 | P0 | done | Enforce activation delay. | WS06-T03 |
| WS06-T06 | P0 | done | Reject Transit version rollback by default. | WS06-T03 |
| WS06-T07 | P0 | done | Add local multi-node consistency diagnostic surface. | WS06-T02, WS06-T08 |
| WS06-T08 | P0 | done | Integrate persisted registry state into rotation promotion and restart behavior. | WS02-T12 |
| WS06-T09 | P1 | done | Add circuit breaker for repeated OpenBao failures. | WS03-T07 |
| WS06-T10 | P1 | planned | Add OpenBao HA failover behavior tests. | WS11-T07 |

Acceptance criteria:

- Healthy Status stays cheap under repeated polling.
- Status becomes unhealthy after stale cache threshold.
- Rotation v1 to v2 promotes once.
- Brief version rollback does not flip Status from old to new to old.
- Plugin restart during pending rotation behaves deterministically.

Test requirements:

- Fake clock tests.
- Rotation state machine unit tests.
- Failure injection around metadata read errors.
- Concurrency tests for active snapshot updates.

Implementation notes:

- `internal/status` owns the production cache behind the `kmsv2.StatusCache` interface and also implements key snapshot lookup for decrypt.
- Background probes refresh auth, read Transit metadata, update persisted registry state, and publish health without logging sensitive request material.
- Pending Transit versions are persisted with observation count and activation timing, but pending and rejected snapshots are not exposed for decrypt lookup.
- Local diagnostics expose redacted active/pending key hashes, Transit versions, state generation, rotation state, cache staleness, and circuit breaker state for later WS09 metrics/logging.
- WS06-T10 remains planned until WS11 failure-injection and OpenBao HA test support lands.

## WS07: Socket And Runtime Service Behavior

Goal: safely expose the local Unix socket and behave predictably under service restarts.

| ID | Priority | Status | Task | Dependencies |
|---|---|---|---|---|
| WS07-T01 | P0 | done | Implement socket parent directory validation. | WS01-T05 |
| WS07-T02 | P0 | done | Implement safe Unix socket creation. | WS07-T01 |
| WS07-T03 | P0 | done | Implement stale socket detection and safe cleanup. | WS07-T02 |
| WS07-T04 | P0 | done | Implement signal handling and graceful shutdown. | WS05-T02 |
| WS07-T05 | P0 | done | Implement `/live` endpoint. | WS05-T02 |
| WS07-T06 | P0 | done | Implement `/ready` endpoint. | WS06-T02 |
| WS07-T07 | P0 | done | Reject symlink and regular-file socket paths. | WS07-T01 |
| WS07-T08 | P1 | planned | Add SELinux/AppArmor deployment notes after testing. | WS10-T01 |
| WS07-T09 | P1 | planned | Add service restart behavior tests. | WS10-T01 |

Acceptance criteria:

- Unsafe socket path fails closed.
- Live socket is not removed.
- Verified dead socket can be removed.
- API server group can connect when configured.
- `/live` and `/ready` return distinct process/readiness states.

Test requirements:

- Unix permission tests.
- Symlink and regular-file tests.
- Live socket collision tests.
- Graceful shutdown tests.

Implementation notes:

- `internal/socket` owns the typed `Listen` primitive, including parent-directory checks, symlink/regular-file/directory rejection, live-peer probe via bounded `net.DialTimeout`, verified-dead stale socket reclamation, and post-bind chmod/chown. `serve` wires `OnStaleSocketRemoved` to `openbao_kms_socket_restarts_total` without coupling the socket package to metrics.
- `internal/health` owns the `/live` and `/ready` HTTP handler behind narrow `LivenessProbe` and `ReadinessProbe` interfaces; `/ready` consumes `status.Diagnostics` and flips 503 on unhealthy, stale, or no-active-snapshot states. The handler rejects non-GET/HEAD methods and unknown paths.
- `internal/runtime` composes the socket listener, gRPC server, optional health HTTP server, optional metrics HTTP server, signal handling (`SIGINT`/`SIGTERM`), and bounded graceful shutdown that falls through `GracefulStop` to `Stop` past `ShutdownTimeout`. The runtime owns listener lifecycle while `serve` supplies health and metrics handlers.
- WS08-T01 (`serve`) is responsible for translating typed config into `runtime.Options` and wiring CLI flags, structured logging, Prometheus metrics, and the socket cleanup hook.
- WS07-T08 and WS07-T09 remain planned because both depend on WS10-T01 deployment artifacts.

## WS08: CLI Tooling

Goal: provide operational commands required by the design.

| ID | Priority | Status | Task | Dependencies |
|---|---|---|---|---|
| WS08-T01 | P0 | done | Implement `serve`. | WS05-T02, WS07-T02 |
| WS08-T02 | P0 | done | Implement `doctor` framework. | WS01-T03 |
| WS08-T03 | P0 | done | Add `doctor` OpenBao TLS/auth checks. | WS03-T02, WS04-T04 |
| WS08-T04 | P0 | done | Add `doctor` Transit key and policy checks. | WS03-T04, WS03-T10 |
| WS08-T05 | P0 | done | Add `doctor` socket checks. | WS07-T01 |
| WS08-T06 | P0 | done | Add `doctor` EncryptionConfiguration checks. | WS01-T08 |
| WS08-T07 | P0 | done | Implement `verify-key`. | WS03-T04 |
| WS08-T08 | P0 | done | Implement `benchmark` smoke mode. | WS03-T09 |
| WS08-T09 | P0 | done | Implement `rotation-plan`. | WS06-T03 |
| WS08-T10 | P0 | done | Implement `verify-rotation` initial confidence report. | WS06-T03 |
| WS08-T11 | P1 | planned | Add stable JSON output for automation. | WS08-T02 |
| WS08-T12 | P1 | planned | Add shell completion generation. | WS08-T01 |

Acceptance criteria:

- `doctor` catches bad socket, bad JWT, bad policy, and dangerous Transit key config.
- `verify-key` detects exportable keys, plaintext backup, deletion allowed, and version restriction hazards.
- `benchmark` output is redacted.
- CLI exits with stable codes for automation.

Test requirements:

- CLI golden output tests.
- Redaction tests.
- Fake OpenBao diagnostics tests.
- Integration tests for `doctor` against real OpenBao.

Implementation notes:

- `cmd/bao-kms-provider` now wires `serve`, `doctor`, `verify-key`, `benchmark`, `rotation-plan`, and `verify-rotation` through typed config loading and stable command exit codes.
- `serve` composes JWT auth, the OpenBao Transit client adapter, status store/controller/scheduler, KMS v2 gRPC, Unix socket runtime, and optional HTTP health readiness.
- `doctor` emits redacted text checks for local config/JWT/socket state, OpenBao TLS and JWT auth, Transit capabilities, key metadata, `disable_upsert`, key profile hazards, non-secret probe encrypt/decrypt, deterministic key ID derivation, and the Status/encrypt key ID invariant. `--encryption-config` validates Kubernetes KMS v2 provider identity and reports identity fallback as a warning.
- `verify-key` reuses Transit metadata/profile checks and validates local registry state against `min_available_version`, `min_encryption_version`, and `min_decryption_version` when state exists.
- `benchmark` is an intentionally narrow redacted smoke benchmark for Transit encrypt/decrypt latency. Full decrypt-storm and micro-batching comparisons remain release-gate performance work.
- `rotation-plan` and `verify-rotation` report local registry/Transit rotation state using key ID hashes only. `verify-rotation` is explicitly a limited confidence report until later Kubernetes or etcd inspection support exists.

## WS09: Observability And Redaction

Goal: make the provider diagnosable without leaking sensitive material.

| ID | Priority | Status | Task | Dependencies |
|---|---|---|---|---|
| WS09-T01 | P0 | done | Add structured JSON logger. | WS00-T02 |
| WS09-T02 | P0 | done | Implement redaction helpers. | WS09-T01 |
| WS09-T03 | P0 | done | Add Prometheus metrics registry. | WS05-T02 |
| WS09-T04 | P0 | done | Add KMS request metrics. | WS05-T04, WS05-T05 |
| WS09-T05 | P0 | done | Add OpenBao request metrics. | WS03-T01 |
| WS09-T06 | P0 | done | Add auth/token metrics. | WS04-T09 |
| WS09-T07 | P0 | done | Add status cache and rotation metrics. | WS06-T02 |
| WS09-T08 | P0 | done | Add AAD/key ID validation metrics. | WS02-T08 |
| WS09-T09 | P0 | done | Add log/metric redaction tests. | WS09-T02 |
| WS09-T10 | P1 | done | Add example alert rules. | WS09-T03 |
| WS09-T11 | P1 | done | Add debug correlation mode with strict guardrails. | WS03-T07 |

Acceptance criteria:

- Logs are JSON.
- Metrics match [Observability](../observability.md).
- No plaintext, JWT, token, full ciphertext, raw key name, or raw mount path appears in normal logs.
- Metrics use bounded labels.

Test requirements:

- Redaction unit tests.
- Integration test log capture.
- Metrics label cardinality tests.

Implementation notes:

- `internal/logging` owns the `slog`-backed JSON/text logger, stable log field names, and redaction placeholder helpers. Runtime success-path KMS and OpenBao request events log at debug level; failures log at warning level.
- `internal/metrics` owns an isolated Prometheus registry and `/metrics` handler using pinned `github.com/prometheus/client_golang`.
- KMS v2, OpenBao, auth, and status packages expose narrow observer interfaces so metrics/logging remain composed from `serve` without coupling protocol or OpenBao clients to concrete observability packages.
- Status and auth gauges are collected from redacted snapshots. Metrics never expose raw key IDs, raw OpenBao paths, raw key names, request UIDs, plaintext, JWTs, OpenBao tokens, or full ciphertext.
- WS09 includes example Prometheus alerting rules under `docs/operations/prometheus-alerts.yaml`.
- Debug correlation mode is guarded by config validation: it is disabled by default, requires debug logging, requires an incident ID, requires OpenBao request ID logging, and expires automatically after a bounded TTL.

## WS10: Deployment Artifacts And Packaging

Goal: provide installable artifacts and tested deployment examples.

| ID | Priority | Status | Task | Dependencies |
|---|---|---|---|---|
| WS10-T01 | P0 | done | Add sample systemd unit. | WS07-T02 |
| WS10-T02 | P0 | done | Add sample static pod manifest. | WS07-T02 |
| WS10-T03 | P0 | done | Add sample plugin config. | WS01-T01 |
| WS10-T04 | P0 | done | Add sample Kubernetes `EncryptionConfiguration`. | WS01-T07 |
| WS10-T05 | P0 | done | Add container image build. | WS00-T07 |
| WS10-T06 | P1 | done | Add package install layout for Linux using the resolved identity model. | WS00-T07 |
| WS10-T07 | P1 | done | Add kubeadm lab deployment scripts. | WS11-T06 |
| WS10-T08 | P1 | done | Add upgrade and rollback scripts for lab use. | WS10-T06 |
| WS10-T09 | P2 | done | Add OpenTofu module skeleton. | WS03-T10 |
| WS10-T10 | P2 | done | Add OpenBao policy generator. | WS08-T02 |

Acceptance criteria:

- systemd and static pod samples match docs.
- Static pod manifest does not reference ConfigMaps, Secrets, or ServiceAccounts.
- Container image runs as non-root where feasible.
- Install layout matches [Install](../install.md).

Implementation notes:

- Deployment samples live under `deploy/`.
- The container image uses a pinned distroless non-root runtime base and runs as `65532:65532`.
- `server.socketGroup` accepts a group name or decimal GID; static pod samples use a numeric GID to avoid host group-name dependencies inside the image.
- Linux package snippets use separate `openbao-kms` and `openbao-kms-socket` groups per [ADR 0012](../adr/0012-deployment-identity-and-image.md).

Test requirements:

- Manifest linting.
- Container smoke test.
- systemd unit syntax check where available.
- Static pod hostPath validation in e2e.

## WS11: Test Infrastructure And Validation

Goal: build confidence through layered tests before production claims.

| ID | Priority | Status | Task | Dependencies |
|---|---|---|---|---|
| WS11-T01 | P0 | done | Create testdata layout. | WS00-T02 |
| WS11-T01A | P0 | done | Establish root Ginkgo E2E suite, suite manifest, report targets, and ephemeral OpenBao CI lane. | WS03-T05 |
| WS11-T02 | P0 | done | Implement fake Transit. | WS03-T01 |
| WS11-T03 | P0 | done | Implement fake auth/token manager. | WS04-T05 |
| WS11-T04 | P0 | done | Build KMS v2 fake conformance suite. | WS05-T02 |
| WS11-T05 | P0 | done | Add OpenBao `2.5.3` CI e2e environment. | WS03-T05 |
| WS11-T05A | P0 | done | Extend OpenBao CI environment with JWT auth role bootstrap after WS04 lands. | WS04-T04, WS11-T05 |
| WS11-T05B | P0 | done | Add containerized provider full-stack OpenBao/KMS v2 socket e2e without a Kubernetes API server. | WS05-T02, WS07-T01, WS10-T05, WS11-T05A |
| WS11-T06 | P0 | done | Add minimal pinned Kubernetes `1.34.3` Kind e2e. | WS10-T02, WS00-T09 |
| WS11-T06B | P0 | done | Add public-CI portable Kind multi-control-plane convergence e2e. | WS11-T06 |
| WS11-T06C | P0 | done | Add public-CI portable Kind static-pod upgrade/rollback e2e. | WS11-T06 |
| WS11-T06D | P0 | done | Add portable provider binary upgrade/rollback e2e with distinct images. | WS10-T05, WS11-T05B |
| WS11-T07 | P0 | done | Add failure injection tests. | WS06-T02 |
| WS11-T08 | P0 | done | Add rotation tests. | WS06-T03 |
| WS11-T09 | P0 | done | Add decrypt storm performance smoke test. | WS05-T05 |
| WS11-T10 | P1 | planned | Add kubeadm VM e2e. | WS10-T01, WS10-T02 |
| WS11-T10A | P0 | done | Add systemd and static-pod install script staging checks. | WS10-T07 |
| WS11-T11 | P1 | planned | Add OpenBao HA failover tests. | WS03-T01 |
| WS11-T11A | P0 | done | Add portable provider backend replacement e2e against OpenBao integrated raft storage. | WS11-T05B |
| WS11-T12 | P1 | planned | Add DR restore tests. | WS10-T07 |
| WS11-T12A | P0 | done | Add containerized OpenBao integrated raft snapshot restore e2e. | WS11-T11A |
| WS11-T13 | P0 | planned | Add exact-pinned Kubernetes `1.34.3` release-gate matrix row. | WS11-T06 |
| WS11-T14 | P0 | planned | Add exact-pinned OpenBao `2.5.3` release-gate matrix row. | WS11-T05 |
| WS11-T15 | P1 | planned | Add long-running Status polling test. | WS06-T02 |
| WS11-T16 | P1 | planned | Add future-candidate Kubernetes lanes only after exact versions are approved. | WS11-T13 |

Acceptance criteria:

- v0.1 release gates pass.
- Every PR runs fast tests.
- Nightly runs OpenBao `2.5.3` CI e2e and pinned Kubernetes `1.34.3` Kind e2e.
- Release candidate runs exact-pinned matrix and DR tests.
- E2E lanes are declared in `test/e2e/suites.yaml` and report as Ginkgo JSON plus JUnit.

Test requirements:

- See [Testing strategy](../testing-strategy.md).
- See [Release gates](../release-gates.md).
- See [E2E framework](../e2e-framework.md).

Implementation notes:

- `test/fakes` owns reusable KMS v2 fake Transit, fake Status cache, and fake OpenBao auth/token clients for cross-package validation.
- `test/kmsconformance` starts the real KMS v2 gRPC service over a filesystem Unix socket and exercises it through the Kubernetes KMS v2 protobuf client.
- The KMS conformance suite verifies cached Status behavior, Status no-Transit-call behavior, `EncryptResponse.key_id == Status.key_id`, decrypt of encrypt output, and invalid decrypt rejection before Transit.
- The OpenBao CI e2e environment bootstraps Transit, `disable_upsert`, a least-privilege provider policy, JWT auth with a generated RS256 test issuer, and a role exercised by the real auth manager.
- `test/e2e` has a containerized full-stack test that builds/runs the provider image against real OpenBao and validates KMS v2 Status, Encrypt, Decrypt, unknown-key rejection, and annotation tamper rejection from a second container over the shared Unix socket volume.
- `test/e2e` has provider/OpenBao failure-mode tests for OpenBao down, OpenBao sealed, reduced provider policy, expired JWT startup fail-closed behavior, JWT file rotation and re-login, missing Transit key startup fail-closed behavior, Status staleness, and stale socket reclamation.
- `test/e2e` has a provider/OpenBao decrypt storm smoke test that performs concurrent KMS v2 Decrypt calls through the real provider image against real OpenBao.
- `test/e2e` has a provider/OpenBao rotation lane that runs OpenBao with integrated raft storage, writes ciphertext on the initial Transit version, saves a pre-rotation raft snapshot, rotates the Transit key, waits for provider Status to promote a new `key_id`, verifies old and new ciphertext decrypt, restores the pre-rotation snapshot, and verifies provider fail-closed behavior after the observed Transit version rollback.
- `test/e2e` has a provider binary upgrade/rollback lane that builds distinct old/new provider images, verifies their version metadata differs, encrypts through the old image, upgrades the same state volume to the new image, verifies old and new ciphertext readback, rolls back to the old image, and verifies both ciphertexts remain decryptable.
- `test/e2e` has a pinned Kind smoke lane for Kubernetes `1.34.3` that deploys the provider as a static pod, enables kube-apiserver KMS v2 encryption, verifies Secret create/read, verifies raw etcd storage uses the `k8s:enc:kms:v2:` envelope without plaintext, restarts kube-apiserver, and reads the Secret again.
- `test/e2e` has a pinned Kind multi-control-plane convergence lane that runs three control-plane nodes, stages the provider on each node, verifies each stacked etcd member stores the Secret with a KMS v2 envelope, proves each kube-apiserver can decrypt while it is the only serving API endpoint, and then restarts every kube-apiserver with readback.
- `test/e2e` has a provider/OpenBao backend replacement and restore lane that runs OpenBao with integrated raft storage, verifies fail-closed behavior while the backend is down, restarts the backend under the same Docker network name, saves a raft snapshot, restores it into a fresh storage volume, and decrypts ciphertext created before the outage or restore.
- `test/e2e` has a pinned Kind static-pod upgrade/rollback lane that mutates the provider static pod manifest, waits for kubelet restart, verifies old and new Secret readback, restores the previous manifest, and verifies readback after provider and kube-apiserver restart.
- `test/deployment` stages the systemd and static-pod lab install scripts into temporary roots and verifies expected files, directories, modes, and setgid socket directory behavior without requiring host root privileges.

## WS12: Security Hardening And Supply Chain

Goal: reduce operational and supply-chain risk before broader use.

| ID | Priority | Status | Task | Dependencies |
|---|---|---|---|---|
| WS12-T01 | P0 | planned | Add static security checks. | WS00-T05 |
| WS12-T02 | P0 | planned | Add SBOM generation. | WS10-T05 |
| WS12-T03 | P0 | planned | Add vulnerability scanning. | WS10-T05 |
| WS12-T04 | P0 | planned | Add dependency license check. | WS00-T06 |
| WS12-T05 | P0 | planned | Add artifact signing. | WS00-T07 |
| WS12-T06 | P0 | planned | Add provenance generation and verification. | WS12-T05 |
| WS12-T07 | P1 | planned | Perform security review of key ID/AAD. | WS02-T06 |
| WS12-T08 | P1 | planned | Perform security review of socket handling. | WS07-T03 |
| WS12-T09 | P1 | planned | Perform security review of JWT/token lifecycle. | WS04-T10 |
| WS12-T10 | P1 | planned | Perform threat model review with implementation evidence. | WS11-T07 |
| WS12-T11 | P1 | planned | Add fuzzing to CI or scheduled job. | WS02-T09 |
| WS12-T12 | P0 | planned | Add release byte reproducibility check. | WS00-T08, WS12-T02 |
| WS12-T13 | P0 | planned | Add release evidence pack and provenance index. | WS12-T06 |
| WS12-T14 | P0 | planned | Pin GitHub Actions and release tools by immutable version or commit. | WS00-T05 |
| WS12-T15 | P1 | planned | Add reproducible build hardening beyond release gate. | WS12-T12 |

Acceptance criteria:

- Security checks run in CI.
- Release artifacts include SBOM.
- Release artifacts are signed and attested.
- Release images and SBOMs pass byte reproducibility checks.
- Redaction tests are blocking.
- Security review findings are tracked before production-ready claims.

## WS13: Documentation, Examples, And Release Process

Goal: keep documentation accurate as implementation decisions become concrete.

| ID | Priority | Status | Task | Dependencies |
|---|---|---|---|---|
| WS13-T01 | P0 | planned | Update docs with actual command flags after CLI implementation. | WS08-T01 |
| WS13-T02 | P0 | planned | Add runnable OpenBao setup example. | WS03-T10 |
| WS13-T03 | P0 | planned | Add v0.1 release notes and known limitations. | WS11-T06 |
| WS13-T04 | P0 | planned | Add issue templates using workstream IDs. | WS00-T05 |
| WS13-T05 | P1 | planned | Add production readiness guide after full gates pass. | WS11-T12 |
| WS13-T06 | P1 | planned | Add upgrade guide. | WS10-T08 |
| WS13-T07 | P1 | planned | Add rollback guide. | WS10-T08 |
| WS13-T08 | P1 | planned | Add distro-specific deployment guides after testing. | WS11-T10 |
| WS13-T09 | P2 | planned | Add architecture diagrams generated from source. | WS05-T02 |
| WS13-T10 | P0 | planned | Add CI and supply-chain implementation docs after workflows exist. | WS12-T13 |

Acceptance criteria:

- Docs match implemented flags, config, metrics, and manifests.
- v0.1 docs clearly state engineering preview.
- Compatibility claims match tested matrix.
- Release notes call out migration and wire-format risks.

## Dependency Map

Critical path for v0.1:

```text
WS00 foundation and pinned version policy
  -> WS01 config
  -> WS02 key ID/AAD
  -> WS05 fake KMS conformance
  -> WS03 OpenBao client + WS04 JWT auth
  -> WS06 status/rotation
  -> WS07 runtime/socket
  -> WS08 CLI
  -> WS10 deployment
  -> WS11 e2e/release gates
```

Parallelizable early work:

- WS09 observability can start once WS05 service skeleton exists.
- WS12 supply-chain checks can start once image build exists, but version pinning starts in WS00.
- WS13 documentation updates can happen throughout.
- WS10 sample artifacts can begin from docs but must be verified after runtime code exists.

## Resolved Design Decisions

| ID | Decision | Document |
|---|---|---|
| RD-01 | Historical key IDs are derived from config plus Transit metadata, with a non-secret local registry for observed/promoted snapshots and rotation decisions. | [ADR 0006](../adr/0006-hybrid-key-registry.md) |
| RD-02 | Initial OpenBao validation target is `2.5.3`. | [ADR 0007](../adr/0007-pinned-ci-version-matrix.md) |
| RD-03 | Initial Kubernetes validation target is the `1.34` release line; the Kind lane pins `1.34.3` by image digest while tracking upstream `1.34.7` as latest patch. | [ADR 0007](../adr/0007-pinned-ci-version-matrix.md) |
| RD-04 | AAD is enabled and required for v0.1. | [ADR 0008](../adr/0008-aad-required-for-mvp.md) |
| RD-05 | Decrypt micro-batching is included in v0.1 behind disabled-by-default config. | [ADR 0009](../adr/0009-include-decrypt-microbatching.md) |
| RD-06 | Project name, binary name, Go version, Viper, Makefile, and version policy path are fixed for M0. | [ADR 0010](../adr/0010-project-naming-and-m0-foundation.md) |
| RD-07 | Production Go uses typed models and forbids `map[string]any`, `map[string]interface{}`, and broad `any` outside reviewed boundary adapters. | [ADR 0011](../adr/0011-strict-typed-idiomatic-go.md) |

## Open Questions To Resolve During Implementation

| ID | Question | Needed by | Owner |
|---|---|---|---|
| OQ-01 | Resolved: use `openbao-kms`, `openbao-kms`, and `openbao-kms-socket` with numeric GID support for static pods. | WS10-T06 | [ADR 0012](../adr/0012-deployment-identity-and-image.md) |
| OQ-02 | Resolved: upstream latest is `1.34.7`; the initial Kind lane pins `kindest/node:v1.34.3@sha256:08497ee19eace7b4b5348db5c6a1591d7752b164530a36f855cb0f2bdcbadd48` because newer official Kind `1.34` node images are unavailable. | WS11-T06 | [ADR 0007](../adr/0007-pinned-ci-version-matrix.md) |

## v0.1 Blocking Checklist

This checklist duplicates the release gates in implementation terms:

- WS02 key ID/AAD golden tests pass.
- WS05 KMS v2 fake conformance suite passes.
- WS03/WS04 hermetic OpenBao client integration suite passes.
- WS11 ephemeral OpenBao `2.5.3` CI e2e suite passes.
- WS11 pinned Kubernetes `1.34.3` Kind e2e proves Secret encryption/decryption.
- API server restart with encrypted Secret works.
- Status key ID equals encrypt response key ID.
- Unknown key ID decrypt is rejected before Transit call.
- AAD mismatch decrypt is rejected.
- AAD is required for v0.1 objects.
- Transit encrypt uses explicit `key_version`.
- Decrypt storm smoke coverage passes; decrypt micro-batching remains disabled by default until benchmarking justifies enabling it.
- Rotation v1 to v2 works without key ID flip-flop.
- Old ciphertext remains decryptable after rotation.
- JWT expiry/re-login path works.
- OpenBao outage fails closed.
- `doctor` catches bad socket, bad JWT, bad policy, and bad Transit key config.
- Logs and metrics redaction tests pass.
- Static pod manifest does not rely on API objects.
- systemd unit ordering is validated in kubeadm-style test.
- Supply-chain release gates produce SBOM, signatures, attestations, reproducibility evidence, and provenance index.
