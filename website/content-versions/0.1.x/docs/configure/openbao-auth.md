---
title: OpenBao auth and policy
description: "Variants of the default OpenBao setup: manual or OpenTofu setup, policy details and capabilities to avoid, host JWT agent requirements, JWKS or pinned JWT keys, and certificate auth with PKCS#11."
eyebrow: Configure · Authentication
weight: 10
verifiedBy:
  - cmd/bao-kms-provider/policy.go
  - deploy/opentofu/openbao-kubernetes-kms/main.tofu
  - test/e2e/openbao_cert_auth_test.go
  - test/e2e/provider_certauth_source_test.go
---

[Prepare OpenBao](/docs/get-started/openbao/) runs the generated setup for the
default: a JWT role using OIDC discovery and the standard policy. This page
covers the variants. Commands use the shell variables from
[Manual setup](#manual-setup).

The provider accepts a JWT file or obtains a JWT directly through
[OAuth 2.0 client credentials](/docs/configure/oauth2/). Both sources use the
same OpenBao JWT auth mount and role. OAuth requires an authorization server
that issues signed JWT access tokens and works independently of the protected
Kubernetes API.

## Manual setup

Use these steps instead of the generated `openbao-setup.sh` when you manage
the OpenBao resources by hand or through configuration management. Set these
variables from your recorded values:

```sh
TRANSIT_MOUNT=transit
KEY_NAME=k8s-workload-a-etcd
JWT_MOUNT=k8s-workload-a-jwt
JWT_ROLE=openbao-kms-control-plane
POLICY_NAME=openbao-kms-workload-a
JWT_ISSUER=https://issuer.example.internal
JWT_AUDIENCE=bao-kms-provider
JWT_SUBJECT=system:openbao-kms:workload-a
```

Export `BAO_NAMESPACE` as well when using a non-root OpenBao namespace.

{{< callout type="tip" title="Manage OpenBao with OpenTofu" >}}
The OpenTofu module in `deploy/opentofu/openbao-kubernetes-kms` creates the
Transit mount, `disable_upsert`, the key, and the policy from Steps 1, 2, and 4.
Pin its source to the release you install:
`git::https://github.com/dc-tec/openbao-kubernetes-kms.git//deploy/opentofu/openbao-kubernetes-kms?ref=<release-tag>`.
You still generate the lineage ID and configure auth with Steps 3 and 5.
{{< /callout >}}

### Step 1: Enable the Transit mount

Enable a dedicated Transit mount, then disable upsert so an encrypt call to a
misspelled key name fails instead of creating a new key:

```sh
bao secrets enable -path="${TRANSIT_MOUNT}" transit
bao write "${TRANSIT_MOUNT}/config/keys" disable_upsert=true
```

`disable_upsert` applies to the whole mount, which is one reason the mount must
not be shared with other workloads.

### Step 2: Create the Transit key

```sh
bao write "${TRANSIT_MOUNT}/keys/${KEY_NAME}" \
  type=aes256-gcm96 \
  exportable=false \
  allow_plaintext_backup=false
```

Confirm the key profile:

```sh
bao read "${TRANSIT_MOUNT}/keys/${KEY_NAME}"
```

The output shows `type` `aes256-gcm96` and `false` for `derived`,
`convergent_encryption`, `exportable`, `allow_plaintext_backup`, and
`deletion_allowed`, with `auto_rotate_period` `0s`. `aes256-gcm96` is the only
tested key type in the current release line.

{{< callout type="warning" title="Irreversible key settings" >}}
Never enable `exportable` or `allow_plaintext_backup` on this key. OpenBao
cannot turn either setting off again. Keep `deletion_allowed=false`; enabling
it permits key deletion by any token with delete capability.
{{< /callout >}}

### Step 3: Capture the key lineage ID

If you generated files with `init --new-key`, use the lineage ID recorded in
`generated/config.yaml` and skip this step. Otherwise, generate the lineage ID
for this key and record it with the other values:

```sh
KEY_LINEAGE_ID=$(openssl rand -hex 16)
echo "${KEY_LINEAGE_ID}"
```

The lineage ID is not a secret. Generate it once, when the key is created, and
keep it for the key's whole lifetime. If the Transit key is ever deleted and
recreated, generate a new lineage ID and treat the change as a destructive
migration; the provider rejects ciphertext from a different key generation.

### Step 4: Create the policy

Write the least-privilege policy for the provider token:

```sh
bao policy write "${POLICY_NAME}" - <<EOF
# Read Transit key metadata.
path "${TRANSIT_MOUNT}/keys/${KEY_NAME}" {
  capabilities = ["read"]
}

# Encrypt with the existing key.
path "${TRANSIT_MOUNT}/encrypt/${KEY_NAME}" {
  capabilities = ["update"]
}

# Decrypt existing ciphertext.
path "${TRANSIT_MOUNT}/decrypt/${KEY_NAME}" {
  capabilities = ["update"]
}

# Inspect Transit disable_upsert.
path "${TRANSIT_MOUNT}/config/keys" {
  capabilities = ["read"]
}

# Allow doctor to inspect this token's capabilities.
path "sys/capabilities-self" {
  capabilities = ["update"]
}

# Allow token renewal when the auth role disables the default policy.
path "auth/token/renew-self" {
  capabilities = ["update"]
}
EOF
```

The policy deliberately omits `create` on the encrypt path, `update` on the key
path (rotation stays with operators), and any delete, export, or backup
capability. For variants and the full list of capabilities to avoid, see
[Configure: OpenBao auth and policy](/docs/configure/openbao-auth/).

### Step 5: Configure JWT auth

Enable JWT auth at a dedicated path and trust the issuer that signs the
provider host JWT. This example uses OpenID Connect (OIDC) discovery; for a
JSON Web Key Set (JWKS) URL or pinned public keys, see
[Configure: OpenBao auth and policy](/docs/configure/openbao-auth/).

```sh
bao auth enable -path="${JWT_MOUNT}" jwt
bao write "auth/${JWT_MOUNT}/config" \
  oidc_discovery_url="${JWT_ISSUER}" \
  bound_issuer="${JWT_ISSUER}"
```

Create the role the provider logs in with. It binds the audience and subject,
attaches only the policy from Step 4, and issues short-lived tokens:

```sh
bao write "auth/${JWT_MOUNT}/role/${JWT_ROLE}" \
  role_type=jwt \
  bound_audiences="${JWT_AUDIENCE}" \
  bound_subject="${JWT_SUBJECT}" \
  user_claim=sub \
  token_policies="${POLICY_NAME}" \
  token_ttl=30m \
  token_max_ttl=1h \
  token_no_default_policy=true
```

If the issuer adds a cluster or environment claim, bind it as well with
`bound_claims`.

The provider reads its JWT from a file on the host. Do not rely on a Kubernetes
ServiceAccount token from the protected cluster as the only credential: if that
API server is down, the provider cannot refresh its token during recovery.

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

### Host JWT agent

For `auth.jwt.source: file`, the provider reads the JWT file and never obtains
a replacement itself. Before installation, verify that the host agent:

- starts and renews without calling the protected Kubernetes API,
- obtains JWTs with the expected issuer, audience, and subject, and enough
  lifetime to exceed `auth.jwt.minRemainingTtl` plus renewal and outage margin,
- replaces the file atomically as described above, without symlinks,
- reports renewal failure and has a documented way to recover its own identity.

Test a renewal and a cold provider start with the protected API stopped in a
disposable environment. This contract does not qualify every host agent; keep
evidence for the agent and issuer your platform uses.

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
