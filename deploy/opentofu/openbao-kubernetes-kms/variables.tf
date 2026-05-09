variable "provider_name" {
  type        = string
  description = "Kubernetes KMS provider name and providerName key ID scope."
}

variable "cluster_id" {
  type        = string
  description = "Stable identity-bearing cluster or trust-domain ID."
}

variable "openbao_address" {
  type        = string
  description = "OpenBao HTTPS address."
}

variable "openbao_namespace" {
  type        = string
  description = "OpenBao namespace, or empty when namespaces are not used."
  default     = ""
}

variable "openbao_ca_file" {
  type        = string
  description = "Provider-local CA bundle path."
  default     = "/etc/openbao-kms/tls/ca.crt"
}

variable "openbao_tls_name" {
  type        = string
  description = "TLS server name expected from OpenBao."
}

variable "openbao_instance_id" {
  type        = string
  description = "Stable identity-bearing OpenBao instance ID."
}

variable "auth_mount_path" {
  type        = string
  description = "OpenBao JWT auth mount path."
  default     = "auth/k8s-workload-a-jwt"
}

variable "auth_role" {
  type        = string
  description = "OpenBao JWT auth role."
  default     = "openbao-kms-control-plane"
}

variable "jwt_file" {
  type        = string
  description = "Provider-local JWT file path."
  default     = "/var/lib/openbao-kms/identity.jwt"
}

variable "transit_mount_path" {
  type        = string
  description = "OpenBao Transit mount path."
  default     = "transit"
}

variable "transit_key_name" {
  type        = string
  description = "OpenBao Transit key name."
  default     = "k8s-workload-a-etcd"
}

variable "transit_mount_id" {
  type        = string
  description = "Stable identity-bearing Transit mount ID."
}

variable "transit_key_lineage" {
  type        = string
  description = "Stable identity-bearing lineage ID for the Transit key."
}

variable "socket_path" {
  type        = string
  description = "Unix socket path served by the provider."
  default     = "/run/openbao-kms/kms.sock"
}

variable "socket_group" {
  type        = string
  description = "Socket group name for systemd or decimal host GID for static pod mode."
}

variable "metrics_address" {
  type        = string
  description = "Metrics listen address."
  default     = "127.0.0.1:8081"
}

variable "health_address" {
  type        = string
  description = "Health listen address."
  default     = "127.0.0.1:8082"
}

variable "state_path" {
  type        = string
  description = "Provider state file path."
  default     = "/var/lib/openbao-kms/state/key-registry.json"
}

variable "encrypted_resources" {
  type        = list(string)
  description = "Kubernetes API resources covered by the EncryptionConfiguration."
  default     = ["secrets", "configmaps"]
}
