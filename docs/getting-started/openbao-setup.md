---
title: "OpenBao Setup"
description: "Provision the Transit mount, key, least-privilege policy, and OpenBao authentication required by bao-kms-provider."
weight: 20
---

# OpenBao Setup

The provider expects an existing OpenBao deployment with the Transit secrets engine enabled, a single named key per Kubernetes cluster, a least-privilege policy, and one supported auth method for the host running the provider. This page lists the OpenBao-side commands in order. Run them as an OpenBao administrator before installing the provider.

## Prerequisites

- A reachable OpenBao instance (HTTPS endpoint, valid TLS).
- An OpenBao token with administrative capabilities for `sys/`, `auth/`, and `transit/` paths.
- A deterministic name for the Kubernetes Transit key. The naming convention used in this guide is `k8s-<workload>-etcd`. Replace `workload-a` with your environment-specific identifier in every example below.
- A stable OpenBao instance ID and Transit mount ID for provider configuration. These are non-secret identity values used in Kubernetes `key_id` and AAD derivation.
- Optional: an OpenBao namespace for this Kubernetes cluster when a single OpenBao cluster serves multiple Kubernetes clusters. Configure it as `openbao.namespace`; auth and Transit paths in this guide remain relative to that namespace.

For background on why each choice is made, see [Architecture: Transit Key Model](/architecture/transit-key-model/) and [Security: Auth Model](/security/auth-model/).

## Step 1: Enable The Transit Mount

Enable a dedicated Transit mount for Kubernetes KMS keys:

```sh
bao secrets enable -path=transit transit
```

Disable upsert on the mount so an encrypt call to a misspelled key name does not silently create a new key:

```sh
bao write transit/config/keys disable_upsert=true
```

Use a dedicated Transit mount for the Kubernetes KMS keys. `disable_upsert` is configured at the mount level and would affect any other workloads sharing the same mount.

## Step 2: Create The Transit Key

Create the key with the recommended profile:

```sh
bao write transit/keys/k8s-workload-a-etcd \
  type=aes256-gcm96 \
  exportable=false \
  allow_plaintext_backup=false
```

Recommended properties:

| Property | Value |
|---|---|
| key type | `aes256-gcm96` |
| derived | `false` |
| convergent encryption | `false` |
| exportable | `false` |
| plaintext backup | `false` |
| deletion allowed | `false` |
| auto-rotate period | `0` (rotation is operator-driven) |

For the current release line, `aes256-gcm96` is the only validated and supported key type. Other AEAD Transit key types require implementation and release evidence before they can be documented as supported.

Once the key exists, do not enable `exportable`, `allow_plaintext_backup`, or `deletion_allowed` after the fact. OpenBao does not allow these flags to be turned off again, so the safety properties of the key cannot be restored.

## Step 3: Capture The Key Lineage ID

Generate a stable, non-secret identifier for this Transit key creation event:

```sh
openssl rand -hex 16
```

Example output:

```text
7d34fb7df15f4e4c95d6c2a50fe90d84
```

Store the lineage ID in platform configuration management and supply it to the provider through `transit.keyIdScope.keyLineageId` (see [Configuration](/reference/configuration/)).

The lineage ID is not a secret. It must be:

- generated once when the Transit key is created,
- stable for the full lifetime of that key generation,
- unique across deleted and recreated keys,
- independent of the key name, mount path, OpenBao URL, or cluster name.

An existing platform inventory ID or ULID can be used if it has the same properties. Do not derive the lineage ID from mutable topology strings.

If the Transit key is deleted and recreated, generate a new lineage ID and treat the event as a destructive migration. The provider uses this ID to reject decrypt requests carrying ciphertext from a different key generation.

## Step 4: Generate The Policy

Generate a least-privilege policy from the provider configuration file:

```sh
bao-kms-provider policy openbao \
  --config /etc/openbao-kms/config.yaml
```

Example policy:

```hcl
# Read Transit key metadata.
path "transit/keys/k8s-workload-a-etcd" {
  capabilities = ["read"]
}

# Encrypt with the existing key.
path "transit/encrypt/k8s-workload-a-etcd" {
  capabilities = ["update"]
}

# Decrypt existing ciphertext.
path "transit/decrypt/k8s-workload-a-etcd" {
  capabilities = ["update"]
}

# Inspect Transit disable_upsert.
path "transit/config/keys" {
  capabilities = ["read"]
}

# Allow doctor to inspect this token's capabilities.
path "sys/capabilities-self" {
  capabilities = ["update"]
}
```

The policy must not grant:

- `create` on `transit/encrypt/*` (key creation through encrypt is what `disable_upsert` blocks at the mount level; the policy enforces it again at the token level),
- `update` on `transit/keys/*` (this is the rotation capability and stays with operators or platform automation),
- `delete` on any Transit key path,
- `read` on `transit/export/*`,
- `read` on plaintext backup paths,
- broad `sudo` or admin permissions.

Apply the policy:

```sh
bao policy write openbao-kms-workload-a /tmp/openbao-kms-workload-a.hcl
```

For policy variants and rationale see [Reference: Transit Policy Examples](/reference/transit-policy-examples/).

## Step 5: Configure Auth

Choose one provider auth method. JWT auth is the default build and release path.
Certificate auth with the PKCS#11 source requires a binary built with
`certauth_pkcs11`.

### JWT Auth

Enable JWT auth at a dedicated path:

```sh
bao auth enable -path=auth/k8s-workload-a-jwt jwt
```

Configure JWT validation using one of:

- local public keys,
- a JWKS URL,
- an OIDC discovery URL.

Create a role bound to the control-plane plugin identity:

```sh
bao write auth/k8s-workload-a-jwt/role/openbao-kms-control-plane \
  role_type=jwt \
  bound_audiences="bao-kms-provider" \
  bound_subject="system:openbao-kms:workload-a" \
  user_claim="sub" \
  policies="openbao-kms-workload-a" \
  ttl="30m" \
  max_ttl="1h"
```

Recommended role constraints:

- bound issuer,
- bound audience,
- bound subject,
- bound cluster or environment claim,
- short token TTL,
- no default policy unless the deployment requires it,
- the narrow policy from Step 4.

The provider reads JWTs from a host-mounted file. The JWT issuer should be reachable independently of the protected Kubernetes API server. Avoid using a Kubernetes ServiceAccount token from the same protected cluster as the only credential source. If that API server is unavailable, refreshing the token may be impossible during the recovery the provider is meant to support. See [Security: Auth Model](/security/auth-model/) for the trust-boundary discussion.

### Certificate Auth

Enable cert auth at a dedicated path:

```sh
bao auth enable -path=auth/k8s-workload-a-cert cert
```

The OpenBao listener used by the provider must request TLS client certificates. Keep TLS enabled and do not set `tls_disable_client_certs=true` on that listener.

Keep cert auth binding enabled for token renewal:

```sh
bao write auth/k8s-workload-a-cert/config \
  disable_binding=false
```

Configure a cert role bound to the provider identity. For a CA-issued provider
certificate, use the issuing CA as the trusted certificate and bind stable
identity fields such as URI SAN, DNS SAN, common name, OU, or required
extensions. This example binds a URI SAN:

```sh
bao write auth/k8s-workload-a-cert/certs/openbao-kms-control-plane \
  display_name=openbao-kms-control-plane \
  certificate=@/etc/openbao/trust/openbao-kms-client-ca.pem \
  allowed_uri_sans="urn:openbao-kms:workload-a" \
  token_policies="openbao-kms-workload-a" \
  token_ttl="30m" \
  token_max_ttl="1h" \
  token_no_default_policy=true \
  ocsp_fail_open=false
```

For PKCS#11-backed client certificates, keep the private key inside the PKCS#11
module and give the provider only the certificate chain, module path, token
label, key label, and local PIN file path.

SPIFFE certificate-source wiring remains in tree for local verification and
upstream OpenBao alignment work, but `auth.cert.source: spiffe` is not a
supported user configuration until the supported OpenBao version can derive
cert-auth identity aliases from URI SANs.

## Step 6: Verify

After installing the provider, `bao-kms-provider doctor` validates the OpenBao side end-to-end:

- TLS connection to OpenBao succeeds.
- The configured auth material is locally valid.
- OpenBao auth login succeeds.
- The token can read Transit key metadata.
- The token can encrypt and decrypt probe data.
- The token cannot rotate, export, back up, or delete the key.
- The key type and flags match the recommended profile.
- `disable_upsert` is enabled on the Transit mount.

Doctor failures during initial setup are usually policy-related. See [Operations: Troubleshooting](/operations/troubleshooting/) for common cases.

Before wiring Kubernetes encryption, run the focused key check as well:

```sh
bao-kms-provider verify-key \
  --config /etc/openbao-kms/config.yaml
```

`verify-key` checks Transit metadata, key type, export settings, plaintext backup settings, deletion settings, and version restrictions. It is useful before changing API server encryption because it narrows OpenBao setup problems away from Kubernetes socket and API server wiring.

## Read Next

1. [Install](/getting-started/install/) to fetch the provider binary and verify the local environment.
2. [Kubernetes Encryption Config](/getting-started/kubernetes-encryption-config/) once the provider runs and exposes its Unix socket.
