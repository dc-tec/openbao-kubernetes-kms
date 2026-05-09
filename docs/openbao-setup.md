# OpenBao Setup

This guide defines the required OpenBao-side setup for the provider.

## Principles

- OpenBao should be reachable independently of the protected Kubernetes API server.
- The plugin should authenticate with JWT by default.
- The plugin must receive only the permissions required for hot-path KMS operation.
- The plugin must not create, rotate, delete, export, or back up Transit keys.
- Transit keys must be backed up and restored as part of OpenBao disaster recovery.

## Transit Mount

Enable a dedicated Transit mount for Kubernetes KMS use:

```sh
bao secrets enable -path=transit transit
```

For a dedicated Transit mount, disable key upsert:

```sh
bao write transit/config/keys disable_upsert=true
```

`disable_upsert` prevents accidental key creation through an encrypt call to a misspelled key name.

## Transit Key

Recommended key:

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

Optional key type:

- `xchacha20-poly1305` may be considered for non-FIPS environments after integration testing.

Avoid:

- convergent encryption,
- derived keys,
- exportable keys,
- plaintext backup,
- deleting and recreating keys,
- raising `min_decryption_version` before migration verification.

## Key Lineage ID

Create and store a stable `transit_key_lineage_id` when the Transit key is created. This value is not a secret, but it is identity-bearing.

Example:

```text
01HXEXAMPLEKEYLINEAGEID
```

Store it in platform configuration management and in the plugin config. If the Transit key is deleted and recreated, generate a new lineage ID and treat the event as a destructive migration.

## Policy

Generate a least-privilege policy from the provider config:

```sh
bao-kms-provider policy openbao \
  --config /etc/openbao-kms/config.yaml
```

Example output:

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

Do not grant these capabilities to the plugin token:

- `create` on `transit/encrypt/*`,
- `update` on `transit/keys/*`,
- `delete` on any Transit key path,
- `read` on `transit/export/*`,
- `read` on plaintext backup paths,
- broad `sudo` or admin permissions.

## JWT Auth

Enable JWT auth:

```sh
bao auth enable -path=auth/k8s-workload-a-jwt jwt
```

Configure JWT validation using one of:

- local public keys,
- JWKS URL,
- OIDC discovery URL.

Create a role scoped to the control-plane plugin identity:

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

Recommended constraints:

- bound issuer,
- bound audience,
- bound subject,
- bound cluster or environment claim,
- short token TTL,
- no default policy unless required,
- narrow policy set.

## Authentication Source

The plugin reads JWTs from a host-mounted file. The JWT issuer should be outside the protected Kubernetes API server path when possible.

Avoid using a Kubernetes ServiceAccount token from the same protected cluster as the only recovery identity. If the API server is down, issuing or refreshing that token may be impossible.

## Verification

Before starting the plugin, `doctor` should verify:

- OpenBao TLS connection succeeds,
- JWT file is readable and not near expiry,
- JWT login succeeds,
- token can read Transit key metadata,
- token can encrypt and decrypt probe data,
- token cannot rotate, export, back up, or delete keys,
- key type and flags match policy,
- `disable_upsert` is enabled where required.

## Rotation Ownership

Transit key rotation is an operator/platform action, not a hot-path plugin action. The plugin observes new Transit versions, promotes a new active key snapshot after stability checks, and exposes the new Kubernetes `key_id` through Status.

## Source References

- [OpenBao Transit API](https://openbao.org/api-docs/secret/transit/)
- [OpenBao Transit documentation](https://openbao.org/docs/secrets/transit/)
- [OpenBao JWT auth](https://openbao.org/docs/2.4.x/auth/jwt/)
