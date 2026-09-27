---
title: E2E framework
description: "Every end-to-end lane and its make target, how labels and the suite manifest select lanes, the environment variables, and where reports land."
eyebrow: Contribute
weight: 40
verifiedBy:
  - test/e2e/suites.yaml
  - mk/e2e.mk
  - hack/tools/e2e_release_gate
---

End-to-end (E2E) lanes run Ginkgo specs against real OpenBao, the provider
container, Kind, and `kube-apiserver`, with versions pinned in
`.ci/versions.yaml`. Unit and hermetic integration tests never use external
services. Every lane needs a Docker-compatible runtime; Kind lanes also need
Kind and kubectl.

## Lanes

| Lane | Command | Proves |
|---|---|---|
| OpenBao | `make test-e2e-openbao-ci` | Transit, JWT auth, least-privilege policy, and the primary pinned OpenBao version. |
| OpenBao cert auth | `make test-e2e-cert-auth-openbao-ci` | TLS cert auth with a URI SAN role binding, login, and Transit access. |
| PKCS#11 source | `make test-e2e-provider-certauth-pkcs11-openbao-ci` | SoftHSM token, session pool timeout and recovery under the race detector, PKCS#11 signer, cert login, and KMS v2 through the provider. |
| Certificate sources | `make test-e2e-provider-certauth-sources-openbao-ci` | The supported PKCS#11 source lane. |
| SPIRE source | `make test-e2e-provider-certauth-spiffe-openbao-ci` | Local implementation check only; not in CI or the release gate. |
| Provider full stack | `make test-e2e-provider-openbao-ci` | Provider image, Unix socket, KMS v2 client, Transit, and auth. |
| Provider CLI | `make test-e2e-provider-cli-openbao-ci` | CLI diagnostics and hardening failures against real OpenBao and state. |
| Provider failure | `make test-e2e-provider-failure-openbao-ci` | OpenBao down or sealed, token revocation and recovery, bad policy, bad auth material, missing key, stale Status, and stale sockets. |
| OpenBao HA | `make test-e2e-provider-ha-openbao-ci` | Raft active-node failover with old decrypts and new operations. |
| Decrypt storm | `make test-e2e-provider-decrypt-storm-openbao-ci` | Concurrent decrypts through the provider. |
| Decrypt soak | `make test-e2e-provider-decrypt-soak-openbao-ci` | Sustained decrypt latency, errors, memory, and process growth. |
| Load soak | `make test-e2e-provider-load-soak-openbao-ci` | Sustained Status, Encrypt, and Decrypt with resource checks. |
| OpenBao restore | `make test-e2e-provider-restore-openbao-ci` | Backend replacement and raft snapshot restore with old ciphertext readback. |
| Transit rotation | `make test-e2e-provider-rotation-openbao-ci` | Two-node promotion, pending decrypt and discovery, minimum versions, retirement, writer lock, and rollback rejection. |
| Upgrade and rollback | `make test-e2e-provider-upgrade-rollback-openbao-ci` | Old and new images over one state volume. |
| Kind smoke | `make test-e2e-kind-smoke` | Real API server encryption, raw etcd envelopes, restart, and readback. |
| Kind OAuth | `make test-e2e-kind-oauth2` | Pinned Keycloak, OpenBao OIDC discovery, generated provider static pod, both client authentication methods, secret rotation, issuer outage, and recovery while the protected API is stopped. Auth-failure waits include the full maximum token TTL, two probe intervals, two login timeouts, and five seconds of scheduling margin. Status, Encrypt, and Decrypt rejection assertions remain required. |
| Kind convergence | `make test-e2e-kind-convergence` | Three API servers converge through node-local providers. |
| Kind upgrade | `make test-e2e-kind-upgrade-rollback` | Static pod upgrade and rollback with old Secret readback. |
| Kind DR runbook | `make test-e2e-kind-dr-runbook` | Raft restore, provider state rehydration, API server restart, and readback. |

`make test-e2e` runs every enabled spec selected by labels. Soak lanes are
evidence for the pinned CI environment, not capacity or SLO claims. kubeadm VM
validation of boot ordering, reboots, paired restore, and multi-control-plane
recovery is a release-candidate gate outside public CI.

## Labels

Labels route specs to lanes. Keep them small and composable, such as
`Label("openbao", "kmsv2", "rotation", "ci")` or `Label("kind", "kmsv2", "smoke")`,
and add a stable `case:<id>` label when a spec becomes release evidence or a
regression target:

```sh
make test-e2e E2E_LABEL_FILTER='openbao && transit && ci'
```

## Suite manifest and release gate

`test/e2e/suites.yaml` defines each lane's selectors, timeout, environment,
reports, and the preview release gate groups. Versions stay in
`.ci/versions.yaml` and are referenced through `versionRefs`.

The release workflow runs only the aggregate gates:

```sh
make test-e2e-release-preview-openbao
make test-e2e-release-preview-kind
```

They build the images once, then run the lanes listed under
`releaseGate.preview.groups`. The Kind gate runs once per
`validation.kubernetes.previewMatrix` entry with `releaseGate: true`; set
`E2E_KUBERNETES_LINE=1.34` to run one line. To debug one lane through the
same runner:

```sh
E2E_PROVIDER_IMAGE=ghcr.io/dc-tec/bao-kms-provider:e2e-local \
  go run ./hack/tools/e2e_release_gate -group openbao -lane openbao-ha-ci
```

`make verify-e2e-manifest` checks the manifest schema, lane IDs, release gate
references and workflow wiring, make targets, and floating `latest` references.

## Environment

| Variable | Default | Purpose |
|---|---|---|
| `E2E_OPENBAO_IMAGE` | `validation.openbao.image` | Digest-pinned OpenBao image. |
| `E2E_KEYCLOAK_IMAGE` | `validation.keycloak.image` | Digest-pinned Keycloak image for the OAuth lane. |
| `E2E_KIND_NODE_IMAGE` | `validation.kubernetes.kindNodeImage` | Kind node image; the Kind gate sets it per matrix line. |
| `E2E_KUBERNETES_LINE` | unset | Run the Kind gate for one Kubernetes line. |
| `E2E_PROVIDER_IMAGE` | `ghcr.io/dc-tec/bao-kms-provider:e2e-<commit>` | Provider image under test. |
| `E2E_PROVIDER_OLD_IMAGE`, `E2E_PROVIDER_NEW_IMAGE` | `…:e2e-upgrade-old-<commit>`, `…:e2e-upgrade-new-<commit>` | Images for upgrade and rollback lanes. |
| `E2E_PROVIDER_BUILD` | `true` | `false` reuses prebuilt images. |
| `DOCKER` | `docker` | Container runtime CLI. |
| `E2E_SKIP_CLEANUP` | `false` | Keep containers and TLS files for debugging. |
| `E2E_TIMEOUT`, `E2E_PARALLEL_NODES` | `30m`, `1` | Suite timeout and Ginkgo parallelism. Raise parallelism only for lanes with isolated environments. |

## Reports

Generic runs write `artifacts/e2e/junit.xml` and `artifacts/e2e/ginkgo.json`
(`E2E_ARTIFACT_DIR`, `E2E_JUNIT_REPORT`, `E2E_JSON_REPORT`). Release gate lanes
write to `artifacts/e2e/<group>/<lane-id>/`, or
`artifacts/e2e/kind/<kubernetes-line>/<lane-id>/` for Kind, with a
`console.log` and, for Ginkgo lanes, JUnit and JSON reports. Artifacts never
contain tokens, JWTs, plaintext, or full ciphertext.

New specs go in the `test/e2e` package, and shared helpers in
`test/e2e/framework`, kept small until several specs need them.

## Candidate qualification matrix

`make test-e2e-release-preview-openbao` runs every OpenBao row in
`.ci/versions.yaml`. Set `E2E_OPENBAO_VERSION=2.6.3` or `2.7.0` to select one.
`make test-e2e-release-preview-kind` runs the Kubernetes rows against the primary
OpenBao version. Set `E2E_KUBERNETES_LINE` to select one Kubernetes minor.

Reports include console output for each version and lane. Go test lanes use
JSON events and fail if no tests pass or a selected test skips. Ginkgo lanes
fail when the selection is empty. Version pins and the provider commit identify
the run; no additional provenance service is needed.

Fresh Harvester lab installations use Raft storage. Existing file-backed lab
data requires a separate migration before using OpenBao 2.7; changing the
storage stanza does not migrate data.

## Upgrade and soak qualification

The provider upgrade lane pulls the published image pinned by
`validation.provider.upgradeBaselineImage`. It reuses the same configuration,
registry, and checkpoint through upgrade and rollback, and checks reads through
a peer running the old release. The Kind lane changes the actual static-pod
image and restarts the API server after each transition before reading Secrets.

Run `make test-e2e-provider-openbao-upgrade` to upgrade the pinned OpenBao
compatibility version to the primary version with its Raft data intact. This
checks historical and new ciphertext again after a provider restart. It does
not qualify an OpenBao binary downgrade or a rolling server-cluster upgrade.

For a longer load run, set `E2E_SOAK_DURATION=30m` and run
`make test-e2e-provider-load-soak-openbao-ci`. The same provider stays running
while bounded client windows report latency, errors, memory, and process counts.
Tokens have a 30-second TTL and a two-minute maximum lifetime to exercise renewal
and reauthentication. Set `E2E_SOAK_TIMEOUT` above the requested duration plus
six minutes; its default is 40 minutes. For example, an overnight run uses
`E2E_SOAK_DURATION=8h E2E_SOAK_TIMEOUT=8h10m`.

The HA lane sends Status, encrypt, and historical/new decrypt requests during
active-node failure. It permits transient availability errors for at most 20
seconds between successful rounds and requires healthy traffic at the end.
The bound is a test acceptance criterion, not an availability guarantee.

## Pre-release qualification workflow

Run **Provider Qualification** manually against the candidate branch or commit.
The workflow also runs when a pull request changes its definition. It builds one
JWT provider image and one PKCS#11 test image, then reuses those images across:

- The full provider suite on both pinned OpenBao versions.
- The full Kind suite on all four Kubernetes versions with primary OpenBao.
- Kind smoke and provider upgrade/rollback on all four Kubernetes versions with
  compatibility OpenBao.
- The OpenBao server upgrade and a 30-minute load soak on primary OpenBao.

The workflow summary records the provider commit and image IDs. Test artifacts
contain versions, executed tests, failures, and latency/resource measurements.
The matrix does not establish Kubernetes cluster-upgrade support. The extended
VM systemd/static-pod recovery campaign and overnight soak remain separate
qualification steps before the release decision.
