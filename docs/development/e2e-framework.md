---
title: "E2E Framework"
description: "Ginkgo and Gomega suite layout, label routing, suite manifest, lane definitions, command set, reports, and parallelism for the bao-kms-provider end-to-end test framework."
weight: 40
---

# E2E Framework

The E2E framework is intentionally smaller than the OpenBao Operator framework, but it follows the same direction: Ginkgo and Gomega specs, label-based routing, a suite manifest, pinned version policy, and machine-readable reports.

## Goals

- keep unit and hermetic integration tests free of external services,
- provide PR-capable ephemeral OpenBao `2.5.3` CI coverage, provider full-stack OpenBao and KMS v2 socket coverage, provider failure-mode coverage, decrypt storm smoke, provider load-soak, provider backend replacement and raft restore coverage, provider Transit rotation coverage, provider binary upgrade and rollback coverage, pinned Kind KMS v2 smoke and convergence lanes, static-pod upgrade and rollback coverage, and a Kind DR runbook lane,
- avoid duplicated OpenBao and Kubernetes versions across workflows,
- produce JUnit and Ginkgo JSON reports for CI summaries and release evidence.

## Test Layers

| Layer | Command | External dependency |
|---|---|---|
| Unit | `go test ./...` | None |
| Hermetic OpenBao integration | `go test -tags=integration ./internal/openbao` | None |
| E2E | `make test-e2e` | Depends on selected labels |
| OpenBao CI E2E | `make test-e2e-openbao-ci` | Docker-compatible container runtime |
| Provider OpenBao and KMS v2 full-stack E2E | `make test-e2e-provider-openbao-ci` | Docker-compatible container runtime |
| Provider OpenBao failure E2E | `make test-e2e-provider-failure-openbao-ci` | Docker-compatible container runtime |
| Provider decrypt storm E2E | `make test-e2e-provider-decrypt-storm-openbao-ci` | Docker-compatible container runtime |
| Provider load-soak E2E | `make test-e2e-provider-load-soak-openbao-ci` | Docker-compatible container runtime |
| Provider backend replacement and raft restore E2E | `make test-e2e-provider-restore-openbao-ci` | Docker-compatible container runtime |
| Provider Transit rotation E2E | `make test-e2e-provider-rotation-openbao-ci` | Docker-compatible container runtime |
| Provider binary upgrade and rollback E2E | `make test-e2e-provider-upgrade-rollback-openbao-ci` | Docker-compatible container runtime |
| Kind KMS v2 smoke E2E | `make test-e2e-kind-smoke` | Docker-compatible container runtime, Kind, kubectl |
| Kind multi-control-plane convergence E2E | `make test-e2e-kind-convergence` | Docker-compatible container runtime, Kind, kubectl |
| Kind static-pod upgrade and rollback E2E | `make test-e2e-kind-upgrade-rollback` | Docker-compatible container runtime, Kind, kubectl |
| Kind DR restore runbook E2E | `make test-e2e-kind-dr-runbook` | Docker-compatible container runtime, Kind, kubectl |

The OpenBao CI lane starts an owned OpenBao container, enables Transit, creates the test key, disables Transit upsert, configures a least-privilege policy, bootstraps JWT auth with a generated RS256 test issuer, and runs Transit assertions without external OpenBao credentials.

The provider full-stack slice builds the provider image, runs the provider container on a Docker network with OpenBao, shares the Unix socket through a Docker volume, and runs a containerized Kubernetes KMS v2 client against that socket.

The provider failure slice reuses the same real OpenBao, provider, and KMS v2 socket path for OpenBao down, OpenBao sealed, reduced policy, expired JWT, JWT file rotation, missing Transit key, Status staleness, and stale socket reclamation cases. It stores only ciphertext, `key_id`, and annotations between phases.

The decrypt storm slice performs concurrent KMS v2 decrypts through the provider against real OpenBao.

The load-soak slice runs sustained Status, Encrypt, and Decrypt traffic through the real provider and OpenBao path. It checks zero client errors, p95 latency, Docker memory growth, and provider PID growth.

The restore slice runs OpenBao with integrated raft storage, saves a raft snapshot, replaces or restores the backend, and decrypts ciphertext written before the outage or restore.

The rotation slice writes ciphertext on the initial Transit version, saves a pre-rotation raft snapshot, rotates the Transit key, waits for provider Status to promote a new `key_id`, verifies old and new ciphertext decrypt, restores the pre-rotation snapshot, and verifies the provider rejects the observed Transit version rollback.

The provider binary upgrade and rollback slice builds distinct old and new provider images, runs old to new to old over the same provider state volume, and verifies ciphertext from both sides of the transition remains decryptable.

The Kind smoke lane creates a pinned Kubernetes `1.34.3` cluster, runs OpenBao on the Kind Docker network, deploys the provider as a static pod, configures kube-apiserver KMS v2 encryption, verifies Secret readback, checks raw etcd storage uses the `k8s:enc:kms:v2:` envelope, restarts kube-apiserver, and reads the Secret again.

The Kind convergence lane creates a three-control-plane Kind cluster, stages one provider static pod per control-plane node, verifies every stacked etcd member stores the Secret with a KMS v2 envelope, temporarily holds non-target API server manifests so each kube-apiserver must decrypt as the only serving API endpoint, then restarts every API server and verifies readback.

The Kind upgrade and rollback lane mutates and restores the provider static pod manifest, proving kubelet restarts do not break decrypt of existing data.

The Kind DR runbook lane restores an OpenBao raft snapshot into a fresh volume, rehydrates provider configuration, TLS, JWT, and registry state on the control-plane node, restarts the provider and API server, and verifies Kubernetes Secret readback after replacement.

## Layout

```text
test/e2e/
  e2e_suite_test.go
  kind_dr_test.go
  openbao_transit_test.go
  kind_smoke_test.go
  suites.yaml
  suites_manifest_test.go
  provider_container_test.go
  provider_failure_test.go
  provider_load_test.go
  provider_rotation_test.go
  provider_upgrade_test.go
  provider_restore_test.go
  kmsclient/
    main.go
  framework/
    artifacts.go
    env.go
    labels.go
    openbao_environment.go
```

The root `test/e2e` package is the primary suite package. New Ginkgo E2E specs are added to this package unless there is a concrete reason to split binaries. Container-only helpers (such as the KMS v2 socket client) live under `test/e2e`.

Shared helpers live under `test/e2e/framework`. Helpers stay small and explicit; broad test-harness abstractions wait until multiple specs need them.

## Suite Manifest

`test/e2e/suites.yaml` describes E2E lanes. It does not own concrete OpenBao or Kubernetes versions. Those remain in `.ci/versions.yaml`, and lanes reference the relevant fields through `versionRefs`.

Current lanes:

| Lane | Status | Purpose |
|---|---|---|
| `openbao-ci` | active | Run OpenBao Transit, JWT auth, and provider KMS v2 socket coverage against ephemeral OpenBao `2.5.3` CI environments. |
| `kind-smoke` | active | Validate KMS v2 encryption with a real pinned Kind API server. |
| `kind-convergence` | active | Validate multi-control-plane KMS v2 convergence with real pinned Kind API servers. |
| `openbao-failure-ci` | active | Validate fail-closed behavior for OpenBao, policy, JWT, Transit key, and Status staleness failure modes. |
| `openbao-decrypt-storm-ci` | active | Exercise concurrent provider decrypts against real OpenBao as a smoke test. |
| `openbao-load-soak-ci` | active | Validate sustained Status, Encrypt, and Decrypt traffic with latency and resource checks. |
| `openbao-restore-ci` | active | Validate provider recovery after backend replacement and OpenBao integrated raft snapshot restore. |
| `openbao-rotation-ci` | active | Validate Transit rotation promotion, old and new decrypt compatibility, and rollback rejection. |
| `openbao-provider-upgrade-rollback-ci` | active | Validate provider binary upgrade and rollback decrypt compatibility. |
| `kind-upgrade-rollback` | active | Validate static-pod provider upgrade and rollback behavior in pinned Kind. |
| `kind-dr-runbook` | active | Validate Kubernetes readback after OpenBao raft restore and provider state and configuration rehydration. |
| `release-gate` | planned | Collect the pinned v0.1 OpenBao and Kubernetes E2E checks. |

Manifest validation:

```sh
make verify-e2e-manifest
```

The validation checks schema shape, required fields, lane IDs, status values, and the absence of floating `latest` references.

## Labels

Labels are the routing API. Prefer small composable labels:

```go
Label("openbao", "transit", "ci")
Label("kind", "kmsv2", "smoke")
Label("kind", "kmsv2", "convergence")
Label("openbao", "kmsv2", "restore", "ci")
Label("openbao", "kmsv2", "rotation", "ci")
Label("openbao", "kmsv2", "soak", "ci")
Label("openbao", "kmsv2", "upgrade", "rollback", "ci")
Label("kind", "kmsv2", "restore", "dr")
Label("kind", "kmsv2", "upgrade", "rollback")
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

Each Make target listed in the test layers table runs a focused lane. The provider lanes build `E2E_PROVIDER_IMAGE` first by default; set `E2E_PROVIDER_BUILD=false` to test a prebuilt image tag.

## Environment

| Variable | Default | Purpose |
|---|---|---|
| `E2E_OPENBAO_CI` | set by `make test-e2e-openbao-ci` | Enables the PR-capable OpenBao CI spec. |
| `E2E_OPENBAO_IMAGE` | `validation.openbao.image` from `.ci/versions.yaml` | OpenBao image used by the CI environment. |
| `E2E_KIND_NODE_IMAGE` | `validation.kubernetes.kindNodeImage` from `.ci/versions.yaml` | Kind node image used by the Kubernetes smoke lane. |
| `E2E_PROVIDER_IMAGE` | `ghcr.io/dc-tec/bao-kms-provider:e2e-<commit>` | Provider image loaded into Kind or run in Docker full-stack tests. |
| `E2E_PROVIDER_OLD_IMAGE` | `ghcr.io/dc-tec/bao-kms-provider:e2e-upgrade-old-<commit>` | Old provider image used by the binary upgrade and rollback lane. |
| `E2E_PROVIDER_NEW_IMAGE` | `ghcr.io/dc-tec/bao-kms-provider:e2e-upgrade-new-<commit>` | New provider image used by the binary upgrade and rollback lane. |
| `E2E_PROVIDER_BUILD` | `true` | Set to `false` to use a prebuilt provider image. |
| `DOCKER` | `docker` | Container runtime CLI compatible with Docker commands. |
| `E2E_SKIP_CLEANUP` | `false` | Keeps generated OpenBao TLS files for debugging. |

## Reports And Artifacts

| Variable | Default |
|---|---|
| `E2E_ARTIFACT_DIR` | `artifacts/e2e` |
| `E2E_JUNIT_REPORT` | `artifacts/e2e/junit.xml` |
| `E2E_JSON_REPORT` | `artifacts/e2e/ginkgo.json` |
| `E2E_TIMEOUT` | `30m` |
| `E2E_PARALLEL_NODES` | `1` |

E2E logs are redacted. The framework never writes OpenBao tokens, JWTs, plaintext, or full ciphertext to artifacts.

## Parallelism

Default parallelism is one node. Increase `E2E_PARALLEL_NODES` only for lanes whose environments are isolated per process. Shared socket paths and single Kind clusters remain single-node until the environment code proves isolation.
