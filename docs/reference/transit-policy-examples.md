---
title: "Transit Policy Examples"
description: "Reference OpenBao policy, auth role, and Transit key configuration examples for bao-kms-provider, plus capabilities to avoid."
weight: 110
---

# Transit Policy Examples

This page collects the reference OpenBao policy, auth role, and Transit key configuration shapes used by `bao-kms-provider`. For the bring-up workflow that applies these examples, see [Getting Started: OpenBao Setup](/getting-started/openbao-setup/). Replace the workload-specific identifiers (mount path, key name, role name, audience, subject, and SPIFFE ID) with values from your environment.

## Plugin Hot-Path Policy

Least-privilege OpenBao policy granting only the capabilities the provider needs at the encrypt and decrypt path:

```hcl
path "transit/encrypt/k8s-workload-a-etcd" {
  capabilities = ["update"]
}
path "transit/decrypt/k8s-workload-a-etcd" {
  capabilities = ["update"]
}
path "transit/keys/k8s-workload-a-etcd" {
  capabilities = ["read"]
}
path "sys/capabilities-self" {
  capabilities = ["update"]
}
```

`sys/capabilities-self` is required so `bao-kms-provider doctor` can verify the token's effective capabilities.

If token renewal is enabled and the JWT role disables the default policy, add only the required self-token paths:

```hcl
path "auth/token/lookup-self" {
  capabilities = ["read"]
}
path "auth/token/renew-self" {
  capabilities = ["update"]
}
```

If the default mode is re-login instead of token renewal, these paths can be omitted.

## Capabilities To Avoid

The plugin token must not have:

- `create` on `transit/encrypt/*` (key creation through encrypt; blocked by `disable_upsert` at the mount and refused at the token level),
- `update` on `transit/keys/*` (the rotation capability; rotation belongs to operators or platform automation),
- `delete` on any Transit key path,
- `read` on `transit/export/*`,
- `read` on plaintext backup paths,
- broad `sudo` or admin permissions.

OpenBao policies are path-based and deny by default. Capabilities are only what is explicitly granted.

## JWT Auth Role: OIDC Discovery

```sh
bao auth enable -path=k8s-workload-a-jwt jwt
bao write auth/k8s-workload-a-jwt/config \
  oidc_discovery_url="https://issuer.example.internal" \
  bound_issuer="https://issuer.example.internal"
bao write auth/k8s-workload-a-jwt/role/openbao-kms-control-plane \
  role_type="jwt" \
  user_claim="sub" \
  bound_audiences='["bao-kms-provider"]' \
  bound_subject="system:openbao-kms:workload-a:control-plane" \
  token_policies='["openbao-kms-workload-a"]' \
  token_ttl="10m" \
  token_max_ttl="30m" \
  token_no_default_policy="true" \
  clock_skew_leeway="60s" \
  expiration_leeway="30s"
```

## JWT Auth Role: Pinned Public Keys

For recovery or isolated environments where OIDC discovery is unavailable:

```sh
bao write auth/k8s-workload-a-jwt/config \
  jwt_validation_pubkeys=@/etc/openbao/jwt-issuer.pub \
  bound_issuer="https://issuer.example.internal"
```

OpenBao JWT auth requires one of: OIDC discovery, a JWKS URL, or local validation public keys.

## Certificate Auth Role: URI SAN

```sh
bao auth enable -path=k8s-workload-a-cert cert
bao write auth/k8s-workload-a-cert/config \
  disable_binding=false
bao write auth/k8s-workload-a-cert/certs/openbao-kms-control-plane \
  display_name="openbao-kms-control-plane" \
  certificate=@/etc/openbao/trust/openbao-kms-client-ca.pem \
  allowed_uri_sans="urn:openbao-kms:workload-a" \
  token_policies='["openbao-kms-workload-a"]' \
  token_ttl="10m" \
  token_max_ttl="30m" \
  token_no_default_policy="true" \
  ocsp_fail_open="false"
```

The OpenBao listener used by the provider must request client certificates. Keep cert auth binding enabled so renewal remains tied to the certificate identity used during login.

## Transit Key Configuration

```sh
bao secrets enable -path=transit transit

# Recommended for a dedicated Transit mount used by Kubernetes KMS.
bao write transit/config/keys disable_upsert=true

bao write transit/keys/k8s-workload-a-etcd \
  type="aes256-gcm96" \
  derived="false" \
  convergent_encryption="false" \
  exportable="false" \
  allow_plaintext_backup="false"

bao write transit/keys/k8s-workload-a-etcd/config \
  deletion_allowed="false" \
  min_encryption_version="0" \
  min_decryption_version="1" \
  auto_rotate_period="0"
```

Verify the exact CLI syntax against the OpenBao CLI version you are running. See [Reference: Compatibility: OpenBao](/reference/compatibility/#openbao) for the validated OpenBao version.

## Generating The Policy From Configuration

The provider CLI generates the hot-path policy from the active configuration:

```sh
bao-kms-provider policy openbao \
  --config /etc/openbao-kms/config.yaml
```

Review the rendered paths before applying. See [Reference: CLI: policy openbao](/reference/cli/#policy-openbao).
