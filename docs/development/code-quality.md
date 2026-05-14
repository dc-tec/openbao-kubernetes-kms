---
title: "Code Quality"
description: "Strict typed Go conventions, ast-grep architecture rules, Semgrep security rules, package boundaries, and required quality gates for bao-kms-provider."
weight: 20
---

# Code Quality

This project treats strict, idiomatic Go as part of the security and reliability model.

The provider runs in the Kubernetes API server boot path and handles plaintext key material at the KMS boundary. Loose typing, dynamic maps, implicit decoding, and unbounded error paths are implementation risks, not style preferences.

## Core Rules

- Production code does not use `map[string]any`.
- Production code does not use `map[string]interface{}`.
- Production code does not use broad `any` or `interface{}` escape hatches except in reviewed boundary adapters.
- Kubernetes annotations use `map[string]string`. This is allowed because the type is a Kubernetes protocol surface.
- Viper is isolated to `internal/config` and command wiring. Business-logic packages do not depend on Viper.
- Configuration, OpenBao DTOs, KMS models, AAD envelopes, annotations, registry state, and status values use typed structs.
- Configuration and external JSON or YAML decode into typed structs with unknown-field rejection where the parser supports it.
- Boundary DTOs are converted into internal domain models before crossing package boundaries.
- JSON, YAML, AAD, annotations, and OpenBao request bodies are not constructed through ad hoc string concatenation.
- State machines are typed constants or enums with validation, not free-form strings.
- Request-path code does not panic.
- `context.Context` propagates through OpenBao calls and KMS request handling.
- Error messages are stable, classified, and redacted.

## Boundary Exceptions

Dynamic input is unavoidable at narrow external boundaries. Acceptable patterns:

- `json.RawMessage` in a small boundary DTO immediately before typed conversion,
- third-party interfaces that require `any`, wrapped in a single package,
- test helpers that intentionally generate malformed input.

Every exception is local, documented in code, covered by tests, and does not cross package boundaries as dynamic state.

## Package Expectations

| Package area | Quality expectation |
|---|---|
| `internal/config` | Viper and environment binding allowed only here or in command setup. Output is typed immutable configuration. |
| `internal/aad` | Canonical serialization with golden tests. No dynamic maps or string-built JSON. |
| `internal/keyregistry` | Typed snapshots and state transitions. Deterministic IDs. Rollback tests. |
| `internal/openbao` | Typed auth and Transit request and response DTOs, typed domain conversion, and redacted error handling. |
| `internal/kmsv2` | No Transit fallback loops. Validation order tested. |
| `internal/logging` | Redaction helpers and bounded structured fields. |
| `internal/socket` | Explicit Unix permission and file-type checks. |

## Required Gates

Every implementation PR passes:

- `gofmt`
- `gofumpt`
- `go vet`
- `staticcheck`
- `govulncheck`
- `golangci-lint`
- ast-grep custom rules
- Semgrep custom rules
- race smoke tests once packages exist
- ast-grep forbidden dynamic-type rules
- redaction tests for logs and command output

The `golangci-lint` policy includes at least:

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

The Makefile runs documentation and version checks plus ast-grep and Semgrep rule tests. The repository encodes custom code-quality policy with separate tool responsibilities:

- `.ast-grep/sgconfig.yml`
- `.ast-grep/rules/architecture`
- `.ast-grep/rules/runtime-safety`
- `.semgrep/rules`
- `.semgrep/tests`

ast-grep owns structural Go and architecture rules:

- no broad dynamic types in production code,
- no runtime panics,
- no root contexts in runtime packages,
- no Viper imports outside the configuration boundary,
- no environment reads outside the configuration boundary,
- no concrete OpenBao or Transit client imports from `internal/kmsv2`.

Semgrep owns security and dangerous-API rules:

- no disabled TLS verification,
- no default HTTP client or package-level HTTP helpers,
- no `http.NewRequest` without context,
- no runtime subprocess execution,
- no sensitive log field names.

The repository also includes `.golangci.yml` as the baseline lint policy.

The default is strict. Narrow reviewed exceptions are added only when a typed alternative is demonstrably worse. Prefer ast-grep or Semgrep exceptions over broad grep exclusions because they are easier to test and review.
