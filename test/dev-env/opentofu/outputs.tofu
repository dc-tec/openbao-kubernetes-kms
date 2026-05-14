output "transit_mount_path" {
  value = module.openbao_kubernetes_kms.transit_mount_path
}

output "transit_key_name" {
  value = module.openbao_kubernetes_kms.transit_key_name
}

output "provider_policy_name" {
  value = module.openbao_kubernetes_kms.provider_policy_name
}

output "jwt_auth_mount_path" {
  value = var.enable_jwt_auth ? "auth/${vault_jwt_auth_backend.provider[0].path}" : null
}

output "jwt_auth_role" {
  value = var.enable_jwt_auth ? vault_jwt_auth_backend_role.provider[0].role_name : null
}

output "cert_auth_mount_path" {
  value = var.enable_pkcs11_cert_auth ? "auth/${vault_auth_backend.cert[0].path}" : null
}

output "cert_auth_role" {
  value = var.enable_pkcs11_cert_auth ? var.cert_auth_role : null
}
