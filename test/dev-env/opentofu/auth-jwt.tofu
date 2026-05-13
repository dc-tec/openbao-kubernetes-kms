resource "vault_jwt_auth_backend" "provider" {
  count = var.enable_jwt_auth ? 1 : 0

  path                   = var.jwt_auth_backend_path
  jwt_validation_pubkeys = [file(var.jwt_public_key_file)]
  bound_issuer           = var.jwt_issuer
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
