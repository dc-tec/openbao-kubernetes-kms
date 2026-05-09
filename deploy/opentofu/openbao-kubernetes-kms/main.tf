locals {
  transit_mount_path = trim(var.transit_mount_path, "/")
}

output "provider_config_yaml" {
  description = "Rendered bao-kms-provider configuration."
  value = templatefile("${path.module}/templates/provider-config.yaml.tftpl", {
    socket_path         = var.socket_path
    socket_group        = var.socket_group
    metrics_address     = var.metrics_address
    health_address      = var.health_address
    openbao_address     = var.openbao_address
    openbao_namespace   = var.openbao_namespace
    openbao_ca_file     = var.openbao_ca_file
    openbao_tls_name    = var.openbao_tls_name
    openbao_instance_id = var.openbao_instance_id
    auth_mount_path     = var.auth_mount_path
    auth_role           = var.auth_role
    jwt_file            = var.jwt_file
    transit_mount_path  = local.transit_mount_path
    transit_key_name    = var.transit_key_name
    provider_name       = var.provider_name
    cluster_id          = var.cluster_id
    transit_mount_id    = var.transit_mount_id
    transit_key_lineage = var.transit_key_lineage
    state_path          = var.state_path
  })
}

output "encryption_config_yaml" {
  description = "Rendered Kubernetes EncryptionConfiguration with identity fallback for initial migration."
  value = templatefile("${path.module}/templates/encryption-config.yaml.tftpl", {
    provider_name = var.provider_name
    socket_path   = var.socket_path
    resources     = var.encrypted_resources
  })
}

output "openbao_policy_hcl" {
  description = "Rendered least-privilege OpenBao policy for the provider token."
  value = templatefile("${path.module}/templates/openbao-policy.hcl.tftpl", {
    transit_mount_path = local.transit_mount_path
    transit_key_name   = var.transit_key_name
  })
}
