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
| WS03-T01 | P0 | planned | Define OpenBao client interfaces. | WS00-T02 |
| WS03-T02 | P0 | planned | Implement TLS configuration with CA and server name validation. | WS01-T01 |
| WS03-T03 | P0 | planned | Implement Transit key metadata read. | WS03-T01 |
| WS03-T04 | P0 | planned | Parse Transit key metadata into internal key profile. | WS03-T03 |
| WS03-T05 | P0 | planned | Implement Transit encrypt with explicit `key_version`. | WS03-T01, WS02-T05 |
| WS03-T06 | P0 | planned | Implement Transit decrypt with required v0.1 AAD and future compatibility hooks. | WS03-T01, WS02-T05 |
| WS03-T07 | P0 | planned | Implement OpenBao error classification. | WS03-T03 |
| WS03-T08 | P0 | planned | Ensure HTTP client redacts request/response logging. | WS03-T01 |
| WS03-T09 | P0 | planned | Implement probe encrypt/decrypt with non-secret random data. | WS03-T05, WS03-T06 |
| WS03-T10 | P0 | planned | Implement `disable_upsert` read for `doctor`. | WS03-T03 |
| WS03-T11 | P1 | planned | Implement capability checks for policy diagnostics. | WS03-T01 |
| WS03-T12 | P0 | planned | Add decrypt micro-batching support behind disabled-by-default config. | WS03-T06 |
| WS03-T13 | P2 | planned | Add namespace support tests. | WS03-T01 |

Acceptance criteria:

- Encrypt always sends explicit `key_version`.
- AAD mismatch fails in real OpenBao integration tests.
- Metadata parser detects exportable, plaintext backup, deletion allowed, derived, convergent, and version restrictions.
- Errors are mapped to stable classes without leaking tokens or payloads.
- TLS server name mismatch fails.
- Batched decrypt preserves per-item AAD, key ID validation, cancellation, and error mapping semantics.

Test requirements:

- Unit tests with fake HTTP server.
- Integration tests with real OpenBao.
- Redaction tests for request failures.

## WS04: JWT Authentication And Token Lifecycle

Goal: authenticate to OpenBao through JWT and maintain an in-memory OpenBao token safely.

| ID | Priority | Status | Task | Dependencies |
|---|---|---|---|---|
| WS04-T01 | P0 | planned | Implement JWT file reader. | WS01-T06 |
| WS04-T02 | P0 | planned | Parse JWT claims locally for expiry and diagnostics. | WS04-T01 |
| WS04-T03 | P0 | planned | Validate JWT minimum remaining TTL before login. | WS04-T02 |
| WS04-T04 | P0 | planned | Implement OpenBao JWT login. | WS03-T01 |
| WS04-T05 | P0 | planned | Store OpenBao token only in memory. | WS04-T04 |
| WS04-T06 | P0 | planned | Implement token renewal where allowed. | WS04-T05 |
| WS04-T07 | P0 | planned | Implement re-login before token expiry. | WS04-T05 |
| WS04-T08 | P0 | planned | Re-read JWT file before re-login. | WS04-T07 |
| WS04-T09 | P0 | planned | Expose auth state for Status cache and readiness. | WS04-T05 |
| WS04-T10 | P0 | planned | Add redaction tests for JWT and OpenBao token. | WS04-T01 |
| WS04-T11 | P1 | planned | Add support for non-renewable tokens. | WS04-T07 |
| WS04-T12 | P1 | planned | Add clock skew diagnostics. | WS04-T02 |
| WS04-T13 | P2 | planned | Add certificate auth as non-default alternative. | WS03-T02 |

Acceptance criteria:

- Missing, unreadable, expired, or near-expiry JWT fails closed.
- Wrong role or policy failure is classified.
- JWT is re-read before re-login.
- OpenBao token never touches disk.
- Logs and command output never include JWT or OpenBao token.

Test requirements:

- JWT fixture tests.
- Fake OpenBao auth server tests.
- Integration tests for successful and failed JWT login.
- Token lifecycle tests with fake clocks.

## WS05: KMS v2 Protocol Server

Goal: implement Kubernetes KMS v2 gRPC behavior exactly enough for kube-apiserver interoperability.

| ID | Priority | Status | Task | Dependencies |
|---|---|---|---|---|
| WS05-T01 | P0 | planned | Add Kubernetes KMS v2 protobuf dependency. | WS00-T01 |
| WS05-T02 | P0 | planned | Implement gRPC server skeleton. | WS05-T01 |
| WS05-T03 | P0 | planned | Implement `Status` from cached status. | WS06-T02 |
| WS05-T04 | P0 | planned | Implement `Encrypt`. | WS02-T07, WS03-T05 |
| WS05-T05 | P0 | planned | Implement `Decrypt`. | WS02-T08, WS03-T06 |
| WS05-T06 | P0 | planned | Enforce Status/encrypt key ID invariant. | WS05-T03, WS05-T04 |
| WS05-T07 | P0 | planned | Propagate request UID only to safe logs/traces. | WS09-T01 |
| WS05-T08 | P0 | planned | Add timeout and context cancellation handling. | WS03-T01 |
| WS05-T09 | P0 | planned | Add panic recovery with redacted errors. | WS05-T02 |
| WS05-T10 | P1 | planned | Add graceful shutdown behavior. | WS07-T04 |

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

## WS06: Status Cache, Health, And Rotation Watcher

Goal: keep Kubernetes Status cheap while background probes maintain OpenBao and key state.

| ID | Priority | Status | Task | Dependencies |
|---|---|---|---|---|
| WS06-T01 | P0 | planned | Implement background probe scheduler. | WS03-T03, WS04-T09 |
| WS06-T02 | P0 | planned | Implement Status cache with staleness policy. | WS06-T01 |
| WS06-T03 | P0 | planned | Implement rotation observation state machine. | WS02-T07, WS03-T04 |
| WS06-T04 | P0 | planned | Enforce stable observation count. | WS06-T03 |
| WS06-T05 | P0 | planned | Enforce activation delay. | WS06-T03 |
| WS06-T06 | P0 | planned | Reject Transit version rollback by default. | WS06-T03 |
| WS06-T07 | P0 | planned | Add multi-node consistency diagnostics. | WS09-T03 |
| WS06-T08 | P0 | planned | Integrate persisted registry state into rotation promotion and restart behavior. | WS02-T12 |
| WS06-T09 | P1 | planned | Add circuit breaker for repeated OpenBao failures. | WS03-T07 |
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

## WS07: Socket And Runtime Service Behavior

Goal: safely expose the local Unix socket and behave predictably under service restarts.

| ID | Priority | Status | Task | Dependencies |
|---|---|---|---|---|
| WS07-T01 | P0 | planned | Implement socket parent directory validation. | WS01-T05 |
| WS07-T02 | P0 | planned | Implement safe Unix socket creation. | WS07-T01 |
| WS07-T03 | P0 | planned | Implement stale socket detection and safe cleanup. | WS07-T02 |
| WS07-T04 | P0 | planned | Implement signal handling and graceful shutdown. | WS05-T02 |
| WS07-T05 | P0 | planned | Implement `/live` endpoint. | WS05-T02 |
| WS07-T06 | P0 | planned | Implement `/ready` endpoint. | WS06-T02 |
| WS07-T07 | P0 | planned | Reject symlink and regular-file socket paths. | WS07-T01 |
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

## WS08: CLI Tooling

Goal: provide operational commands required by the design.

| ID | Priority | Status | Task | Dependencies |
|---|---|---|---|---|
| WS08-T01 | P0 | planned | Implement `serve`. | WS05-T02, WS07-T02 |
| WS08-T02 | P0 | planned | Implement `doctor` framework. | WS01-T03 |
| WS08-T03 | P0 | planned | Add `doctor` OpenBao TLS/auth checks. | WS03-T02, WS04-T04 |
| WS08-T04 | P0 | planned | Add `doctor` Transit key and policy checks. | WS03-T04, WS03-T10 |
| WS08-T05 | P0 | planned | Add `doctor` socket checks. | WS07-T01 |
| WS08-T06 | P0 | planned | Add `doctor` EncryptionConfiguration checks. | WS01-T08 |
| WS08-T07 | P0 | planned | Implement `verify-key`. | WS03-T04 |
| WS08-T08 | P0 | planned | Implement `benchmark` smoke mode. | WS03-T09 |
| WS08-T09 | P0 | planned | Implement `rotation-plan`. | WS06-T03 |
| WS08-T10 | P0 | planned | Implement `verify-rotation` initial confidence report. | WS06-T03 |
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

## WS09: Observability And Redaction

Goal: make the provider diagnosable without leaking sensitive material.

| ID | Priority | Status | Task | Dependencies |
|---|---|---|---|---|
| WS09-T01 | P0 | planned | Add structured JSON logger. | WS00-T02 |
| WS09-T02 | P0 | planned | Implement redaction helpers. | WS09-T01 |
| WS09-T03 | P0 | planned | Add Prometheus metrics registry. | WS05-T02 |
| WS09-T04 | P0 | planned | Add KMS request metrics. | WS05-T04, WS05-T05 |
| WS09-T05 | P0 | planned | Add OpenBao request metrics. | WS03-T01 |
| WS09-T06 | P0 | planned | Add auth/token metrics. | WS04-T09 |
| WS09-T07 | P0 | planned | Add status cache and rotation metrics. | WS06-T02 |
| WS09-T08 | P0 | planned | Add AAD/key ID validation metrics. | WS02-T08 |
| WS09-T09 | P0 | planned | Add log/metric redaction tests. | WS09-T02 |
| WS09-T10 | P1 | planned | Add example alert rules. | WS09-T03 |
| WS09-T11 | P1 | planned | Add debug correlation mode with strict guardrails. | WS03-T07 |

Acceptance criteria:

- Logs are JSON.
- Metrics match [Observability](../observability.md).
- No plaintext, JWT, token, full ciphertext, raw key name, or raw mount path appears in normal logs.
- Metrics use bounded labels.

Test requirements:

- Redaction unit tests.
- Integration test log capture.
- Metrics label cardinality tests.

## WS10: Deployment Artifacts And Packaging

Goal: provide installable artifacts and tested deployment examples.

| ID | Priority | Status | Task | Dependencies |
|---|---|---|---|---|
| WS10-T01 | P0 | planned | Add sample systemd unit. | WS07-T02 |
| WS10-T02 | P0 | planned | Add sample static pod manifest. | WS07-T02 |
| WS10-T03 | P0 | planned | Add sample plugin config. | WS01-T01 |
| WS10-T04 | P0 | planned | Add sample Kubernetes `EncryptionConfiguration`. | WS01-T07 |
| WS10-T05 | P0 | planned | Add container image build. | WS00-T07 |
| WS10-T06 | P1 | planned | Add package install layout for Linux using the resolved identity model. | WS00-T07 |
| WS10-T07 | P1 | planned | Add kubeadm lab deployment scripts. | WS11-T06 |
| WS10-T08 | P1 | planned | Add upgrade and rollback scripts for lab use. | WS10-T06 |
| WS10-T09 | P2 | planned | Add OpenTofu module skeleton. | WS03-T10 |
| WS10-T10 | P2 | planned | Add OpenBao policy generator. | WS08-T02 |

Acceptance criteria:

- systemd and static pod samples match docs.
- Static pod manifest does not reference ConfigMaps, Secrets, or ServiceAccounts.
- Container image runs as non-root where feasible.
- Install layout matches [Install](../install.md).

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
| WS11-T02 | P0 | planned | Implement fake Transit. | WS03-T01 |
| WS11-T03 | P0 | planned | Implement fake auth/token manager. | WS04-T05 |
| WS11-T04 | P0 | planned | Build KMS v2 fake conformance suite. | WS05-T02 |
| WS11-T05 | P0 | planned | Add OpenBao `2.5.3` integration test container. | WS03-T05, WS04-T04 |
| WS11-T06 | P0 | planned | Add minimal pinned Kubernetes `1.34.x` kind e2e. | WS10-T02, WS00-T09 |
| WS11-T07 | P0 | planned | Add failure injection tests. | WS06-T02 |
| WS11-T08 | P0 | planned | Add rotation tests. | WS06-T03 |
| WS11-T09 | P0 | planned | Add decrypt storm performance smoke test. | WS05-T05 |
| WS11-T10 | P1 | planned | Add kubeadm VM e2e. | WS10-T01, WS10-T02 |
| WS11-T11 | P1 | planned | Add OpenBao HA failover tests. | WS03-T01 |
| WS11-T12 | P1 | planned | Add DR restore tests. | WS10-T07 |
| WS11-T13 | P0 | planned | Add exact-pinned Kubernetes `1.34.x` release-gate matrix row. | WS11-T06 |
| WS11-T14 | P0 | planned | Add exact-pinned OpenBao `2.5.3` release-gate matrix row. | WS11-T05 |
| WS11-T15 | P1 | planned | Add long-running Status polling test. | WS06-T02 |
| WS11-T16 | P1 | planned | Add future-candidate Kubernetes lanes only after exact versions are approved. | WS11-T13 |

Acceptance criteria:

- v0.1 release gates pass.
- Every PR runs fast tests.
- Nightly runs OpenBao `2.5.3` integration and pinned Kubernetes `1.34.x` kind e2e.
- Release candidate runs exact-pinned matrix and DR tests.

Test requirements:

- See [Testing strategy](../testing-strategy.md).
- See [Release gates](../release-gates.md).

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
| RD-03 | Initial Kubernetes validation target is the `1.34` release line with exact patch/image digest pinned in CI. | [ADR 0007](../adr/0007-pinned-ci-version-matrix.md) |
| RD-04 | AAD is enabled and required for v0.1. | [ADR 0008](../adr/0008-aad-required-for-mvp.md) |
| RD-05 | Decrypt micro-batching is included in v0.1 behind disabled-by-default config. | [ADR 0009](../adr/0009-include-decrypt-microbatching.md) |
| RD-06 | Project name, binary name, Go version, Viper, Makefile, and version policy path are fixed for M0. | [ADR 0010](../adr/0010-project-naming-and-m0-foundation.md) |
| RD-07 | Production Go uses typed models and forbids `map[string]any`, `map[string]interface{}`, and broad `any` outside reviewed boundary adapters. | [ADR 0011](../adr/0011-strict-typed-idiomatic-go.md) |

## Open Questions To Resolve During Implementation

| ID | Question | Needed by | Owner |
|---|---|---|---|
| OQ-01 | What exact Linux user/group model should packaged systemd deployments use? | WS10-T06 | Packaging lead |
| OQ-02 | Which exact Kubernetes 1.34 patch and Kind node image digest will be pinned for v0.1? | WS00-T09 | Release lead |

## v0.1 Blocking Checklist

This checklist duplicates the release gates in implementation terms:

- WS02 key ID/AAD golden tests pass.
- WS05 KMS v2 fake conformance suite passes.
- WS03/WS04 real OpenBao `2.5.3` integration suite passes.
- WS11 pinned Kubernetes `1.34.x` kind e2e proves Secret encryption/decryption.
- API server restart with encrypted Secret works.
- Status key ID equals encrypt response key ID.
- Unknown key ID decrypt is rejected before Transit call.
- AAD mismatch decrypt is rejected.
- AAD is required for v0.1 objects.
- Transit encrypt uses explicit `key_version`.
- Decrypt micro-batching is implemented behind disabled-by-default config and benchmarked.
- Rotation v1 to v2 works without key ID flip-flop.
- Old ciphertext remains decryptable after rotation.
- JWT expiry/re-login path works.
- OpenBao outage fails closed.
- `doctor` catches bad socket, bad JWT, bad policy, and bad Transit key config.
- Logs and metrics redaction tests pass.
- Static pod manifest does not rely on API objects.
- systemd unit ordering is validated in kubeadm-style test.
- Supply-chain release gates produce SBOM, signatures, attestations, reproducibility evidence, and provenance index.
