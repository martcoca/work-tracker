output "identity_callback_url" {
  description = "Exact callback URL to register for the Google provider."
  value       = local.callback_url
}

output "identity_logout_url" {
  description = "Exact post-logout return URL."
  value       = local.logout_url
}

output "firestore_database_id" {
  description = "Durable packet event database selected for the Cloud Run runtime."
  value       = google_firestore_database.events.name
}

output "apply_prerequisites" {
  description = "Human-owned facts deliberately not created by this plan."
  value = {
    hostname                    = local.hostname
    dns_and_certificate_managed = false
    dns_and_certificate_reason  = "The DNS zone remains at the Founder-controlled registrar; managing its records or Firebase certificate challenge here would require a DNS credential this repository deliberately does not hold."
    required_dns_records = {
      hosting = {
        name  = local.hostname
        type  = "CNAME"
        value = "${var.hosting_site_id}.web.app."
      }
      certificate = {
        name  = "_acme-challenge.${local.hostname}"
        type  = "TXT"
        value = var.custom_domain_acme_challenge
      }
    }
    tenant_claims_managed     = false
    cloud_run_managed_by_plan = false
  }
}
