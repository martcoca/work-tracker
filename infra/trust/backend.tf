terraform {
  backend "gcs" {
    prefix = "work-tracker/deploy-trust"
  }
}
