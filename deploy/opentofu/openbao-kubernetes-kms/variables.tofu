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
  description = "Transit key type. aes256-gcm96 is the v0.1 recommended default."
  default     = "aes256-gcm96"

  validation {
    condition     = contains(["aes256-gcm96", "xchacha20-poly1305"], var.transit_key_type)
    error_message = "transit_key_type must be aes256-gcm96 or xchacha20-poly1305."
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
