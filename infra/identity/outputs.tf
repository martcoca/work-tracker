output "identity_callback_url" {
  description = "Exact callback URL to register for the Google provider."
  value       = local.callback_url
}

output "identity_logout_url" {
  description = "Exact post-logout return URL."
  value       = local.logout_url
}

output "apply_prerequisites" {
  description = "Human-owned facts deliberately not created by this plan."
  value = {
    hostname                    = local.hostname
    dns_and_certificate_managed = false
    tenant_claims_managed       = false
    cloud_run_managed_by_plan   = false
  }
}
