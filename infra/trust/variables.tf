variable "project_id" {
  description = "Existing project that owns the tracker runtime and deploy identity."
  type        = string
  nullable    = false
}

variable "project_number" {
  description = "Numeric project identity used in the workload principal name."
  type        = string
  nullable    = false

  validation {
    condition     = can(regex("^[0-9]+$", var.project_number))
    error_message = "project_number must contain only digits."
  }
}

variable "region" {
  description = "Region of the existing Cloud Run service and Artifact Registry repository."
  type        = string
  nullable    = false
}

variable "state_bucket_name" {
  description = "Existing versioned GCS state bucket."
  type        = string
  nullable    = false
}

variable "artifact_registry_repository_id" {
  description = "Existing repository from which Cloud Run reads the inspected image."
  type        = string
  nullable    = false
}

variable "runtime_service_account_name" {
  description = "Existing zero-permission runtime service-account ID."
  type        = string
  nullable    = false
}

variable "workload_identity_pool_id" {
  description = "Existing pool dedicated to work-tracker."
  type        = string
  default     = "work-tracker"

  validation {
    condition     = can(regex("^[a-z0-9-]{4,32}$", var.workload_identity_pool_id)) && !startswith(var.workload_identity_pool_id, "gcp-")
    error_message = "workload_identity_pool_id must be a valid GCP pool ID."
  }
}

variable "repository_identity" {
  description = "Immutable owner@id/repository@id value issued in the GitHub OIDC subject."
  type        = string
  nullable    = false

  validation {
    condition     = can(regex("^[^/@[:space:]]+@[0-9]+/[^/@[:space:]]+@[0-9]+$", var.repository_identity))
    error_message = "repository_identity must use immutable owner@id/repository@id, never mutable owner/name."
  }
}

variable "repository_ref" {
  description = "One exact Git ref authorized to deploy."
  type        = string
  default     = "refs/heads/main"

  validation {
    condition     = var.repository_ref == "refs/heads/main"
    error_message = "Only refs/heads/main may deploy."
  }
}

variable "deployer_service_account_name" {
  description = "ID of the keyless deploy service account."
  type        = string
  default     = "work-tracker-deployer"

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{4,28}[a-z0-9]$", var.deployer_service_account_name))
    error_message = "deployer_service_account_name must be a valid service-account ID."
  }
}
