output "identity_callback_url" {
  description = "Exact callback URL to register for the Google provider."
  value       = local.callback_url
}

output "identity_logout_url" {
  description = "Exact post-logout return URL."
  value       = local.logout_url
}

output "firebase_hosting_configuration" {
  description = "Configuration the Founder deploys after DNS, certificate, app image, and identity prerequisites exist."
  value = jsonencode({
    hosting = {
      site      = google_firebase_hosting_site.tracker.site_id
      public    = "dist"
      cleanUrls = true
      rewrites = [
        {
          source = "/api/**"
          run = {
            serviceId = google_cloud_run_v2_service.reader.name
            region    = google_cloud_run_v2_service.reader.location
            pinTag    = true
          }
        },
        {
          source      = "**"
          destination = "/index.html"
        },
      ]
      headers = [{
        source = "**"
        headers = [
          { key = "Content-Security-Policy", value = "default-src 'self'; connect-src 'self' https://*.googleapis.com; frame-src 'self' https://accounts.google.com; script-src 'self'; style-src 'self'; img-src 'self' data:; base-uri 'none'; form-action 'self'; frame-ancestors 'none'" },
          { key = "Referrer-Policy", value = "no-referrer" },
          { key = "X-Content-Type-Options", value = "nosniff" },
        ]
      }]
    }
  })
}

output "apply_prerequisites" {
  description = "Human-owned facts deliberately not created by this plan."
  value = {
    hostname              = local.hostname
    dns_ready             = false
    certificate_ready     = false
    tenant_claims_ready   = false
    container_image_ready = false
  }
}
