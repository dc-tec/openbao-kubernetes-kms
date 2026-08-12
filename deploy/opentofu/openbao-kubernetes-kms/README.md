# OpenTofu Module Skeleton

This module skeleton configures the OpenBao resources required by
`bao-kms-provider`:

- Transit secrets engine mount,
- Transit key with safe Kubernetes KMS defaults,
- Transit `disable_upsert` mount policy,
- least-privilege OpenBao policy for the provider token, including `doctor`
  capability checks and optional token renewal.

The module does not render provider configuration files or Kubernetes
`EncryptionConfiguration`. It also does not configure provider authentication
roles, rotate Transit keys, or publish Kubernetes API objects.

The module uses OpenTofu `.tofu` files and the Vault-compatible provider.
Configure the provider in the calling stack with the OpenBao address, token,
certificate authority (CA) bundle, and namespace.

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
the deployment uses re-login instead of renewal and the auth role does not need
`auth/token/renew-self`.

Outputs:

- `transit_mount_path`
- `transit_key_name`
- `transit_key_latest_version`
- `provider_policy_name`
- `provider_policy_hcl`

The module fixes `deletion_allowed`, `exportable`, `allow_plaintext_backup`,
`derived`, `convergent_encryption`, and `auto_rotate_period` instead of exposing
them as inputs. Destroying the module does not provide a key-deletion path.
OpenBao blocks Transit key deletion unless an operator first performs a separate
break-glass change.
