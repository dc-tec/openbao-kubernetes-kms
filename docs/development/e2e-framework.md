---
title: "E2E Framework"
description: "Runnable E2E lanes, label routing, suite manifest, environment variables, reports, and artifacts for bao-kms-provider."
weight: 40
---

# E2E Framework

The E2E framework is the command reference for integration and end-to-end
validation. It uses Ginkgo and Gomega specs, label-based routing, a suite
manifest, pinned versions from `.ci/versions.yaml`, and machine-readable JUnit
and Ginkgo JSON reports.

Unit tests and hermetic integration tests should stay free of external services.
E2E lanes own real OpenBao, provider container, Kind, and Kubernetes API server
behavior.

## Command Matrix

| Lane | Command | Proves | External dependency |
|---|---|---|---|
| Full enabled E2E suite | `make test-e2e` | Runs the enabled Ginkgo E2E specs selected by labels. | Depends on selected labels |
| OpenBao CI | `make test-e2e-openbao-ci` | Transit, provider auth, least-privilege policy, and OpenBao `2.5.3` behavior. | Docker-compatible runtime |
| Provider full stack | `make test-e2e-provider-openbao-ci` | Provider image, real Unix socket, KMS v2 client, OpenBao Transit, and provider auth. | Docker-compatible runtime |
| Provider CLI | `make test-e2e-provider-cli-openbao-ci` | Provider image CLI commands against real OpenBao/config/state, including diagnostics and hardening failures. | Docker-compatible runtime |
| Provider failure | `make test-e2e-provider-failure-openbao-ci` | OpenBao down or sealed, bad policy, expired or identity-drifted auth material, missing Transit key, Status staleness, and stale socket cleanup. | Docker-compatible runtime |
| OpenBao HA failover | `make test-e2e-provider-ha-openbao-ci` | Integrated-raft active node failover while preserving old decrypt and new KMS operations. | Docker-compatible runtime |
| Decrypt storm smoke | `make test-e2e-provider-decrypt-storm-openbao-ci` | Concurrent KMS v2 decrypts through the provider and real OpenBao. | Docker-compatible runtime |
| Sustained direct decrypt soak | `make test-e2e-provider-decrypt-soak-openbao-ci` | Direct decrypt latency, error bounds, memory growth, and PID growth. | Docker-compatible runtime |
| Provider load soak | `make test-e2e-provider-load-soak-openbao-ci` | Sustained Status, Encrypt, and Decrypt traffic with latency and resource checks. | Docker-compatible runtime |
| OpenBao restore | `make test-e2e-provider-restore-openbao-ci` | Backend replacement and integrated-raft snapshot restore with old ciphertext readback. | Docker-compatible runtime |
| Transit rotation | `make test-e2e-provider-rotation-openbao-ci` | Key version promotion, old and new ciphertext decrypt, historical decryptability enforcement, missing-state fail-closed behavior, and observed rollback rejection. | Docker-compatible runtime |
| Provider upgrade and rollback | `make test-e2e-provider-upgrade-rollback-openbao-ci` | Old and new provider images over the same state volume preserve decrypt compatibility. | Docker-compatible runtime |
| Kind smoke | `make test-e2e-kind-smoke` | Real API server KMS v2 encryption, raw etcd envelope storage, API server restart, and readback. | Docker-compatible runtime, Kind, kubectl |
| Kind convergence | `make test-e2e-kind-convergence` | Three control-plane API servers decrypt through node-local providers and converge on KMS state. | Docker-compatible runtime, Kind, kubectl |
| Kind static-pod upgrade | `make test-e2e-kind-upgrade-rollback` | Static-pod provider manifest upgrade and rollback preserves old Secret readback. | Docker-compatible runtime, Kind, kubectl |
| Kind DR runbook | `make test-e2e-kind-dr-runbook` | OpenBao raft restore, provider state and config rehydration, API server restart, and Secret readback. | Docker-compatible runtime, Kind, kubectl |

Local kubeadm VM validation is a release-candidate gate, not public CI. It
exercises host boot ordering, systemd, static-pod behavior, node reboot, paired
restore, and multi-control-plane recovery in a VM substrate. Infrastructure-specific
maintainer notes live next to the local harness implementation.

## Labels

Labels are the routing API. Prefer small composable labels:

```go
Label("openbao", "transit", "ci")
Label("openbao", "kmsv2", "failure", "ci")
Label("openbao", "kmsv2", "ha", "ci")
Label("openbao", "kmsv2", "restore", "ci")
Label("openbao", "kmsv2", "rotation", "ci")
Label("openbao", "kmsv2", "soak", "ci")
Label("openbao", "kmsv2", "upgrade", "rollback", "ci")
Label("kind", "kmsv2", "smoke")
Label("kind", "kmsv2", "convergence")
Label("kind", "kmsv2", "restore", "dr")
Label("kind", "kmsv2", "upgrade", "rollback")
Label("release-gate")
```

Use stable `case:<id>` labels when a spec becomes release evidence or a known
regression target.

Run a label-filtered suite when Ginkgo is installed:

```sh
make test-e2e E2E_LABEL_FILTER='openbao && transit && ci'
```

## Suite Manifest

`test/e2e/suites.yaml` describes E2E lanes. It does not own concrete OpenBao or
Kubernetes versions. Those remain in `.ci/versions.yaml`, and lanes reference
the relevant fields through `versionRefs`.

Current lane IDs:

| Lane ID | Status |
|---|---|
| `openbao-ci` | active |
| `openbao-failure-ci` | active |
| `openbao-provider-cli-ci` | active |
| `openbao-ha-ci` | active |
| `openbao-decrypt-storm-ci` | active |
| `openbao-decrypt-soak-ci` | active |
| `openbao-load-soak-ci` | active |
| `openbao-restore-ci` | active |
| `openbao-rotation-ci` | active |
| `openbao-provider-upgrade-rollback-ci` | active |
| `kind-smoke` | active |
| `kind-convergence` | active |
| `kind-upgrade-rollback` | active |
| `kind-dr-runbook` | active |
| `release-gate` | planned |

Manifest validation:

```sh
make verify-e2e-manifest
```

The validation checks schema shape, required fields, lane IDs, status values,
and the absence of floating `latest` references.

## Layout

```text
test/e2e/
  e2e_suite_test.go
  kind_dr_test.go
  kind_smoke_test.go
  openbao_transit_test.go
  provider_cli_test.go
  provider_container_test.go
  provider_failure_test.go
  provider_ha_test.go
  provider_load_test.go
  provider_restore_test.go
  provider_rotation_test.go
  provider_upgrade_test.go
  suites.yaml
  suites_manifest_test.go
  kmsclient/
    main.go
  framework/
    artifacts.go
    env.go
    labels.go
    openbao_environment.go
    openbao_ha_environment.go
```

The root `test/e2e` package is the primary suite package. New Ginkgo E2E specs
are added to this package unless there is a concrete reason to split binaries.
Container-only helpers, such as the KMS v2 socket client, live under
`test/e2e`.

Shared helpers live under `test/e2e/framework`. Helpers stay small and explicit;
broad harness abstractions wait until multiple specs need them.

## Environment

| Variable | Default | Purpose |
|---|---|---|
| `E2E_OPENBAO_CI` | set by `make test-e2e-openbao-ci` | Enables the PR-capable OpenBao CI spec. |
| `E2E_OPENBAO_IMAGE` | `validation.openbao.image` from `.ci/versions.yaml` | OpenBao image used by CI E2E environments. |
| `E2E_KIND_NODE_IMAGE` | `validation.kubernetes.kindNodeImage` from `.ci/versions.yaml` | Kind node image used by Kubernetes lanes. |
| `E2E_PROVIDER_IMAGE` | `ghcr.io/dc-tec/bao-kms-provider:e2e-<commit>` | Provider image loaded into Kind or run in Docker full-stack tests. |
| `E2E_PROVIDER_OLD_IMAGE` | `ghcr.io/dc-tec/bao-kms-provider:e2e-upgrade-old-<commit>` | Old provider image used by upgrade and rollback tests. |
| `E2E_PROVIDER_NEW_IMAGE` | `ghcr.io/dc-tec/bao-kms-provider:e2e-upgrade-new-<commit>` | New provider image used by upgrade and rollback tests. |
| `E2E_PROVIDER_BUILD` | `true` | Set to `false` to use a prebuilt provider image. |
| `DOCKER` | `docker` | Container runtime CLI compatible with Docker commands. |
| `E2E_SKIP_CLEANUP` | `false` | Keeps generated OpenBao TLS files and containers for debugging. |

The provider lanes build `E2E_PROVIDER_IMAGE` by default. Use
`E2E_PROVIDER_BUILD=false` only when the referenced image is already built and
available to the selected runtime.

## Reports And Artifacts

| Variable | Default |
|---|---|
| `E2E_ARTIFACT_DIR` | `artifacts/e2e` |
| `E2E_JUNIT_REPORT` | `artifacts/e2e/junit.xml` |
| `E2E_JSON_REPORT` | `artifacts/e2e/ginkgo.json` |
| `E2E_TIMEOUT` | `30m` |
| `E2E_PARALLEL_NODES` | `1` |

E2E logs are redacted. The framework never writes OpenBao tokens, JWTs,
plaintext, or full ciphertext to artifacts.

## Parallelism

Default parallelism is one node. Increase `E2E_PARALLEL_NODES` only for lanes
whose environments are isolated per process. Shared socket paths and single Kind
clusters remain single-node until the environment code proves isolation.
