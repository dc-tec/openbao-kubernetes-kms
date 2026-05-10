# Development Guide

This document defines expectations for implementation work.

## Current State

The repository is in implementation. Workstreams should still update the contract docs, ADRs, and backlog in the same change when behavior changes.

## M0 Foundation Decisions

| Area | Decision |
|---|---|
| Project/repository | `openbao-kubernetes-kms` |
| Go module | `github.com/dc-tec/openbao-kubernetes-kms` |
| Binary | `bao-kms-provider` |
| Go toolchain | `1.26.3` |
| CLI/config framework | Viper |
| Task runner | Makefile |
| Version policy | `.ci/versions.yaml` |

## Suggested Package Boundaries

Current Go package layout:

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

Planned package boundaries:

```text
test/integration
```

## Implementation Order

Recommended order:

1. Config loader and validation.
2. Key ID and AAD golden tests.
3. Fake Transit implementation and shared test fakes.
4. KMS v2 protobuf server/client test harness.
5. KMS v2 conformance tests.
6. OpenBao client and integration tests.
7. JWT auth manager.
8. Socket handling.
9. Status cache and background probes.
10. CLI commands.
11. systemd/static pod packaging.
12. kind e2e.

## Test Expectations

Every PR should run:

```sh
go test ./...
go test -race ./internal/...
go vet ./...
staticcheck ./...
gofmt -w
```

Once packages exist, add:

- KMS v2 fake conformance tests,
- redaction tests,
- config validation tests,
- fuzz smoke tests,
- key ID/AAD golden tests.

### OpenBao Integration Tests

OpenBao integration tests are build-tagged and must stay hermetic. They use in-process HTTPS fakes for OpenBao response shapes and must not depend on external OpenBao credentials:

```sh
go test -tags=integration ./internal/openbao -run TestOpenBaoTransitIntegration -count=1
```

### OpenBao E2E Tests

OpenBao E2E validation uses the ephemeral CI lane. E2E specs use Ginkgo v2 and Gomega, pinned in `.ci/versions.yaml`, with lanes described in `test/e2e/suites.yaml`:

```sh
make test-e2e-openbao-ci
```

The OpenBao CI target also runs the provider full-stack slice under the `e2e` build tag. That test builds the provider image, starts real OpenBao, boots JWT auth, runs the provider container, and exercises the Unix socket from a second container with the Kubernetes KMS v2 protobuf client.

Optional environment:

| Variable | Default |
|---|---|
| `E2E_OPENBAO_IMAGE` | `.ci/versions.yaml` `validation.openbao.image` |
| `DOCKER` | `docker` |
| `E2E_SKIP_CLEANUP` | `false` |

The OpenBao CI and release-gate e2e lanes must use OpenBao `2.5.3`, matching `.ci/versions.yaml`.

See [E2E framework](e2e-framework.md) for labels, artifact paths, report variables, and suite manifest rules.

## Go Code Quality

Implementation must follow [Code quality](code-quality.md).

Key rules:

- no `map[string]any` in production code,
- no `map[string]interface{}` in production code,
- no broad `any` / `interface{}` outside reviewed boundary adapters,
- Viper stays at the CLI/config boundary,
- OpenBao, config, KMS, AAD, and registry data use typed structs,
- decode unknown fields strictly where possible,
- no free-form string state machines in internal logic,
- no panics in request-path code.

## Wire Compatibility

The following are wire-format commitments:

- Kubernetes provider name behavior,
- key ID derivation,
- annotation keys and values,
- AAD canonicalization,
- historical key lookup behavior,
- compatibility mode semantics.

Any change requires:

- ADR update,
- golden fixture update,
- migration plan,
- release note.

## Redaction Rule

Tests must prove these never appear in logs or command output:

- plaintext,
- JWT,
- OpenBao token,
- full ciphertext,
- raw Transit key material.

## Dependency Policy

Prefer:

- official Kubernetes KMS protobuf package,
- official or OpenBao-compatible API client where practical,
- Viper for CLI configuration loading and command environment binding,
- standard library parsers for structured data,
- small dependencies with clear maintenance status.

Avoid:

- ad hoc string parsing for YAML/JSON/JWT where robust parsers exist,
- dependencies that log requests by default,
- dependencies that make TLS verification difficult to control.

## Documentation Updates

When implementation changes behavior, update docs in the same PR:

- config changes update [Configuration](configuration.md),
- KMS protocol behavior updates [KMS v2 contract](kms-v2-contract.md),
- key ID/AAD changes update [Key ID and AAD](key-id-and-aad.md),
- operational changes update deployment/runbook docs,
- support changes update [Compatibility](compatibility.md).
