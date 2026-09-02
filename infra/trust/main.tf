locals {
  service_name = "tracker-reader"
  exact_subject = join("", [
    "repo:",
    var.repository_identity,
    ":ref:",
    var.repository_ref,
  ])
  exact_principal = join("", [
    "principal://iam.googleapis.com/projects/",
    var.project_number,
    "/locations/global/workloadIdentityPools/",
    var.workload_identity_pool_id,
    "/subject/",
    local.exact_subject,
  ])
  deployer_email  = "${var.deployer_service_account_name}@${var.project_id}.iam.gserviceaccount.com"
  deployer_name   = "projects/${var.project_id}/serviceAccounts/${local.deployer_email}"
  deployer_member = "serviceAccount:${local.deployer_email}"
  runtime_service_account = join("", [
    "projects/",
    var.project_id,
    "/serviceAccounts/",
    var.runtime_service_account_name,
    "@",
    var.project_id,
    ".iam.gserviceaccount.com",
  ])
}

check "exact_subject_length" {
  assert {
    condition     = length(local.exact_subject) <= 127
    error_message = "The immutable GitHub subject exceeds GCP's 127-character google.subject limit."
  }
}

resource "google_service_account" "deployer" {
  project      = var.project_id
  account_id   = var.deployer_service_account_name
  display_name = "Work Tracker deployer"
  description  = "Keyless main-only identity for one existing Cloud Run service and Firebase Hosting"

  lifecycle {
    prevent_destroy = true
  }
}

# The existing work-tracker provider admits only the immutable repository. This binding
# narrows the deploy identity further to one complete subject: refs/heads/main only.
resource "google_service_account_iam_member" "exact_main" {
  service_account_id = local.deployer_name
  role               = "roles/iam.workloadIdentityUser"
  member             = local.exact_principal

  depends_on = [google_service_account.deployer]
}

# Cloud Run Developer is bound to one already-existing service, not the project. It can
# update that service but cannot create a second one.
resource "google_cloud_run_v2_service_iam_member" "deployer" {
  project  = var.project_id
  location = var.region
  name     = local.service_name
  role     = "roles/run.developer"
  member   = local.deployer_member
}

# Cloud Run validates that the deployer may read the exact repository whose digest it
# attaches. No upload, delete, repository administration, or IAM permission is granted.
resource "google_artifact_registry_repository_iam_member" "reader" {
  project    = var.project_id
  location   = var.region
  repository = var.artifact_registry_repository_id
  role       = "roles/artifactregistry.reader"
  member     = local.deployer_member
}

# Attaching an identity to a revision requires actAs. Scope it to the runtime identity,
# which itself has no project roles.
resource "google_service_account_iam_member" "runtime_user" {
  service_account_id = local.runtime_service_account
  role               = "roles/iam.serviceAccountUser"
  member             = local.deployer_member
}

# GCS backend locking and state replacement require create/update/delete on state objects.
# Object listing is evaluated on the bucket resource and cannot be prefix-restricted with
# resource.name. Permit that read-only bucket-level operation, while every object read or
# write remains restricted to delivery state. Foundation state contains the OAuth client
# secret and must remain unreadable to the deploy identity. No bucket administration is
# granted.
resource "google_storage_bucket_iam_member" "state" {
  bucket = var.state_bucket_name
  role   = "roles/storage.objectAdmin"
  member = local.deployer_member

  condition {
    title       = "work_tracker_delivery_state_only"
    description = "List the bucket; read, lock, and replace only delivery state objects"
    expression = join(" || ", [
      "resource.name == \"projects/_/buckets/${var.state_bucket_name}\"",
      "resource.name.startsWith(\"projects/_/buckets/${var.state_bucket_name}/objects/work-tracker/delivery/\")",
    ])
  }
}

# Firebase documents that custom roles cannot control Hosting. This is the narrowest
# supported Hosting write role, in the dedicated tracker project. The workflow exposes no
# command that creates, disables, or deletes a site; it only creates a version and release.
resource "google_project_iam_member" "hosting" {
  project = var.project_id
  role    = "roles/firebasehosting.admin"
  member  = local.deployer_member
}

# Firebase documents this additional read-only role as required for CLI deploys. It can
# inspect API-key metadata but cannot create, update, delete, or reveal key material.
resource "google_project_iam_member" "api_keys" {
  project = var.project_id
  role    = "roles/serviceusage.apiKeysViewer"
  member  = local.deployer_member
}
