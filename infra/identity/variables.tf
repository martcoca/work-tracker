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
  description = "Immutable tracker reader image containing the last verified exports."
  type        = string
  nullable    = false

  validation {
    condition     = length(trimspace(var.container_image)) > 0
    error_message = "container_image is required."
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
