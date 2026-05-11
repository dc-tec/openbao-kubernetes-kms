# OpenTofu Module Skeleton

This module skeleton configures the OpenBao-side primitives required by `bao-kms-provider`:

- Transit secrets engine mount,
- Transit key with safe Kubernetes KMS defaults,
- Transit `disable_upsert` mount policy,
- least-privilege OpenBao policy for the provider token, including `doctor`
  capability checks and optional token-renewal self paths.

It intentionally does not render provider config files, render Kubernetes `EncryptionConfiguration`, configure JWT auth roles, rotate Transit keys, or publish Kubernetes API objects.

The module uses OpenTofu `.tofu` files and the Vault-compatible provider. Configure the provider in the calling stack for your OpenBao address, token, CA bundle, and namespace.

Example:

```hcl
provider "vault" {
  address = "https://bao.example.internal:8200"
}

module "openbao_kubernetes_kms" {
  source = "./deploy/opentofu/openbao-kubernetes-kms"

  transit_mount_path = "transit"
  transit_key_name   = "k8s-workload-a-etcd"
  policy_name        = "openbao-kms-workload-a"
}
```

`include_token_renewal_capabilities` defaults to `true` because the maintained
provider configuration samples enable token renewal. Set it to `false` only when
the deployment uses re-login instead of renewal and the JWT role does not need
`auth/token/lookup-self` or `auth/token/renew-self`.

Outputs:

- `transit_mount_path`
- `transit_key_name`
- `transit_key_latest_version`
- `provider_policy_name`
- `provider_policy_hcl`

`deletion_allowed`, `exportable`, `allow_plaintext_backup`, `derived`, `convergent_encryption`, and `auto_rotate_period` are fixed to safe values. Destroying the module should not be treated as a key deletion path; Transit key deletion is intentionally blocked by OpenBao unless an operator performs a separate break-glass change.
