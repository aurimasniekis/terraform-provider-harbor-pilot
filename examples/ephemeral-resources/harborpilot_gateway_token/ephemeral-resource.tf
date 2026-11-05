# Issue a short-lived token for one run and hand it to a write-only
# argument; it is revoked when Terraform finishes.
ephemeral "harborpilot_gateway_token" "relay" {
  binding_id  = harborpilot_client_binding.relay.id
  name        = "relay"
  ttl_seconds = 900
}

resource "harborpilot_notification_channel" "relay" {
  name = "relay-webhook"
  spec = jsonencode({
    type        = "webhook"
    webhook_url = "https://relay.example.com/notify"
    enabled     = true
  })
  secrets_wo         = { secret = ephemeral.harborpilot_gateway_token.relay.token }
  secrets_wo_version = 1
}
