resource "harborpilot_provider_account" "anthropic" {
  name = "anthropic-team-key"
  spec = jsonencode({
    provider_id         = harborpilot_provider.anthropic.id
    billing             = "metered_api"
    credential_ref      = "env:HP_SECRET_ANTHROPIC_API_KEY" # resolved by the server
    requests_per_minute = 120
    labels              = { team = "platform" }
    enabled             = true
  })
}

# Alternatively, store the credential (write-only, Terraform 1.11+).
resource "harborpilot_provider_account" "stored" {
  name = "anthropic-stored-key"
  spec = jsonencode({
    provider_id = harborpilot_provider.anthropic.id
    billing     = "metered_api"
    enabled     = true
  })
  secrets_wo         = { credential = var.anthropic_api_key } # an ephemeral or sensitive variable
  secrets_wo_version = 1                                      # bump to rotate
}
