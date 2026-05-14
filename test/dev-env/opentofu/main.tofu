module "openbao_kubernetes_kms" {
  source = "../../../deploy/opentofu/openbao-kubernetes-kms"

  transit_mount_path = var.transit_mount_path
  transit_key_name   = var.transit_key_name
  transit_key_type   = "aes256-gcm96"
  policy_name        = var.provider_policy_name
}
