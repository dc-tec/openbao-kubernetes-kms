---
title: Local lab
description: "Build the provider from source and run it against a local OpenBao and Kind cluster with one command, for development and evaluation."
eyebrow: Contribute
weight: 15
verifiedBy:
  - test/dev-env/README.md
  - mk/dev-env.mk
  - test/dev-env/scripts/verify-kms.sh
---

The local lab runs the whole encryption path on one workstation: a Kind
cluster, a single-node OpenBao in Docker Compose, the provider as a static pod,
and Prometheus and Grafana. It builds the provider from your checkout, so it is
suited to development and evaluation. It is not a release install and does not
replace the end-to-end release gates.

## Prerequisites

- Docker with Compose v2
- Kind
- kubectl
- OpenTofu
- OpenSSL
- curl

## Start the lab

From the repository root:

```sh
make dev-env-up
```

The target generates a local JWT signer and provider JWT, creates the Kind
cluster, starts OpenBao, Prometheus, and Grafana, and builds and loads the
provider image. It configures OpenBao through the OpenTofu module in
`deploy/opentofu/openbao-kubernetes-kms`. It stages the provider with
`init --model static-pod` and the generated `node-setup.sh`, as in
[Run as a static pod](/docs/get-started/static-pod/). Finally it enables KMS
encryption on `kube-apiserver` and checks that a new Secret reads back and is
stored in etcd with the `k8s:enc:kms:v2:` prefix.

To exercise native OAuth 2.0 client credentials with a Keycloak issuer instead
of a JWT file:

```sh
make dev-env-reset
make dev-env-up AUTH=oauth2
```

Keycloak runs in Compose with a realm built from
`deploy/config/keycloak-client.json`, and OpenBao validates its tokens through
OpenID Connect discovery. The provider obtains tokens itself; this mode does not
install a JWT refresh helper. The issuer runs outside the Kind cluster so token
acquisition does not depend on the API server that the provider protects.

Resetting the lab deletes its cluster and generated state. Use a fresh lab when
changing authentication modes. See `test/dev-env/README.md` for issuer outage
commands and the behavior of an existing OpenBao token.

To exercise PKCS#11 certificate auth with SoftHSM instead of JWT:

```sh
make dev-env-reset
make dev-env-up AUTH=pkcs11
```

Grafana runs at `http://127.0.0.1:18300` with user and password `admin`, and
shows the dashboard shipped in `deploy/grafana/dashboards/`.

## Stop the lab

```sh
make dev-env-down    # stop services and delete the cluster, keep generated state
make dev-env-reset   # also delete the generated state under .state/
```

Generated secrets, including the OpenBao root token, seal key, TLS keys, and
JWT, are local-only and stored under the ignored `.state/` directory.

The lab binds the provider metrics and health endpoints to `0.0.0.0` so
Prometheus can scrape the Kind node; the production samples bind them to
localhost. For every lab target and more detail, see `test/dev-env/README.md`.
