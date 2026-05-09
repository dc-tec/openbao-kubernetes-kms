output "transit_mount_path" {
  description = "Transit mount path configured for the provider."
  value       = vault_mount.transit.path
}

output "transit_key_name" {
  description = "Transit key name configured for the provider."
  value       = vault_transit_secret_backend_key.kubernetes_kms.name
}

output "transit_key_latest_version" {
  description = "Latest Transit key version reported by OpenBao."
  value       = vault_transit_secret_backend_key.kubernetes_kms.latest_version
}

output "provider_policy_name" {
  description = "OpenBao policy name for provider auth roles."
  value       = vault_policy.provider.name
}

output "provider_policy_hcl" {
  description = "Rendered least-privilege OpenBao policy."
  value       = vault_policy.provider.policy
}
