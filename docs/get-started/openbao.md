---
title: Prepare OpenBao
description: "Create the dedicated Transit mount and key, the least-privilege policy, and the JWT auth role the provider logs in with."
eyebrow: Get started · Step 4
weight: 40
verifiedBy:
  - deploy/opentofu/openbao-kubernetes-kms/main.tofu
  - cmd/bao-kms-provider/policy.go
  - test/dev-env/opentofu/main.tofu
---

Run these steps once per Kubernetes cluster as an OpenBao administrator. At the
end, OpenBao holds a Transit key the provider can use for encrypt and decrypt
only, and a JWT role that issues the provider a token with exactly that
permission.

## Before you begin

- Choose and record the values from [Plan identity values](/docs/get-started/plan-values/),
  and set the shell variables from that page in this shell.
- Use an OpenBao endpoint with valid TLS that serves requests without HTTP
  redirects. For HA, use server-side request forwarding or an endpoint routed
  to the active node.
- Log in to the `bao` CLI with a token that can manage `sys/mounts`, `sys/auth`,
  `sys/policies`, and the new Transit mount.
- Confirm that the JWT issuer for the provider host credential is reachable
  independently of the protected Kubernetes API server. See
  [Security: Auth model](/docs/security/auth-model/).

{{< callout type="tip" title="Manage OpenBao with OpenTofu" >}}
The OpenTofu module in `deploy/opentofu/openbao-kubernetes-kms` creates the
Transit mount, `disable_upsert`, the key, and the policy from Steps 1, 2, and 4.
Pin its source to the release you install, for example
`git::https://github.com/dc-tec/openbao-kubernetes-kms.git//deploy/opentofu/openbao-kubernetes-kms?ref=0.1.0-preview.2`.
You still generate the lineage ID and configure auth with Steps 3 and 5.
{{< /callout >}}

## Step 1: Enable the Transit mount

Enable a dedicated Transit mount, then disable upsert so an encrypt call to a
misspelled key name fails instead of creating a new key:

```sh
bao secrets enable -path="${TRANSIT_MOUNT}" transit
bao write "${TRANSIT_MOUNT}/config/keys" disable_upsert=true
```

`disable_upsert` applies to the whole mount, which is one reason the mount must
not be shared with other workloads.

## Step 2: Create the Transit key

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

## Step 3: Capture the key lineage ID

Generate the lineage ID for this key and record it with the other values:

```sh
KEY_LINEAGE_ID=$(openssl rand -hex 16)
echo "${KEY_LINEAGE_ID}"
```

The lineage ID is not a secret. Generate it once, when the key is created, and
keep it for the key's whole lifetime. If the Transit key is ever deleted and
recreated, generate a new lineage ID and treat the change as a destructive
migration; the provider rejects ciphertext from a different key generation.

## Step 4: Create the policy

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

## Step 5: Configure JWT auth

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

## Result

OpenBao now has the Transit mount with `disable_upsert`, the key with the
recommended profile, the policy, and the JWT role. You verify the provider's
view of this setup with `bao-kms-provider doctor` and `verify-key` after the
provider is installed on a control-plane node.
