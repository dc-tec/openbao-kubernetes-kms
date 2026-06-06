---
title: "Contributing"
description: "Repository layout, local development setup, test expectations, code quality rules, wire compatibility commitments, and documentation update policy for bao-kms-provider contributors."
weight: 10
---

# Contributing

This page covers contributing to `bao-kms-provider`. Operator-side documentation is in [Start Here](/getting-started/) and the rest of the user-facing sections.

## Project Layout

| Area | Value |
|---|---|
| Project | `openbao-kubernetes-kms` |
| Go module | `github.com/dc-tec/openbao-kubernetes-kms` |
| Binary | `bao-kms-provider` |
| Go toolchain | `1.26.4` (pinned in `.go-version` and `.ci/versions.yaml`) |
| CLI and configuration framework | Viper (isolated to `internal/config` and command setup) |
| Task runner | Makefile |
| Version policy file | `.ci/versions.yaml` |

Go package layout:

```text
cmd/bao-kms-provider
internal/aad
internal/auth
internal/config
internal/health
internal/keyregistry
internal/kmsv2
internal/logging
internal/metrics
internal/openbao
internal/runtime
internal/socket
internal/status
internal/version
test/e2e
test/fakes
test/kmsconformance
test/deployment
```

## Local Development

Every PR should run the local core gate:

```sh
make ci-core
```

Run focused E2E lanes when a change touches OpenBao, Kubernetes, deployment,
rotation, failure injection, or release packaging behavior. The lane commands
live in [Development: E2E Framework](/development/e2e-framework/).

For deployment sample or package metadata changes, run the focused deployment
checks:

```sh
make deployment-samples-check
make package-build-check
```

`deployment-samples-check` verifies the systemd unit with the host
`systemd-analyze` binary when it is installed. `package-build-check` uses the
pinned nFPM version from `.ci/versions.yaml`/`mk/config.mk` through `go run` and
builds throwaway `.deb` and `.rpm` packages from a temporary placeholder binary.

## OpenBao Integration Tests

OpenBao integration tests are build-tagged and stay hermetic. They use in-process HTTPS fakes for OpenBao response shapes and do not require external OpenBao credentials:

```sh
go test -tags=integration ./internal/openbao -run TestOpenBaoTransitIntegration -count=1
```

## OpenBao E2E Tests

OpenBao E2E validation uses the ephemeral CI lane. E2E specs use Ginkgo v2 and Gomega, pinned in `.ci/versions.yaml`. Lanes are described in `test/e2e/suites.yaml`:

```sh
make test-e2e-openbao-ci
```

The OpenBao CI target starts real OpenBao, bootstraps provider auth, runs the
provider, and exercises the Unix socket with the Kubernetes KMS v2 protobuf
client.

For the full E2E framework, label routing, suite manifest rules, and report artifacts see [Development: E2E Framework](/development/e2e-framework/).

## Go Code Quality

Implementation follows [Development: Code Quality](/development/code-quality/). Key rules:

- no `map[string]any` in production code,
- no `map[string]interface{}` in production code,
- no broad `any` or `interface{}` outside reviewed boundary adapters,
- Viper stays at the CLI and configuration boundary,
- OpenBao, configuration, KMS, AAD, and registry data use typed structs,
- decode unknown fields strictly where the parser supports it,
- no free-form string state machines in internal logic,
- no panics in request-path code.

## Wire Compatibility

The following surfaces are wire-format commitments:

- Kubernetes provider name behavior,
- `key_id` derivation,
- annotation keys and values,
- AAD canonicalization,
- historical key lookup behavior,
- compatibility mode semantics.

Any change to these surfaces requires:

- a documented migration plan,
- updated golden fixtures,
- a release note,
- an explicit compatibility section in [Reference: Compatibility](/reference/compatibility/).

## Redaction Rules

Tests must prove these never appear in logs or command output:

- plaintext,
- JWTs,
- OpenBao tokens,
- full ciphertext,
- raw Transit key material.

For the full redaction policy see [Reference: Observability: Logs](/reference/observability/#logs) and [Security: Hardening: Logging](/security/hardening/#logging).

## Dependency Policy

Prefer:

- the official Kubernetes KMS protobuf package,
- official or OpenBao-compatible API clients where practical,
- Viper for CLI configuration loading and command environment binding,
- standard library parsers for structured data,
- small dependencies with clear maintenance status.

Avoid:

- ad hoc string parsing for YAML, JSON, or JWT where robust parsers exist,
- dependencies that log requests by default,
- dependencies that make TLS verification difficult to control.

## Documentation Updates

When implementation changes behavior, update documentation in the same change:

- configuration changes update [Reference: Configuration](/reference/configuration/),
- KMS protocol behavior updates [Reference: KMS v2 Contract](/reference/kms-v2-contract/),
- `key_id` or AAD changes update [Reference: Key ID And AAD](/reference/key-id-and-aad/),
- operational changes update the relevant operations or deployment runbook,
- support and version-envelope changes update [Reference: Compatibility](/reference/compatibility/).

For writing style, page structure, links, and docs verification, see
[Development: Docs Style Guide](/development/docs-style-guide/).
