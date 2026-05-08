# E2E Framework

The E2E framework is intentionally smaller than the OpenBao Operator framework, but it follows the same direction: Ginkgo/Gomega specs, label-based routing, a suite manifest, pinned version policy, and machine-readable reports.

## Goals

- keep unit and hermetic integration tests free of external services,
- provide a PR-capable ephemeral OpenBao `2.5.3` CI environment and a path for Kind KMS v2 smoke tests,
- avoid duplicated OpenBao and Kubernetes versions across workflows,
- produce JUnit and Ginkgo JSON reports for CI summaries and release evidence.

## Test Layers

| Layer | Command | External dependency |
|---|---|---|
| Unit | `go test ./...` | None |
| Hermetic OpenBao integration | `go test -tags=integration ./internal/openbao` | None |
| E2E | `make test-e2e` | Depends on selected labels |
| OpenBao CI E2E | `make test-e2e-openbao-ci` | Docker-compatible container runtime |

The OpenBao CI lane starts an owned OpenBao container, enables Transit, creates the test key, disables Transit upsert, and then runs Transit assertions without external OpenBao credentials.

## Layout

```text
test/e2e/
  e2e_suite_test.go
  openbao_transit_test.go
  suites.yaml
  suites_manifest_test.go
  framework/
    artifacts.go
    env.go
    labels.go
    openbao_environment.go
```

The root `test/e2e` package is the primary suite package. New E2E specs should be added to this package unless there is a concrete reason to split binaries.

Shared helpers live under `test/e2e/framework`. Keep helpers small and explicit; broad test harness abstractions should wait until multiple specs need them.

## Suite Manifest

`test/e2e/suites.yaml` describes E2E lanes. It does not own concrete OpenBao or Kubernetes versions. Those remain in `.ci/versions.yaml`, and lanes reference the relevant fields through `versionRefs`.

The current lanes are:

| Lane | Status | Purpose |
|---|---|---|
| `openbao-ci` | active | Run OpenBao Transit coverage against an ephemeral OpenBao `2.5.3` CI environment. |
| `kind-smoke` | planned | Validate KMS v2 encryption with a real Kind API server. |
| `release-gate` | planned | Collect the pinned v0.1 OpenBao and Kubernetes E2E checks. |

The manifest is validated by:

```sh
make verify-e2e-manifest
```

The validation currently checks schema shape, required fields, lane IDs, status values, and the absence of floating `latest` references. It is deliberately lightweight until CI matrix generation exists.

## Labels

Labels are the routing API. Prefer small composable labels:

```go
Label("openbao", "transit", "ci")
Label("kind", "kmsv2", "smoke")
Label("release-gate")
```

Use stable `case:<id>` labels when a spec becomes release evidence or a known regression target.

## Commands

Run the whole enabled E2E suite:

```sh
make test-e2e
```

Run a label-filtered suite when Ginkgo is installed:

```sh
make test-e2e E2E_LABEL_FILTER='openbao && transit && ci'
```

Run the OpenBao CI lane:

```sh
make test-e2e-openbao-ci
```

`make test-e2e-openbao` is an alias for the OpenBao CI lane.

OpenBao CI environment:

| Variable | Default | Purpose |
|---|---|---|
| `E2E_OPENBAO_CI` | set by `make test-e2e-openbao-ci` | Enables the PR-capable OpenBao CI spec. |
| `E2E_OPENBAO_IMAGE` | `validation.openbao.image` from `.ci/versions.yaml` through Make | OpenBao image used by the CI environment. |
| `DOCKER` | `docker` | Container runtime CLI compatible with Docker commands. |
| `E2E_SKIP_CLEANUP` | `false` | Keeps generated OpenBao TLS files for debugging. |

## Reports And Artifacts

The Makefile exposes:

| Variable | Default |
|---|---|
| `E2E_ARTIFACT_DIR` | `artifacts/e2e` |
| `E2E_JUNIT_REPORT` | `artifacts/e2e/junit.xml` |
| `E2E_JSON_REPORT` | `artifacts/e2e/ginkgo.json` |
| `E2E_TIMEOUT` | `30m` |
| `E2E_PARALLEL_NODES` | `1` |

Keep E2E logs redacted. Never write OpenBao tokens, JWTs, plaintext, or full ciphertext values to artifacts.

## Parallelism

Default parallelism is one node. Only increase `E2E_PARALLEL_NODES` for lanes whose environments are isolated per process. Shared socket paths and single Kind clusters should remain single-node until the environment code proves isolation.

## Next Steps

- Add Kind environment helpers once the provider can serve KMS v2.
- Generate CI matrices from `test/e2e/suites.yaml` after the lane set stabilizes.
- Add an E2E report summarizer once Ginkgo JSON reports are uploaded by CI.
