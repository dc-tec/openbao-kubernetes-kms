variable "transit_mount_path" {
  type        = string
  description = "OpenBao Transit mount path to create or manage."
  default     = "transit"

  validation {
    condition = (
      trim(var.transit_mount_path, "/") == var.transit_mount_path &&
      !strcontains(var.transit_mount_path, "//") &&
      var.transit_mount_path != ""
    )
    error_message = "transit_mount_path must be a non-empty relative OpenBao path without leading, trailing, or repeated slashes."
  }
}

variable "transit_mount_description" {
  type        = string
  description = "Description for the Transit secrets engine mount."
  default     = "OpenBao Kubernetes KMS provider Transit engine"
}

variable "transit_key_name" {
  type        = string
  description = "OpenBao Transit key name for Kubernetes API resource encryption."
  default     = "k8s-workload-a-etcd"
}

variable "transit_key_type" {
  type        = string
  description = "Transit key type supported by the provider."
  default     = "aes256-gcm96"

  validation {
    condition     = var.transit_key_type == "aes256-gcm96"
    error_message = "transit_key_type must be aes256-gcm96."
  }
}

variable "policy_name" {
  type        = string
  description = "Name of the OpenBao policy granted to provider auth roles."
  default     = "openbao-kms-workload-a"
}

variable "disable_transit_key_upsert" {
  type        = bool
  description = "Write Transit mount key configuration with disable_upsert=true."
  default     = true
}

variable "include_token_renewal_capabilities" {
  type        = bool
  description = "Add auth/token/renew-self to the provider policy for token renewal."
  default     = true
}
