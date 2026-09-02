# Synthetic plan inputs only. Immutable repository identity includes both owner and
# repository numeric IDs; mutable owner/name is rejected by the module.
project_id                      = "project-synthetic"
project_number                  = "1234567890"
region                          = "region-synthetic"
state_bucket_name               = "state-synthetic"
artifact_registry_repository_id = "images-synthetic"
runtime_service_account_name    = "reader-synthetic"
repository_identity             = "owner@123/repository@456"
