---
title: OpenBao auth and policy
description: "Variants of the default OpenBao setup: policy details and capabilities to avoid, JWKS or pinned JWT keys, certificate auth with PKCS#11, and generating the policy from configuration."
eyebrow: Configure · Authentication
weight: 10
verifiedBy:
  - cmd/bao-kms-provider/policy.go
  - test/e2e/openbao_cert_auth_test.go
  - test/e2e/provider_certauth_source_test.go
---

[Prepare OpenBao](/docs/get-started/openbao/) sets up the default: a JWT role
using OIDC discovery and the standard policy. This page covers the variants.
Commands use the shell variables from
[Plan identity values](/docs/get-started/plan-values/#record-the-values).

The provider accepts a JWT file or obtains a JWT directly through
[OAuth 2.0 client credentials](/docs/configure/oauth2/). Both sources use the
same OpenBao JWT auth mount and role. OAuth requires an authorization server
that issues signed JWT access tokens and works independently of the protected
Kubernetes API.

## Policy

The standard policy grants metadata read, encrypt, and decrypt on the key,
read on `<mount>/config/keys` so the provider can verify `disable_upsert`, and
update on `sys/capabilities-self` so `doctor` can check the token.

- Keep `auth/token/renew-self` when the role sets `token_no_default_policy=true`
  and the provider renews its token. Drop it if the provider only logs in
  again.
- The provider never calls `auth/token/lookup-self`; grant it only to separate
  diagnostic tooling.

Never grant the provider:

- `create` on `<mount>/encrypt/*`, which would let an encrypt call create a key,
- write access to key creation, rotation, configuration, or trim paths, which
  belong to operators,
- write access to `<mount>/config/keys`, which could turn off `disable_upsert`,
- write access to `<mount>/restore`, `<mount>/restore/<key>`, or
  `<mount>/rewrap/<key>`,
- `delete` on any key path, `read` on export or plaintext backup paths, or
  broad `sudo` or admin capabilities.

`doctor` queries the token's capabilities on these paths for the configured key
and mount without calling them, and the standard policy needs no extra grant
for that. A pass covers only the queried paths; review the whole policy for
access to other keys or paths.

`bao-kms-provider policy openbao` prints the standard policy from the active
configuration; review its paths before applying it. See
[Reference: CLI](/docs/reference/cli/#policy-openbao).

## JWT key sources

The default JWT config uses OIDC discovery. To use a JSON Web Key Set URL
instead, replace `<jwks-url>` with the issuer's key set URL:

```sh
bao write "auth/${JWT_MOUNT}/config" \
  jwks_url="<jwks-url>" \
  bound_issuer="${JWT_ISSUER}"
```

For recovery or isolated environments without a reachable issuer, pin the
issuer's public keys:

```sh
bao write "auth/${JWT_MOUNT}/config" \
  jwt_validation_pubkeys=@/etc/openbao/jwt-issuer.pub \
  bound_issuer="${JWT_ISSUER}"
```

Pinned keys must be updated when the issuer rotates its signing keys.

## Certificate auth

Certificate auth uses a client certificate whose private key stays in a
PKCS#11 token. Published release artifacts support JWT only; certificate auth
needs the separate host build, which requires cgo and a PKCS#11 module on the
host:

```sh
make build-certauth-pkcs11
make release-artifact-certauth-pkcs11-host
```

It is supported only when the selected release publishes that artifact and
marks it as tested; see [Reference: Compatibility](/docs/reference/compatibility/#auth-methods).

Configure a cert auth mount and a role bound to the provider's certificate
identity. This example binds a URI SAN:

```sh
bao auth enable -path=k8s-workload-a-cert cert
bao write auth/k8s-workload-a-cert/config disable_binding=false
bao write auth/k8s-workload-a-cert/certs/openbao-kms-control-plane \
  display_name=openbao-kms-control-plane \
  certificate=@/etc/openbao/trust/openbao-kms-client-ca.pem \
  allowed_uri_sans=urn:openbao-kms:workload-a \
  token_policies="${POLICY_NAME}" \
  token_ttl=10m \
  token_max_ttl=30m \
  token_no_default_policy=true \
  ocsp_fail_open=false
```

The OpenBao listener the provider uses must request client certificates.
Keep `disable_binding=false` so renewal stays tied to the login certificate.

On the provider side, set `auth.method: cert` with the PKCS#11 fields from
[Reference: Configuration](/docs/reference/configuration/#auth). The systemd
service validates the selected auth material during startup. For static pods, mount the certificate chain,
PIN file, and PKCS#11 module instead of the JWT.
