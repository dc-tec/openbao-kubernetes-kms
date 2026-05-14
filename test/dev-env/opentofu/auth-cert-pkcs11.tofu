resource "vault_auth_backend" "cert" {
  count = var.enable_pkcs11_cert_auth ? 1 : 0

  path = var.cert_auth_backend_path
  type = "cert"
}

resource "vault_generic_endpoint" "cert_config" {
  count = var.enable_pkcs11_cert_auth ? 1 : 0

  path = "auth/${vault_auth_backend.cert[0].path}/config"
  data_json = jsonencode({
    disable_binding = false
  })

  disable_delete       = true
  ignore_absent_fields = true
}

resource "vault_generic_endpoint" "cert_role" {
  count = var.enable_pkcs11_cert_auth ? 1 : 0

  path = "auth/${vault_auth_backend.cert[0].path}/certs/${var.cert_auth_role}"
  data_json = jsonencode({
    display_name     = var.cert_auth_role
    policies         = [module.openbao_kubernetes_kms.provider_policy_name]
    certificate      = file(var.pkcs11_client_ca_file)
    allowed_uri_sans = [var.pkcs11_uri_san]
    ttl              = "10m"
    max_ttl          = "30m"
  })

  disable_delete       = true
  ignore_absent_fields = true

  depends_on = [
    vault_generic_endpoint.cert_config,
  ]
}
