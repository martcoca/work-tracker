terraform {
  required_version = ">= 1.12.0, < 2.0.0"

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 7.0"
    }
  }
}

provider "google" {
  project = var.project_id

  # The keyless deploy account belongs to this project, so its quota project is already
  # correct. A user-project override would require an unnecessary project-wide
  # serviceusage.services.use grant.
}
