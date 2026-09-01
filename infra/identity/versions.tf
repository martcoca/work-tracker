terraform {
  required_version = ">= 1.12.0, < 2.0.0"

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 7.0"
    }
    google-beta = {
      source  = "hashicorp/google-beta"
      version = "~> 7.0"
    }
  }
}

provider "google" {
  project = var.project_id

  # Identity Platform and Firebase reject ADC requests that carry no billing
  # project: they bill Google's default client project, where the API is
  # disabled. Attribute quota to the project that actually owns the resources.
  billing_project       = var.project_id
  user_project_override = true
}

provider "google-beta" {
  project = var.project_id

  # Identity Platform and Firebase reject ADC requests that carry no billing
  # project: they bill Google's default client project, where the API is
  # disabled. Attribute quota to the project that actually owns the resources.
  billing_project       = var.project_id
  user_project_override = true
}
