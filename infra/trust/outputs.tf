output "deployer_service_account_email" {
  description = "Repository variable GCP_DEPLOYER_SERVICE_ACCOUNT after the Founder applies trust."
  value       = local.deployer_email
  sensitive   = true
}

output "exact_github_subject" {
  description = "One immutable GitHub subject admitted to the deploy identity."
  value       = local.exact_subject
  sensitive   = true
}
