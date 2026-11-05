resource "harborpilot_api_token" "ci" {
  service_principal_id = harborpilot_service_principal.ci.id
  name                 = "pipeline"
  ttl_seconds          = 2592000
}
