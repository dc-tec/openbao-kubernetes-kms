# OpenTofu Module Skeleton

This module skeleton renders provider-side configuration artifacts. It intentionally does not create OpenBao Transit keys, rotate keys, or publish Kubernetes API objects.

Use platform automation to create the Transit mount, Transit key, JWT auth method, and policy before rendering these files.

Example:

```hcl
module "openbao_kubernetes_kms" {
  source = "./deploy/opentofu/openbao-kubernetes-kms"

  provider_name        = "openbao-kms-workload-a"
  cluster_id           = "workload-a"
  openbao_address      = "https://bao.example.internal:8200"
  openbao_tls_name     = "bao.example.internal"
  openbao_instance_id  = "bao-prod-a"
  transit_mount_id     = "transit-prod-primary"
  transit_key_lineage  = "01HXEXAMPLEKEYLINEAGEID"
  socket_group         = "openbao-kms-socket"
}
```

Outputs:

- `provider_config_yaml`
- `encryption_config_yaml`
- `openbao_policy_hcl`
