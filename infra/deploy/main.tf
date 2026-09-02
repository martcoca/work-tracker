locals {
  hostname     = "tracker.martcoca.com"
  service_name = "tracker-reader"
}

resource "google_cloud_run_v2_service" "reader" {
  project  = var.project_id
  name     = local.service_name
  location = var.region
  ingress  = "INGRESS_TRAFFIC_ALL"

  deletion_protection = true

  template {
    service_account                  = "${var.runtime_service_account_name}@${var.project_id}.iam.gserviceaccount.com"
    max_instance_request_concurrency = 80

    annotations = {
      source-commit = var.source_commit
    }

    scaling {
      min_instance_count = 0
      max_instance_count = 2
    }

    containers {
      image = var.container_image

      ports {
        container_port = 8080
      }

      resources {
        cpu_idle = true
        limits = {
          cpu    = "1"
          memory = "256Mi"
        }
      }

      env {
        name  = "FIREBASE_PROJECT_ID"
        value = var.project_id
      }
      env {
        name  = "PACKET_EXPORT_URL"
        value = var.packet_export_url
      }
      env {
        name  = "TENANT_DIRECTORY_URL"
        value = var.tenant_directory_url
      }
      env {
        name  = "AGENT_GRANTS_URL"
        value = var.agent_grants_url
      }
      env {
        name  = "EXPORT_REFRESH_INTERVAL"
        value = var.export_refresh_interval
      }
      env {
        name  = "EXPORT_FETCH_TIMEOUT"
        value = var.export_fetch_timeout
      }

      startup_probe {
        initial_delay_seconds = 0
        timeout_seconds       = 2
        period_seconds        = 3
        failure_threshold     = 10

        http_get {
          path = "/healthz"
          port = 8080
        }
      }
    }
  }

  traffic {
    type    = "TRAFFIC_TARGET_ALLOCATION_TYPE_LATEST"
    percent = 100
  }
}
