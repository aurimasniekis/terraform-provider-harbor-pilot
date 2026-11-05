resource "harborpilot_service_principal" "ci" {
  name        = "ci"
  description = "CI pipelines"
  permissions = ["config.read", "tokens.manage"]
}
