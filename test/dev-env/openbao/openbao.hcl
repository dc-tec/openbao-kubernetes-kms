storage "raft" {
  path = "/vault/data"
  node_id = "openbao-kms-dev-openbao-01"
  performance_multiplier = 1
}

listener "tcp" {
  address = "0.0.0.0:8200"
  cluster_address = "0.0.0.0:8201"
  tls_disable = 0
  tls_cert_file = "/vault/certs/openbao-kms-dev-openbao-01/full-chain.pem"
  tls_key_file = "/vault/certs/openbao-kms-dev-openbao-01/private-key.pem"
  tls_disable_client_certs = false
  max_request_duration = "90s"
}

cluster_name = "openbao-kms-dev"
api_addr = "https://openbao:8200"
cluster_addr = "https://openbao:8201"
raw_storage_endpoint = false

ui = true

log_level = "INFO"
log_format = "json"

disable_mlock = true
disable_cache = false
default_max_request_duration = "90s"

seal "static" {
  current_key_id = "local-1"
  current_key = "file:///vault/seal/static-unseal.key"
  disabled = "false"
}
