locals {
  env         = var.env
  // For names that must remain identical in prod, use empty suffix; for dev, append "-dev"
  name_suffix = var.env == "prod" ? "" : "-${var.env}"
  // For API stage names (if using HTTP API v2, stage names must exist)
  stage_name  = var.env == "prod" ? "prod" : var.env
}