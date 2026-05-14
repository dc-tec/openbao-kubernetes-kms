provider "vault" {
  address         = var.openbao_address
  token           = var.openbao_token
  ca_cert_file    = var.openbao_ca_cert_file
  tls_server_name = var.openbao_tls_server_name
}
