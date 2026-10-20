# OpenBao Kubernetes KMS Dev Environment

The interactive local validation environment for `bao-kms-provider` uses
upstream Kubernetes through Kind, external OpenBao through Docker Compose, and
OpenTofu for OpenBao-side configuration.

The lab is for development and controlled local validation. It is not release
evidence by itself and does not replace the end-to-end (E2E) release gates.

## What It Creates

- a single-control-plane Kind cluster using the pinned Kind node image from
  `.ci/versions.yaml`,
- a local single-node OpenBao server on the Kind Docker network, using raft
  storage, generated Transport Layer Security (TLS) material, and a local
  static seal,
- OpenBao Transit and provider authentication configured through OpenTofu,
- a kubelet-managed provider static pod on the Kind control-plane node,
- a Kubernetes `EncryptionConfiguration` that points `kube-apiserver` at the
  provider Unix socket,
- Prometheus and Grafana for local metric inspection.
- with `AUTH=oauth2`, a Keycloak issuer on the Kind Docker network that issues
  the provider's JWT through OAuth 2.0 client credentials.

Generated identity material, OpenBao TLS material, OpenTofu state, and rendered
provider files live under `.state/`, which is ignored by git.

## Prerequisites

Install these tools locally:

- Docker with Compose v2,
- Kind,
- kubectl,
- OpenTofu,
- OpenSSL,
- curl.

## Run

From the repository root:

```sh
make dev-env-up
```

This target performs the full JSON Web Token (JWT) authentication flow by
default:

1. Generate a local JWT signer and provider JWT.
2. Create the Kind cluster.
3. Start OpenBao, Prometheus, and Grafana.
4. Build and load the provider image into Kind.
5. Configure OpenBao with OpenTofu.
6. Generate the installation files with `init --model static-pod` and run the
   generated `node-setup.sh` phases in the control-plane node. `make dev-env-stage`
   expects a fresh node; run `make dev-env-reset` before staging again.
7. Patch `kube-apiserver` with the KMS config.
8. Verify Secret readback and raw etcd KMS v2 envelope storage.

To test native OAuth 2.0 client credentials with Keycloak, start a fresh lab.
The reset command deletes the lab cluster and its generated state.

```sh
make dev-env-reset
make dev-env-up AUTH=oauth2
```

This mode starts Keycloak with a realm built from
`deploy/config/keycloak-client.json`, so the lab uses the documented client
settings. Keycloak serves TLS from a certificate signed by the lab CA, at
`https://keycloak:8443` inside the Kind network and `https://localhost:18443`
from the host. OpenBao validates the provider's tokens through Keycloak's
OpenID Connect discovery, and `node-setup.sh install` receives the client
secret and the issuer CA. To observe the provider during an issuer outage,
stop Keycloak and follow the provider's auth metrics in Grafana:

```sh
docker compose -f test/dev-env/compose.yaml --project-name openbao-kms-dev-env --profile oauth2 stop keycloak
```

A running provider can continue using and renewing a valid OpenBao token while
Keycloak is unavailable. New token acquisition requires Keycloak. To restore the
issuer and check a client credentials grant:

```sh
docker compose -f test/dev-env/compose.yaml --project-name openbao-kms-dev-env --profile oauth2 start keycloak
make dev-env-keycloak AUTH=oauth2
```

The runtime OAuth E2E lane uses short OpenBao token lifetimes to verify issuer
outage rejection, credential rotation, and cold recovery while the protected API
server is stopped:

```sh
make test-e2e-kind-oauth2
```

The loopback ports can be changed with `DEV_ENV_OPENBAO_PORT`,
`DEV_ENV_KEYCLOAK_PORT`, `DEV_ENV_PROMETHEUS_PORT`, and `DEV_ENV_GRAFANA_PORT`.
Keep the same values for each command that operates on that lab.

To test the PKCS#11 certificate-auth path with SoftHSM:

```sh
make dev-env-reset
make dev-env-up AUTH=pkcs11
```

This mode builds the PKCS#11-enabled provider image and generates a SoftHSM token
and client certificate. It configures OpenBao certificate authentication with a
URI subject alternative name (SAN) binding. It then stages the hardware
security module (HSM) material into the Kind
control-plane node for the static pod.

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
make dev-env-keycloak AUTH=oauth2
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
- The OpenBao deployment follows the local server-mode pattern used by
  `openbao-demo`. It remains single-node to reduce local iteration time.
