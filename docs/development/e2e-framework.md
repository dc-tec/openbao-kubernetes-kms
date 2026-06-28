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
| Preview release OpenBao gate | `make test-e2e-release-preview-openbao` | Runs the manifest-defined OpenBao release gate group from `test/e2e/suites.yaml`. | Docker-compatible runtime |
| Preview release Kind gate | `make test-e2e-release-preview-kind` | Runs the manifest-defined Kind release gate group from `test/e2e/suites.yaml`. | Docker-compatible runtime, Kind, kubectl |
| OpenBao CI | `make test-e2e-openbao-ci` | Transit, provider auth, least-privilege policy, and OpenBao `2.5.5` behavior. | Docker-compatible runtime |
| OpenBao certificate auth | `make test-e2e-cert-auth-openbao-ci` | OpenBao TLS cert auth method, listener client-certificate request, URI SAN role binding, cert login, and Transit access with the issued token. | Docker-compatible runtime |
| Provider SPIRE certificate source | `make test-e2e-provider-certauth-spiffe-openbao-ci` | Explicit implementation check for real SPIRE server and agent, Workload API socket, X.509 SVID selection, and provider local SPIFFE certificate validation. This lane is not wired into CI or the preview release gate. | Docker-compatible runtime |
| Provider PKCS#11 SoftHSM certificate source | `make test-e2e-provider-certauth-pkcs11-openbao-ci` | Real SoftHSM token, PKCS#11 signer, provider image, OpenBao cert login, KMS v2 socket client, and Transit access. | Docker-compatible runtime |
| Provider certificate sources | `make test-e2e-provider-certauth-sources-openbao-ci` | Runs the supported PKCS#11 SoftHSM source lane. SPIRE remains explicit local implementation coverage only. | Docker-compatible runtime |
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
Label("openbao", "certauth", "ci")
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
```

Use stable `case:<id>` labels when a spec becomes release evidence or a known
regression target.

Run a label-filtered suite when Ginkgo is installed:

```sh
make test-e2e E2E_LABEL_FILTER='openbao && transit && ci'
```

## Suite Manifest

`test/e2e/suites.yaml` describes E2E lanes, run selectors, timeouts, required
environment, reports, and the preview release gate groups. It does not own
concrete OpenBao or Kubernetes versions. Those remain in `.ci/versions.yaml`,
and lanes reference the relevant fields through `versionRefs`.

The release workflow calls only the aggregate preview gate targets:

```sh
make test-e2e-release-preview-openbao
make test-e2e-release-preview-kind
```

Those targets prepare the required local images once, then the release-gate
runner reads `releaseGate.preview.groups` from the suite manifest and executes
the listed lanes directly. The per-lane `makeTarget` values remain supported
operator/developer entrypoints, but release evidence must come from the
manifest-defined groups, run selectors, timeouts, and environment. Ginkgo
label lanes constrain Go's test entrypoint to `TestE2E`; plain `testing` lanes
must be selected explicitly with manifest `runRegex`.

The Kind aggregate also reads `validation.kubernetes.previewMatrix` from
`.ci/versions.yaml`. By default it runs the Kind lane group once for every
matrix entry with `releaseGate: true`. Set `E2E_KUBERNETES_LINE=1.34` or
`E2E_KUBERNETES_LINE=1.35` to run one validated line locally. Kubernetes
`1.36` is tracked as the intended next validation line until a digest-pinned
Kind node image is available.

For focused release-gate diagnosis after the required images are available, run
a single lane through the same runner:

```sh
E2E_PROVIDER_IMAGE=ghcr.io/dc-tec/bao-kms-provider:e2e-local \
  go run ./hack/tools/e2e_release_gate -group openbao -lane openbao-ha-ci
```

Soak lanes are release evidence for the pinned CI environment only. They are not
an SLO, capacity, or production performance claim.

The SPIRE certificate-source lane is not part of the CI or preview release gate.
It remains an explicit implementation check only. It does not make
`auth.cert.source: spiffe` a supported user configuration until the supported
OpenBao version can derive cert-auth identity aliases from URI SANs and release
evidence covers that login path.

Current lane IDs:

| Lane ID | Status |
|---|---|
| `openbao-ci` | active |
| `openbao-provider-openbao-ci` | active |
| `openbao-cert-auth-ci` | active |
| `openbao-provider-certauth-spiffe-source-ci` | planned |
| `openbao-provider-certauth-pkcs11-source-ci` | active |
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

Manifest validation:

```sh
make verify-e2e-manifest
```

The validation checks schema shape, required fields, lane IDs, status values,
release-gate references, release workflow wiring, Make target availability, and
the absence of floating `latest` references.

## Layout

```text
test/e2e/
  e2e_suite_test.go
  kind_dr_test.go
  kind_smoke_test.go
  openbao_transit_test.go
  openbao_cert_auth_test.go
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
| `E2E_OPENBAO_IMAGE` | `validation.openbao.image` from `.ci/versions.yaml` | Digest-pinned OpenBao image used by CI E2E environments. |
| `E2E_KUBERNETES_LINE` | unset | Optional release-gate selector for one Kubernetes preview matrix line. |
| `E2E_KIND_NODE_IMAGE` | `validation.kubernetes.kindNodeImage` from `.ci/versions.yaml` | Kind node image used by individual Kubernetes lanes. The Kind release gate overrides this from `validation.kubernetes.previewMatrix`. |
| `E2E_PROVIDER_IMAGE` | `ghcr.io/dc-tec/bao-kms-provider:e2e-<commit>` | Provider image loaded into Kind or run in Docker full-stack tests. |
| `E2E_PROVIDER_OLD_IMAGE` | `ghcr.io/dc-tec/bao-kms-provider:e2e-upgrade-old-<commit>` | Old provider image used by upgrade and rollback tests. |
| `E2E_PROVIDER_NEW_IMAGE` | `ghcr.io/dc-tec/bao-kms-provider:e2e-upgrade-new-<commit>` | New provider image used by upgrade and rollback tests. |
| `E2E_PROVIDER_BUILD` | `true` | Set to `false` to use a prebuilt provider image. |
| `DOCKER` | `docker` | Container runtime CLI compatible with Docker commands. |
| `E2E_SKIP_CLEANUP` | `false` | Keeps generated OpenBao TLS files and containers for debugging. |

Individual provider lanes build their required image by default. The preview
release aggregate targets prebuild the required images once, then run the
manifest-defined lanes with `E2E_PROVIDER_BUILD=false` so the same image tags
are reused across the release gate. Use `E2E_PROVIDER_BUILD=false` directly only
when the referenced images are already built and available to the selected
runtime.

CI builds the default provider image and PKCS#11 validation image once in the
`e2e-provider-images` job, uploads Docker archives, and has the image scan,
OpenBao E2E, and Kind E2E jobs load those exact images with
`E2E_PROVIDER_BUILD=false`. The release workflow pulls the digest produced by
the release build job, tags it locally as `E2E_PROVIDER_IMAGE`, and runs the
preview gate against that immutable build output. OpenBao release validation
still builds local PKCS#11 and upgrade/rollback images because those are
validation-only variants, not public release images.

## Reports And Artifacts

| Variable | Default |
|---|---|
| `E2E_ARTIFACT_DIR` | `artifacts/e2e` |
| `E2E_JUNIT_REPORT` | `artifacts/e2e/junit.xml` |
| `E2E_JSON_REPORT` | `artifacts/e2e/ginkgo.json` |
| `E2E_TIMEOUT` | `30m` |
| `E2E_PARALLEL_NODES` | `1` |

Preview release gates write per-lane artifacts under
`artifacts/e2e/<group>/<lane-id>/` for OpenBao lanes and
`artifacts/e2e/kind/<kubernetes-line>/<lane-id>/` for Kind lanes. Every lane
writes `console.log`. Ginkgo-spec lanes also write `junit.xml` and
`ginkgo.json`; plain `testing` lanes are selected through manifest `runRegex`
and keep their evidence in `console.log`. The shared `E2E_JUNIT_REPORT` and
`E2E_JSON_REPORT` variables apply to the generic `make test-e2e` entrypoint.

E2E logs are redacted. The framework never writes OpenBao tokens, JWTs,
plaintext, or full ciphertext to artifacts.

## Parallelism

Default parallelism is one node. Increase `E2E_PARALLEL_NODES` only for lanes
whose environments are isolated per process. Shared socket paths and single Kind
clusters remain single-node until the environment code proves isolation.
