terraform {
  backend "gcs" {
    prefix = "work-tracker/delivery"
  }
}
