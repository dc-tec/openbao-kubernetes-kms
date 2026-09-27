---
title: Code quality
description: "The strict typed Go rules, package boundaries, and lint, ast-grep, and Semgrep gates every pull request must pass."
eyebrow: Contribute
weight: 20
verifiedBy:
  - .golangci.yml
  - .ast-grep/sgconfig.yml
  - .semgrep/rules
  - mk/checks.mk
---

The provider handles key material on the API server boot path, so loose
typing, dynamic maps, implicit decoding, and unbounded error paths count as
security risks.

## Rules

- No `map[string]any`, `map[string]interface{}`, or broad `any` in production
  code, outside reviewed boundary adapters. Kubernetes annotations use
  `map[string]string` because that is the protocol type.
- Configuration, OpenBao DTOs, KMS models, AAD envelopes, annotations, registry
  state, and status use typed structs. External JSON and YAML decode into them
  with unknown-field rejection where the parser supports it, and DTOs become
  domain models before crossing a package boundary.
- JSON, YAML, AAD, annotations, and OpenBao request bodies are never built by
  string concatenation.
- State machines use validated typed constants, not free-form strings.
- Request-path code never panics, and `context.Context` flows through every
  OpenBao call and KMS request.
- Errors are stable, classified, and redacted.
- Viper and environment reads stay in `internal/config` and command wiring.

Dynamic input is allowed only at narrow boundaries: a `json.RawMessage` in a
small DTO right before typed conversion, a third-party interface that requires
`any` wrapped in one package, or a test helper that generates malformed input.
Each exception stays local, is documented in code, and is tested.

## Package expectations

| Package | Expectation |
|---|---|
| `internal/config` | The only place for Viper and environment binding; produces typed immutable configuration. |
| `internal/aad` | Canonical serialization with golden tests. |
| `internal/keyregistry` | Typed snapshots and transitions, deterministic IDs, rollback tests. |
| `internal/openbao` | Typed request and response DTOs converted to domain types, redacted errors. |
| `internal/kmsv2` | No Transit fallback loops; validation order tested; no concrete OpenBao client imports. |
| `internal/logging` | Redaction helpers and bounded fields. |
| `internal/socket` | Explicit Unix permission and file-type checks. |

## Gates

Every pull request passes `gofmt`, `gofumpt`, `go vet`, `staticcheck`,
`govulncheck`, `golangci-lint`, race smoke tests, redaction tests, and the
custom rules below. `.golangci.yml` enables at least `bodyclose`, `errcheck`,
`gosec`, `govet`, `ineffassign`, `misspell`, `revive`, `staticcheck`,
`unparam`, and `unused`.

`make ci-core` runs both the default lint pass and `make lint-tagged`.
The tagged pass uses cgo and enables `certauth_pkcs11`, `certauth_spiffe`,
`openbao_kms_e2e_spiffe_certauth`, and `e2e`. It checks optional certificate
sources, test tools, and container/Kind fixtures that the default build omits.
Including a source in this lint pass does not make it a supported release
configuration. Keep exceptions local to the fixture operation and explain
why the input or permission is required.

The core gate also runs `make test-certauth`: race-enabled unit tests for
certificate authentication, the CLI integration, and the certificate fixture
tool. These tests use local fixtures and do not require a hardware token.

| Tool | Rules | Location |
|---|---|---|
| ast-grep | No broad dynamic types, runtime panics, root contexts in runtime packages, Viper imports or environment reads outside configuration, or concrete OpenBao clients in `internal/kmsv2` | `.ast-grep/rules/architecture`, `.ast-grep/rules/runtime-safety` |
| Semgrep | No disabled TLS verification, default HTTP clients or package-level HTTP helpers, `http.NewRequest` without context, runtime subprocesses, or sensitive log field names | `.semgrep/rules`, tested in `.semgrep/tests` |

The default is strict. Add a narrow, reviewed exception only when the typed
alternative would weaken the boundary, and prefer a scoped ast-grep or Semgrep
exception over a broad exclusion.
