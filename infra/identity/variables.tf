variable "project_id" {
  description = "Existing billing-enabled Google Cloud project. The Founder supplies it at plan/apply time."
  type        = string
  nullable    = false

  validation {
    condition     = length(trimspace(var.project_id)) > 0
    error_message = "project_id must be supplied outside tracked configuration."
  }
}

variable "region" {
  description = "Founder-selected Cloud Run region. No real topology is committed."
  type        = string
  nullable    = false

  validation {
    condition     = length(trimspace(var.region)) > 0
    error_message = "region is required."
  }
}

variable "hosting_site_id" {
  description = "Globally unique Firebase Hosting site id selected by the Founder."
  type        = string
  nullable    = false
}

variable "runtime_service_account_name" {
  description = "Project-local id for the zero-permission Cloud Run runtime identity."
  type        = string
  nullable    = false
}

variable "container_image" {
  description = "Immutable tracker reader runtime image. Exports are fetched after start and are never baked in."
  type        = string
  nullable    = false

  validation {
    condition     = can(regex("^[^[:space:]]+@sha256:[0-9a-f]{64}$", var.container_image))
    error_message = "container_image must be a full immutable registry reference ending in @sha256:<64 lowercase hex>."
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
    error_message = "export_refresh_interval must be a positive single-unit Go duration such as 5m."
  }
}

variable "export_fetch_timeout" {
  description = "Go duration limiting each outbound export fetch."
  type        = string
  default     = "5s"

  validation {
    condition     = can(regex("^[1-9][0-9]*(ms|s|m|h)$", var.export_fetch_timeout))
    error_message = "export_fetch_timeout must be a positive single-unit Go duration such as 5s."
  }
}

variable "google_oauth_client_id" {
  description = "Google OAuth client id supplied by the Founder."
  type        = string
  sensitive   = true
  nullable    = false
}

variable "google_oauth_client_secret" {
  description = "Google OAuth client secret supplied only through a protected plan/apply input."
  type        = string
  sensitive   = true
  nullable    = false
}
