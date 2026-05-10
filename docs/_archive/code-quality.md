# Code Quality

This project treats strict, idiomatic Go as part of the security and reliability model.

The provider runs in the Kubernetes API server boot path and handles plaintext key material at the KMS boundary. Loose typing, dynamic maps, implicit decoding, and unbounded error paths are implementation risks, not style preferences.

## Core Rules

- Production code must not use `map[string]any`.
- Production code must not use `map[string]interface{}`.
- Production code must not use broad `any` / `interface{}` escape hatches except in reviewed boundary adapters.
- Kubernetes annotations use `map[string]string`; this is allowed because it is a typed Kubernetes protocol surface.
- Viper is isolated to `internal/config` and command wiring. Business logic packages must not depend on Viper.
- Config, OpenBao DTOs, KMS models, AAD envelopes, annotations, registry state, and status values must use typed structs.
- Decode config and external JSON/YAML into typed structs with unknown-field rejection where the parser supports it.
- Convert boundary DTOs into internal domain models before crossing package boundaries.
- Do not construct JSON, YAML, AAD, annotations, or OpenBao request bodies through ad hoc string concatenation.
- Do not represent state machines as free-form strings in internal logic; use typed constants or enums with validation.
- Do not panic in request-path code.
- Always propagate `context.Context` through OpenBao calls and KMS request handling.
- Error messages must be stable, classified, and redacted.

## Boundary Exceptions

Dynamic input may be unavoidable at narrow external boundaries. Acceptable exception patterns:

- `json.RawMessage` in a small boundary DTO before immediate typed conversion.
- Third-party interfaces that require `any`, wrapped in one package.
- Test helpers that intentionally generate malformed input.

Every exception must be local, documented in code, covered by tests, and must not cross package boundaries as dynamic state.

## Package Expectations

| Package area | Quality expectation |
|---|---|
| `internal/config` | Viper and environment binding allowed only here or in command setup; output is typed immutable config. |
| `internal/aad` | Canonical serialization with golden tests; no dynamic maps or string-built JSON. |
| `internal/keyregistry` | Typed snapshots and state transitions; deterministic IDs; rollback tests. |
| `internal/transit` | Typed request/response DTOs and typed domain conversion. |
| `internal/kmsv2` | No Transit fallback loops; validation order tested. |
| `internal/logging` | Redaction helpers and bounded structured fields. |
| `internal/socket` | Explicit Unix permission and file-type checks. |

## Required Gates

Every M0/M1 implementation PR must pass:

- `gofmt`
- `gofumpt`
- `go vet`
- `staticcheck`
- `govulncheck`
- `golangci-lint`
- ast-grep custom rules
- Semgrep custom rules
- race smoke tests once packages exist
- ast-grep forbidden dynamic type rules
- redaction tests for logs and command output

The planned `golangci-lint` policy must include at least:

- `bodyclose`
- `errcheck`
- `gosec`
- `govet`
- `ineffassign`
- `misspell`
- `revive`
- `staticcheck`
- `unparam`
- `unused`

## Enforcement

The Makefile runs docs/version checks plus ast-grep and Semgrep rule tests. The repository encodes custom code-quality policy with different tool responsibilities:

- `.ast-grep/sgconfig.yml`
- `.ast-grep/rules/architecture`
- `.ast-grep/rules/runtime-safety`
- `.semgrep/rules`
- `.semgrep/tests`

ast-grep owns structural Go and architecture rules:

- no broad dynamic types in production code,
- no runtime panics,
- no root contexts in runtime packages,
- no Viper imports outside the config boundary,
- no environment reads outside the config boundary,
- no concrete OpenBao/Transit client imports from `internal/kmsv2`.

Semgrep owns security and dangerous API-usage rules:

- no disabled TLS verification,
- no default HTTP client or package-level HTTP helpers,
- no `http.NewRequest` without context,
- no runtime subprocess execution,
- no sensitive log field names.

The repository also includes `.golangci.yml` as the baseline lint policy. M0 must validate that config against the pinned `golangci-lint` version selected for CI.

The default is strict: add narrow reviewed exceptions only when a typed alternative is demonstrably worse. Prefer ast-grep or Semgrep exceptions over broad grep exclusions because they are easier to test and review.
