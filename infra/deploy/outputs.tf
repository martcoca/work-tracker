output "firebase_hosting_configuration" {
  description = "Firebase CLI configuration generated from the applied delivery inputs."
  value = jsonencode({
    hosting = {
      site      = var.hosting_site_id
      public    = "dist"
      cleanUrls = true
      rewrites = [
        {
          source = "/api/**"
          run = {
            serviceId = local.service_name
            region    = var.region
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
          # script-src admits apis.google.com and nothing else beyond self. Firebase Auth's
          # redirect flow loads https://apis.google.com/js/api.js to host the gapi iframe
          # that completes sign-in; with script-src 'self' alone the browser blocks it and
          # the SDK surfaces the failure only as auth/internal-error, which names nothing.
          # frame-src admits the same origin because that iframe is framed from it.
          { key = "Content-Security-Policy", value = "default-src 'self'; connect-src 'self' https://*.googleapis.com; frame-src 'self' https://accounts.google.com https://apis.google.com; script-src 'self' https://apis.google.com; style-src 'self'; img-src 'self' data:; base-uri 'none'; form-action 'self'; frame-ancestors 'none'" },
          { key = "Referrer-Policy", value = "no-referrer" },
          { key = "X-Content-Type-Options", value = "nosniff" },
        ]
      }]
    }
  })
}

output "cloud_run_service_name" {
  description = "Stable service name used by provenance verification."
  value       = google_cloud_run_v2_service.reader.name
}
