locals {
  hostname               = "tracker.martcoca.com"
  callback_url           = "https://${local.hostname}/__/auth/handler"
  logout_url             = "https://${local.hostname}/signed-out"
  cloud_run_service_name = "tracker-reader"

  required_services = toset([
    "firebase.googleapis.com",
    "firebasehosting.googleapis.com",
    "iam.googleapis.com",
    "identitytoolkit.googleapis.com",
    "run.googleapis.com",
  ])
}

resource "google_project_service" "required" {
  for_each = local.required_services

  project            = var.project_id
  service            = each.value
  disable_on_destroy = false
}

resource "google_firebase_project" "tracker" {
  provider = google-beta
  project  = var.project_id

  depends_on = [google_project_service.required]
}

resource "google_firebase_web_app" "tracker" {
  provider     = google-beta
  project      = var.project_id
  display_name = "Work Tracker"

  deletion_policy = "ABANDON"
  depends_on      = [google_firebase_project.tracker]
}

resource "google_firebase_hosting_site" "tracker" {
  provider = google-beta
  project  = var.project_id
  site_id  = var.hosting_site_id
  app_id   = google_firebase_web_app.tracker.app_id

  deletion_policy = "ABANDON"
}

resource "google_identity_platform_config" "tracker" {
  project = var.project_id

  authorized_domains         = [local.hostname]
  autodelete_anonymous_users = true

  client {
    permissions {
      disabled_user_deletion = true
      disabled_user_signup   = true
    }
  }

  depends_on = [google_project_service.required]
}

resource "google_identity_platform_default_supported_idp_config" "google" {
  project       = var.project_id
  idp_id        = "google.com"
  enabled       = true
  client_id     = var.google_oauth_client_id
  client_secret = var.google_oauth_client_secret

  deletion_policy = "ABANDON"
  depends_on      = [google_identity_platform_config.tracker]
}

resource "google_service_account" "reader" {
  project      = var.project_id
  account_id   = var.runtime_service_account_name
  display_name = "Work Tracker read-only runtime"

  create_ignore_already_exists = false

  lifecycle {
    prevent_destroy = true
  }

  depends_on = [google_project_service.required]
}

# Firebase Hosting invokes the same-origin API without a Google IAM credential. The API
# itself verifies every Identity Platform ID token and tenant claim before reading data.
resource "google_cloud_run_v2_service_iam_member" "public_entrypoint" {
  project  = var.project_id
  location = var.region
  name     = local.cloud_run_service_name
  role     = "roles/run.invoker"
  member   = "allUsers"
}
