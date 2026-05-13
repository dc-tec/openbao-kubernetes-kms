# OpenBao Kubernetes KMS Dev Environment

This directory contains an interactive local validation environment for
`bao-kms-provider`. It uses upstream Kubernetes through Kind, external OpenBao
through Docker Compose, and OpenTofu for OpenBao-side configuration.

The lab is for development and controlled local validation. It is not release
evidence by itself and does not replace the E2E release gates.

## What It Creates

- a single-control-plane Kind cluster using the pinned Kind node image from
  `.ci/versions.yaml`,
- a local single-node OpenBao server on the Kind Docker network, using raft
  storage, generated TLS, and a local static seal,
- OpenBao Transit and provider auth configured through OpenTofu,
- a kubelet-managed provider static pod on the Kind control-plane node,
- a Kubernetes `EncryptionConfiguration` that points `kube-apiserver` at the
  provider Unix socket,
- Prometheus and Grafana for local metric inspection.

Generated identity material, OpenBao TLS material, OpenTofu state, and rendered
provider files live under `.state/`, which is ignored by git.

## Prerequisites

Install these tools locally:

- Docker with Compose v2,
- Kind,
- kubectl,
- OpenTofu,
- OpenSSL.

## Run

From the repository root:

```sh
make dev-env-up
```

This performs the full JWT-auth flow by default:

1. Generate a local JWT signer and provider JWT.
2. Create the Kind cluster.
3. Start OpenBao, Prometheus, and Grafana.
4. Build and load the provider image into Kind.
5. Configure OpenBao with OpenTofu.
6. Stage the provider static pod and encryption config.
7. Patch `kube-apiserver` with the KMS config.
8. Verify Secret readback and raw etcd KMS v2 envelope storage.

To exercise the PKCS#11 certificate-auth path with SoftHSM:

```sh
make dev-env-reset
make dev-env-up AUTH=pkcs11
```

That mode builds the PKCS#11-enabled provider image, generates a SoftHSM token
and client certificate, configures OpenBao cert auth with a URI SAN binding, and
stages the HSM material into the Kind control-plane node for the static pod.

Open Grafana at:

```text
http://127.0.0.1:18300
```

Username and password are both `admin`. The dev environment provisions the
dashboard from `deploy/grafana/dashboards/openbao-kms-overview.json`, so local
Grafana uses the same dashboard sample shipped with the deployment artifacts.

## Useful Targets

```sh
make dev-env-generate
make dev-env-kind
make dev-env-compose-up
make dev-env-openbao
make dev-env-build
make dev-env-pkcs11-material AUTH=pkcs11
make dev-env-tofu
make dev-env-stage
make dev-env-enable-kms
make dev-env-verify
make dev-env-down
make dev-env-reset
```

`dev-env-down` keeps `.state/` so you can inspect generated files or rerun
steps. `dev-env-reset` deletes the generated state.

## Notes

- The provider metrics and health endpoints bind to `0.0.0.0` in this lab so
  Prometheus can scrape the Kind control-plane node. Production samples remain
  localhost-bound.
- The OpenBao root token, static seal key, TLS keys, JWT, and SoftHSM material
  are local-only and stored under ignored state.
- The Kind cluster must be created before Compose starts because Compose joins
  the external Docker network named `kind`.
- The OpenBao shape intentionally follows the local server-mode pattern used by
  `openbao-demo`, but remains single-node so KMS provider iteration stays fast.
