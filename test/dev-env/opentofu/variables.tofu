variable "openbao_address" {
  type        = string
  description = "OpenBao API address used by OpenTofu from the host."
}

variable "openbao_token" {
  type        = string
  description = "Local OpenBao dev root token."
  sensitive   = true
}

variable "openbao_ca_cert_file" {
  type        = string
  description = "OpenBao CA certificate file on the host."
}

variable "openbao_tls_server_name" {
  type        = string
  description = "TLS server name for the OpenBao dev certificate."
  default     = "localhost"
}

variable "transit_mount_path" {
  type        = string
  description = "Transit mount used by the provider."
  default     = "transit"
}

variable "transit_key_name" {
  type        = string
  description = "Transit key used by the provider."
  default     = "k8s-kind-dev-etcd"
}

variable "provider_policy_name" {
  type        = string
  description = "OpenBao policy granted to provider auth roles."
  default     = "openbao-kms-dev-env"
}

variable "jwt_auth_backend_path" {
  type        = string
  description = "JWT auth backend path to create in OpenBao. Do not include the auth/ API prefix."
  default     = "kind-dev-jwt"
}

variable "enable_jwt_auth" {
  type        = bool
  description = "Enable local JWT auth resources for the provider."
  default     = true
}

variable "jwt_auth_role" {
  type        = string
  description = "JWT auth role for the provider."
  default     = "openbao-kms-control-plane"
}

variable "jwt_public_key_file" {
  type        = string
  description = "Local JWT validation public key file."
}

variable "jwt_issuer" {
  type        = string
  description = "JWT issuer bound by OpenBao and expected by the provider."
}

variable "jwt_audience" {
  type        = string
  description = "JWT audience bound by OpenBao and expected by the provider."
}

variable "jwt_subject" {
  type        = string
  description = "JWT subject bound by OpenBao and expected by the provider."
}

variable "enable_pkcs11_cert_auth" {
  type        = bool
  description = "Enable local SoftHSM-backed OpenBao cert-auth resources."
  default     = false
}

variable "cert_auth_backend_path" {
  type        = string
  description = "Cert auth backend path to create in OpenBao. Do not include the auth/ API prefix."
  default     = "kind-dev-cert"
}

variable "cert_auth_role" {
  type        = string
  description = "Cert auth role for the provider."
  default     = "openbao-kms-control-plane"
}

variable "pkcs11_client_ca_file" {
  type        = string
  description = "Client CA generated for local SoftHSM cert auth."
  default     = ""
}

variable "pkcs11_uri_san" {
  type        = string
  description = "URI SAN bound by the local cert-auth role."
  default     = "spiffe://example.org/openbao-kms/dev-env"
}
