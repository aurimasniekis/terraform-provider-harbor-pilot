# An orchestrator's read-only identity. Its API token carries these
# permissions; usage.read covers GET /api/v1/budgets/status.
resource "harborpilot_service_principal" "orchestrator" {
  name        = "orchestrator"
  permissions = ["config.read", "usage.read", "activity.read"]
}

# Issue the token during the run and hand it to another provider's
# write-only argument. revoke_on_close = false keeps it valid for 30 days
# after Terraform finishes; omit it to revoke the token when the run ends.
ephemeral "harborpilot_service_principal_token" "orchestrator" {
  service_principal_id = harborpilot_service_principal.orchestrator.id
  name                 = "orchestrator"
  ttl_seconds          = 30 * 24 * 3600
  revoke_on_close      = false
}
