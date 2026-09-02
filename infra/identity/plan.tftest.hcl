mock_provider "google" {}
mock_provider "google-beta" {}

variables {
  project_id                   = "project-synthetic"
  region                       = "region-synthetic"
  hosting_site_id              = "hosting-synthetic"
  runtime_service_account_name = "reader-synthetic"
  google_oauth_client_id       = "client-synthetic"
  google_oauth_client_secret   = "secret-synthetic-not-a-credential"
}

run "clean_read_only_plan" {
  command = plan

  assert {
    condition     = length(google_identity_platform_config.tracker.authorized_domains) == 1 && google_identity_platform_config.tracker.authorized_domains[0] == "tracker.martcoca.com"
    error_message = "Identity Platform must authorize only tracker.martcoca.com."
  }

  assert {
    condition     = google_identity_platform_config.tracker.client[0].permissions[0].disabled_user_signup
    error_message = "Public client signup must remain disabled."
  }

  assert {
    condition     = google_identity_platform_default_supported_idp_config.google.idp_id == "google.com"
    error_message = "The plan must use the allocated tier-1 Google provider."
  }

  assert {
    condition     = output.identity_callback_url == "https://tracker.martcoca.com/__/auth/handler"
    error_message = "The callback URL must use the settled hostname."
  }

  assert {
    condition     = output.identity_logout_url == "https://tracker.martcoca.com/signed-out"
    error_message = "The logout URL must use the settled hostname."
  }
}
