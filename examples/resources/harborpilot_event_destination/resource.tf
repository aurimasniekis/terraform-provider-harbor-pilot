resource "harborpilot_event_destination" "webhook" {
  name = "audit-webhook"
  spec = jsonencode({
    type        = "webhook"
    webhook_url = "https://audit.example.com/hooks/harbor-pilot"
    filter      = { events = ["config.*", "access.*"] }
    enabled     = true
  })
  secrets_wo         = { signing_secret = var.webhook_signing_secret }
  secrets_wo_version = 1
}
