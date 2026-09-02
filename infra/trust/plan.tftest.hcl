mock_provider "google" {}

variables {
  project_id                      = "project-synthetic"
  project_number                  = "1234567890"
  region                          = "region-synthetic"
  state_bucket_name               = "state-synthetic"
  artifact_registry_repository_id = "images-synthetic"
  runtime_service_account_name    = "reader-synthetic"
  repository_identity             = "owner@123/repository@456"
}

run "one_immutable_main_subject_and_narrow_resources" {
  command = plan

  assert {
    condition     = local.exact_subject == "repo:owner@123/repository@456:ref:refs/heads/main"
    error_message = "Trust must name the immutable repository identity and exact main ref."
  }

  assert {
    condition     = !strcontains(local.exact_subject, "*") && !strcontains(local.exact_subject, "pull_request")
    error_message = "Trust must contain no wildcard or pull-request subject."
  }

  assert {
    condition     = google_cloud_run_v2_service_iam_member.deployer.name == "tracker-reader" && google_cloud_run_v2_service_iam_member.deployer.role == "roles/run.developer"
    error_message = "Cloud Run update authority must be bound to only tracker-reader."
  }

  assert {
    condition     = google_artifact_registry_repository_iam_member.reader.role == "roles/artifactregistry.reader"
    error_message = "The deployer must receive read-only access to the image repository."
  }

  assert {
    condition = (
      google_storage_bucket_iam_member.state.role == "roles/storage.objectAdmin" &&
      strcontains(google_storage_bucket_iam_member.state.condition[0].expression, "projects/_/buckets/state-synthetic\"") &&
      strcontains(google_storage_bucket_iam_member.state.condition[0].expression, "/objects/work-tracker/delivery/") &&
      !strcontains(google_storage_bucket_iam_member.state.condition[0].expression, "/objects/work-tracker/foundation/")
    )
    error_message = "State access must permit bucket listing while restricting objects to delivery state."
  }

  assert {
    condition     = google_project_iam_member.api_keys.role == "roles/serviceusage.apiKeysViewer"
    error_message = "Firebase CLI must receive only the documented read-only API-key prerequisite."
  }
}

run "mutable_repository_identity_is_refused" {
  command = plan

  variables {
    repository_identity = "owner/repository"
  }

  expect_failures = [var.repository_identity]
}

run "non_main_ref_is_refused" {
  command = plan

  variables {
    repository_ref = "refs/heads/release"
  }

  expect_failures = [var.repository_ref]
}
