resource "vault_jwt_auth_backend" "provider" {
  count = var.enable_jwt_auth ? 1 : 0

  path         = var.jwt_auth_backend_path
  bound_issuer = var.jwt_issuer

  # AUTH=oauth2 validates Keycloak tokens through OIDC discovery; the default
  # mode pins the locally generated signing key.
  jwt_validation_pubkeys = var.jwt_oidc_discovery_url == "" ? [file(var.jwt_public_key_file)] : null
  oidc_discovery_url     = var.jwt_oidc_discovery_url == "" ? null : var.jwt_oidc_discovery_url
  oidc_discovery_ca_pem  = var.jwt_oidc_discovery_url == "" ? null : file(var.jwt_oidc_discovery_ca_file)
}

resource "vault_jwt_auth_backend_role" "provider" {
  count = var.enable_jwt_auth ? 1 : 0

  backend                 = vault_jwt_auth_backend.provider[0].path
  role_name               = var.jwt_auth_role
  role_type               = "jwt"
  user_claim              = "sub"
  bound_audiences         = [var.jwt_audience]
  bound_subject           = var.jwt_subject
  token_policies          = [module.openbao_kubernetes_kms.provider_policy_name]
  token_ttl               = 600
  token_max_ttl           = 1800
  token_no_default_policy = true
  clock_skew_leeway       = 30
  expiration_leeway       = 30
}
