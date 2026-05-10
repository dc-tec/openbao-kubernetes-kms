# 0011: Strict Typed Idiomatic Go

## Status

Accepted.

## Context

The provider handles Kubernetes KMS requests, plaintext at the envelope-encryption boundary, OpenBao credentials, OpenBao Transit metadata, and local control-plane socket access. Dynamic data structures make validation order, redaction, compatibility, and failure behavior harder to reason about.

M0 needs code quality rules before the first Go packages land so the codebase does not accumulate weak patterns.

## Decision

- Production code must not use `map[string]any`.
- Production code must not use `map[string]interface{}`.
- Broad `any` / `interface{}` usage is forbidden outside reviewed boundary adapters and tests.
- Viper is allowed only at the CLI/config boundary. Runtime packages receive typed config structs.
- Config, OpenBao request/response DTOs, KMS models, registry state, AAD envelopes, and annotations must be typed.
- Unknown fields should be rejected during decoding wherever practical.
- Dynamic JSON should use `json.RawMessage` only in narrow boundary DTOs and must be converted immediately into typed domain models.
- Internal states must use typed constants or enums, not free-form strings.
- M0 includes local and CI gates for formatting, vetting, static analysis, vulnerability checks, ast-grep rules, Semgrep rules, and forbidden dynamic-type patterns.

## Allowed Cases

- `map[string]string` for Kubernetes annotations and bounded label maps.
- `json.RawMessage` in boundary packages before typed conversion.
- Third-party callback signatures that require `any`, isolated behind small wrappers.
- Test fuzzing and malformed-input generation.

## Consequences

- OpenBao API integration takes slightly more upfront DTO work.
- Config and compatibility behavior becomes easier to test and review.
- Redaction and error classification have concrete typed surfaces.
- A future custom lint analyzer may replace grep-based checks if exception handling becomes too noisy.
- ast-grep carries structural Go rules for dynamic types, panics, root contexts, Viper/env isolation, and KMS package dependency inversion.
- Semgrep carries security-oriented rules for TLS verification, HTTP client usage, context-aware requests, subprocess execution, and sensitive logging.
