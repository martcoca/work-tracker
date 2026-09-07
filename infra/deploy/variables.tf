variable "project_id" {
  description = "Existing GCP project supplied outside tracked configuration."
  type        = string
  nullable    = false

  validation {
    condition     = length(trimspace(var.project_id)) > 0
    error_message = "project_id is required."
  }
}

variable "region" {
  description = "Existing Cloud Run region supplied outside tracked configuration."
  type        = string
  nullable    = false

  validation {
    condition     = length(trimspace(var.region)) > 0
    error_message = "region is required."
  }
}

variable "hosting_site_id" {
  description = "Existing Firebase Hosting site supplied outside tracked configuration."
  type        = string
  nullable    = false
}

variable "runtime_service_account_name" {
  description = "Project-local ID of the existing runtime identity with conditioned Firestore data access."
  type        = string
  nullable    = false
}

variable "firestore_database_id" {
  description = "Existing Firestore database containing the append-only packet event log."
  type        = string
  default     = "(default)"

  validation {
    condition     = var.firestore_database_id == "(default)"
    error_message = "The runtime is confined to the plan-managed default Firestore database."
  }
}

variable "container_image" {
  description = "Exact registry digest returned by pushing the already-inspected merge image."
  type        = string
  nullable    = false

  validation {
    condition     = can(regex("^[^[:space:]]+@sha256:[0-9a-f]{64}$", var.container_image))
    error_message = "container_image must end in one immutable lowercase sha256 digest."
  }
}

variable "source_commit" {
  description = "Full Git commit that built this revision."
  type        = string
  nullable    = false

  validation {
    condition     = can(regex("^[0-9a-f]{40}$", var.source_commit))
    error_message = "source_commit must be one full lowercase Git object ID."
  }
}

variable "packet_export_url" {
  description = "Public static packet export fetched by the reader."
  type        = string
  default     = "https://tracker.martcoca.com/packets.json"

  validation {
    condition     = can(regex("^https://[^?#]+$", var.packet_export_url))
    error_message = "packet_export_url must be a query-free HTTPS URL."
  }
}

variable "repository_packet_export_url" {
  description = "Public transitional repository-only packet export retained until E04."
  type        = string
  default     = "https://tracker.martcoca.com/repository-packets.json"

  validation {
    condition     = can(regex("^https://[^?#]+$", var.repository_packet_export_url))
    error_message = "repository_packet_export_url must be a query-free HTTPS URL."
  }
}

variable "tenant_directory_url" {
  description = "Public static tenant-directory export fetched by the reader."
  type        = string
  default     = "https://identity.martcoca.com/tenant-directory.json"

  validation {
    condition     = can(regex("^https://[^?#]+$", var.tenant_directory_url))
    error_message = "tenant_directory_url must be a query-free HTTPS URL."
  }
}

variable "agent_grants_url" {
  description = "Public static agent-grants export fetched by the reader."
  type        = string
  default     = "https://identity.martcoca.com/agent-grants.json"

  validation {
    condition     = can(regex("^https://[^?#]+$", var.agent_grants_url))
    error_message = "agent_grants_url must be a query-free HTTPS URL."
  }
}

variable "export_refresh_interval" {
  description = "Go duration between background export refreshes."
  type        = string
  default     = "5m"

  validation {
    condition     = can(regex("^[1-9][0-9]*(ms|s|m|h)$", var.export_refresh_interval))
    error_message = "export_refresh_interval must be a positive single-unit Go duration."
  }
}

variable "export_fetch_timeout" {
  description = "Go duration limiting each outbound export fetch."
  type        = string
  default     = "5s"

  validation {
    condition     = can(regex("^[1-9][0-9]*(ms|s|m|h)$", var.export_fetch_timeout))
    error_message = "export_fetch_timeout must be a positive single-unit Go duration."
  }
}
