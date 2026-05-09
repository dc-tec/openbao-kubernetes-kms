# E2E Framework

The E2E framework is intentionally smaller than the OpenBao Operator framework, but it follows the same direction: Ginkgo/Gomega specs, label-based routing, a suite manifest, pinned version policy, and machine-readable reports.

## Goals

- keep unit and hermetic integration tests free of external services,
- provide a PR-capable ephemeral OpenBao `2.5.3` CI environment, provider full-stack OpenBao/KMS v2 socket tests, provider failure-mode coverage, decrypt storm smoke coverage, provider backend replacement and raft restore coverage, pinned Kind KMS v2 smoke and convergence lanes, and static-pod upgrade/rollback coverage,
- avoid duplicated OpenBao and Kubernetes versions across workflows,
- produce JUnit and Ginkgo JSON reports for CI summaries and release evidence.

## Test Layers

| Layer | Command | External dependency |
|---|---|---|
| Unit | `go test ./...` | None |
| Hermetic OpenBao integration | `go test -tags=integration ./internal/openbao` | None |
| E2E | `make test-e2e` | Depends on selected labels |
| OpenBao CI E2E | `make test-e2e-openbao-ci` | Docker-compatible container runtime |
| Provider OpenBao/KMS v2 full-stack E2E | `make test-e2e-provider-openbao-ci` | Docker-compatible container runtime |
| Provider OpenBao failure E2E | `make test-e2e-provider-failure-openbao-ci` | Docker-compatible container runtime |
| Provider decrypt storm E2E | `make test-e2e-provider-decrypt-storm-openbao-ci` | Docker-compatible container runtime |
| Provider backend replacement and raft restore E2E | `make test-e2e-provider-restore-openbao-ci` | Docker-compatible container runtime |
| Kind KMS v2 smoke E2E | `make test-e2e-kind-smoke` | Docker-compatible container runtime, Kind, kubectl |
| Kind multi-control-plane convergence E2E | `make test-e2e-kind-convergence` | Docker-compatible container runtime, Kind, kubectl |
| Kind static-pod upgrade/rollback E2E | `make test-e2e-kind-upgrade-rollback` | Docker-compatible container runtime, Kind, kubectl |

The OpenBao CI lane starts an owned OpenBao container, enables Transit, creates the test key, disables Transit upsert, configures a least-privilege policy, bootstraps JWT auth with a generated RS256 test issuer, and then runs Transit assertions without external OpenBao credentials. The provider full-stack slice builds the provider image, runs the provider container on a Docker network with OpenBao, shares the Unix socket through a Docker volume, and runs a small containerized Kubernetes KMS v2 client against that socket. The provider failure slice reuses the same real OpenBao/provider/KMS v2 socket path for OpenBao down, OpenBao sealed, reduced policy, expired JWT, JWT file rotation, missing Transit key, Status staleness, and stale socket reclamation cases. It stores only ciphertext, key ID, and annotations between phases. The decrypt storm slice performs concurrent KMS v2 decrypts through the provider against real OpenBao. The restore slice runs OpenBao with integrated raft storage, saves a raft snapshot, replaces or restores the backend, and decrypts ciphertext written before the outage or restore. The Kind smoke lane creates a pinned Kubernetes `1.34.3` cluster, runs OpenBao on the Kind Docker network, deploys the provider as a static pod, configures kube-apiserver KMS v2 encryption, verifies Secret readback, checks raw etcd storage uses the `k8s:enc:kms:v2:` envelope, restarts kube-apiserver, and reads the Secret again. The Kind convergence lane creates a three-control-plane Kind cluster, stages one provider static pod per control-plane node, verifies each stacked etcd member stores the Secret with a KMS v2 envelope, temporarily holds non-target API-server manifests so each kube-apiserver must decrypt as the only serving API endpoint, then restarts every API server and verifies readback. The Kind upgrade/rollback lane mutates and restores the provider static pod manifest, proving kubelet restarts do not break decrypt of existing data.

## Layout

```text
test/e2e/
  e2e_suite_test.go
  openbao_transit_test.go
  kind_smoke_test.go
  suites.yaml
  suites_manifest_test.go
  provider_container_test.go
  provider_failure_test.go
  provider_restore_test.go
  kmsclient/
    main.go
  framework/
    artifacts.go
    env.go
    labels.go
    openbao_environment.go
```

The root `test/e2e` package is the primary suite package. New Ginkgo E2E specs should be added to this package unless there is a concrete reason to split binaries. Container-only helpers, such as the KMS v2 socket client, should remain under `test/e2e`.

Shared helpers live under `test/e2e/framework`. Keep helpers small and explicit; broad test harness abstractions should wait until multiple specs need them.

## Suite Manifest

`test/e2e/suites.yaml` describes E2E lanes. It does not own concrete OpenBao or Kubernetes versions. Those remain in `.ci/versions.yaml`, and lanes reference the relevant fields through `versionRefs`.

The current lanes are:

| Lane | Status | Purpose |
|---|---|---|
| `openbao-ci` | active | Run OpenBao Transit, JWT auth, and provider/KMS v2 socket coverage against ephemeral OpenBao `2.5.3` CI environments. |
| `kind-smoke` | active | Validate KMS v2 encryption with a real pinned Kind API server. |
| `kind-convergence` | active | Validate multi-control-plane KMS v2 convergence with real pinned Kind API servers. |
| `openbao-failure-ci` | active | Validate fail-closed behavior for OpenBao, policy, JWT, Transit key, and Status staleness failure modes. |
| `openbao-decrypt-storm-ci` | active | Exercise concurrent provider decrypts against real OpenBao as a smoke test. |
| `openbao-restore-ci` | active | Validate provider recovery after backend replacement and OpenBao integrated raft snapshot restore. |
| `kind-upgrade-rollback` | active | Validate static-pod provider upgrade and rollback behavior in pinned Kind. |
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
Label("kind", "kmsv2", "convergence")
Label("openbao", "kmsv2", "restore", "ci")
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

Run the OpenBao CI lane:

```sh
make test-e2e-openbao-ci
```

`make test-e2e-openbao` is an alias for the OpenBao CI lane.

Run only the provider full-stack OpenBao/KMS v2 socket slice:

```sh
make test-e2e-provider-openbao-ci
```

This target builds `E2E_PROVIDER_IMAGE` first. Set `E2E_PROVIDER_BUILD=false` to test a prebuilt image tag.

Run only the provider/OpenBao failure-mode slice:

```sh
make test-e2e-provider-failure-openbao-ci
```

This target uses the real provider image, real OpenBao Transit and JWT auth bootstrap, and a real KMS v2 gRPC client over the Unix socket. It covers OpenBao down, OpenBao sealed, reduced provider policy, expired JWT startup fail-closed behavior, JWT file rotation and re-login, missing Transit key startup fail-closed behavior, Status staleness, and stale socket reclamation.

Run only the provider decrypt storm smoke slice:

```sh
make test-e2e-provider-decrypt-storm-openbao-ci
```

This target performs concurrent KMS v2 Decrypt calls through the provider against real OpenBao. It is a smoke test, not a replacement for release-candidate load testing.

Run only the provider/OpenBao backend replacement and raft restore slice:

```sh
make test-e2e-provider-restore-openbao-ci
```

This target runs OpenBao with integrated raft storage in Docker. It verifies provider fail-closed behavior while the backend is down, backend replacement under a stable network name, raft snapshot save/restore into a fresh storage volume, and decrypt of ciphertext created before restore.

Run the pinned Kind smoke lane:

```sh
make test-e2e-kind-smoke
```

This target reads `E2E_KIND_NODE_IMAGE` from `.ci/versions.yaml` by default, builds `E2E_PROVIDER_IMAGE`, loads the image into Kind, and runs the Kubernetes API server through the real KMS v2 socket.

Run the pinned Kind multi-control-plane convergence lane:

```sh
make test-e2e-kind-convergence
```

This target uses the same pinned Kind node image and provider image path as the smoke lane, but creates three control-plane nodes and verifies every API server can decrypt through its node-local provider while it is the only serving API endpoint.

Run the pinned Kind static-pod upgrade/rollback lane:

```sh
make test-e2e-kind-upgrade-rollback
```

This target mutates the provider static pod manifest, waits for kubelet to restart the provider, verifies Secret readback, restores the previous static pod manifest, and verifies readback again after provider and API-server restart.

OpenBao CI environment:

| Variable | Default | Purpose |
|---|---|---|
| `E2E_OPENBAO_CI` | set by `make test-e2e-openbao-ci` | Enables the PR-capable OpenBao CI spec. |
| `E2E_OPENBAO_IMAGE` | `validation.openbao.image` from `.ci/versions.yaml` through Make | OpenBao image used by the CI environment. |
| `E2E_KIND_NODE_IMAGE` | `validation.kubernetes.kindNodeImage` from `.ci/versions.yaml` through Make | Kind node image used by the Kubernetes smoke lane. |
| `E2E_PROVIDER_IMAGE` | `ghcr.io/dc-tec/bao-kms-provider:e2e-<commit>` | Provider image loaded into Kind or run in Docker full-stack tests. |
| `E2E_PROVIDER_BUILD` | `true` | Set to `false` to use a prebuilt provider image. |
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

- Generate CI matrices from `test/e2e/suites.yaml` after the lane set stabilizes.
- Add an E2E report summarizer once Ginkgo JSON reports are uploaded by CI.
- Expand the Kind lane with rotation and failure-injection cases.
