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
[Prepare OpenBao](/docs/get-started/openbao/#manual-alternative).

The provider accepts a JWT file or obtains a JWT directly through
[OAuth 2.0 client credentials](/docs/configure/oauth2/). Both sources use the
same OpenBao JWT auth mount and role. OAuth requires an authorization server
that issues signed JWT access tokens and works independently of the protected
Kubernetes API.

## Policy

The standard policy grants metadata read, encrypt, and decrypt on the key,
read on `<mount>/config/keys` so the provider can verify `disable_upsert`, and
update on `sys/capabilities-self` so `doctor` can check the token.

- Generated policies include update on `auth/token/renew-self`, including when
  the role sets `token_no_default_policy=true`. Both `init` and `policy openbao`
  include this permission by default.
- `policy openbao --include-token-renewal=false` omits that stanza. Use it when
  OpenBao issues non-renewable tokens or another attached policy grants renewal.
  The provider attempts renewal for renewable tokens and logs in again when
  renewal fails. It uses login directly for non-renewable tokens. There is no
  runtime switch for disabling renewal; omitting a permission does not disable
  the attempt.
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

## Credential files

JWT files, OAuth client-secret files, and PKCS#11 PIN files must be regular
files at absolute paths. Symlinks are rejected, including the symlinks commonly
used by Kubernetes projected volumes. Use mode `0600`, or `0640` only with a
trusted group that needs credential access. Group write, group execute, and
all world permissions are rejected. Keep parent directories under trusted
host administration.

A node-local credential helper must write a new regular file in the same
private directory, set its owner and permissions, then atomically rename it
over the configured name. Mount that directory into a container so replacement
files remain visible; a single-file bind mount keeps the old inode. JWT and
OAuth credentials are reread before login. The PKCS#11 PIN is read when the
certificate provider is constructed, so restart the provider after changing it.

Credential delivery and recovery must work while the protected Kubernetes API
is unavailable. Copying a projected token into a regular file satisfies the
file format requirement but does not remove its issuer or renewal dependency.
See [Security: Auth model](/docs/security/auth-model/#jwt-source-options) and
[Static pod credential delivery](/docs/get-started/static-pod/).

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
