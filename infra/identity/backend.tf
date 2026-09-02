terraform {
  backend "gcs" {
    prefix = "work-tracker/foundation"
  }
}
