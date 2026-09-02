mock_provider "google" {}

variables {
  project_id                   = "project-synthetic"
  region                       = "region-synthetic"
  hosting_site_id              = "hosting-synthetic"
  runtime_service_account_name = "reader-synthetic"
  container_image              = "registry.invalid/synthetic/tracker@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  source_commit                = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
}

run "immutable_zero_idle_revision" {
  command = plan

  assert {
    condition     = google_cloud_run_v2_service.reader.deletion_protection
    error_message = "The deployed service must retain deletion protection."
  }

  assert {
    condition     = google_cloud_run_v2_service.reader.template[0].annotations["source-commit"] == var.source_commit
    error_message = "The revision must name the merge commit."
  }

  assert {
    condition     = google_cloud_run_v2_service.reader.template[0].containers[0].image == var.container_image
    error_message = "The revision must use the exact pushed digest."
  }

  assert {
    condition     = google_cloud_run_v2_service.reader.template[0].scaling[0].min_instance_count == 0
    error_message = "The service must scale to zero."
  }

  assert {
    condition = tomap({
      for item in google_cloud_run_v2_service.reader.template[0].containers[0].env : item.name => item.value
      }) == tomap({
      FIREBASE_PROJECT_ID     = "project-synthetic"
      PACKET_EXPORT_URL       = "https://tracker.martcoca.com/packets.json"
      TENANT_DIRECTORY_URL    = "https://identity.martcoca.com/tenant-directory.json"
      AGENT_GRANTS_URL        = "https://identity.martcoca.com/agent-grants.json"
      EXPORT_REFRESH_INTERVAL = "5m"
      EXPORT_FETCH_TIMEOUT    = "5s"
    })
    error_message = "The revision must use public runtime exports and bounded refresh timing."
  }

  assert {
    condition = (
      jsondecode(output.firebase_hosting_configuration).hosting.site == "hosting-synthetic" &&
      jsondecode(output.firebase_hosting_configuration).hosting.rewrites[0].source == "/api/**" &&
      jsondecode(output.firebase_hosting_configuration).hosting.rewrites[0].run.pinTag &&
      jsondecode(output.firebase_hosting_configuration).hosting.headers[0].source == "**" &&
      length(jsondecode(output.firebase_hosting_configuration).hosting.headers[0].headers) == 3
    )
    error_message = "Hosting output must retain Firebase CLI source/header-list/pinTag schema."
  }
}

run "floating_image_is_refused" {
  command = plan

  variables {
    container_image = "registry.invalid/synthetic/tracker:latest"
  }

  expect_failures = [var.container_image]
}

run "short_commit_is_refused" {
  command = plan

  variables {
    source_commit = "bbbbbbbb"
  }

  expect_failures = [var.source_commit]
}
