locals {
  transit_mount_path = trim(var.transit_mount_path, "/")

  transit_policy_hcl = <<-EOT
  # Read Transit key metadata.
  path "${local.transit_mount_path}/keys/${var.transit_key_name}" {
    capabilities = ["read"]
  }

  # Encrypt with the existing key.
  path "${local.transit_mount_path}/encrypt/${var.transit_key_name}" {
    capabilities = ["update"]
  }

  # Decrypt existing ciphertext.
  path "${local.transit_mount_path}/decrypt/${var.transit_key_name}" {
    capabilities = ["update"]
  }

  # Inspect Transit disable_upsert.
  path "${local.transit_mount_path}/config/keys" {
    capabilities = ["read"]
  }

  # Allow bao-kms-provider doctor to inspect the token's effective capabilities.
  path "sys/capabilities-self" {
    capabilities = ["update"]
  }
  EOT

  token_renewal_policy_hcl = <<-EOT
  # Allow in-process OpenBao token renewal when the JWT role disables the default policy.
  path "auth/token/renew-self" {
    capabilities = ["update"]
  }
  EOT

  provider_policy_hcl = join("\n\n", compact([
    trimspace(local.transit_policy_hcl),
    var.include_token_renewal_capabilities ? trimspace(local.token_renewal_policy_hcl) : "",
  ]))
}

resource "vault_mount" "transit" {
  path        = local.transit_mount_path
  type        = "transit"
  description = var.transit_mount_description
}

resource "vault_generic_endpoint" "transit_disable_upsert" {
  count = var.disable_transit_key_upsert ? 1 : 0

  path = "${vault_mount.transit.path}/config/keys"
  data_json = jsonencode({
    disable_upsert = true
  })

  disable_delete       = true
  ignore_absent_fields = true
}

resource "vault_transit_secret_backend_key" "kubernetes_kms" {
  backend = vault_mount.transit.path
  name    = var.transit_key_name
  type    = var.transit_key_type

  deletion_allowed       = false
  derived                = false
  convergent_encryption  = false
  exportable             = false
  allow_plaintext_backup = false
  auto_rotate_period     = 0

  depends_on = [
    vault_generic_endpoint.transit_disable_upsert,
  ]
}

resource "vault_policy" "provider" {
  name   = var.policy_name
  policy = local.provider_policy_hcl
}
