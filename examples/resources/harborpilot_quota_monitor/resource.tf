resource "harborpilot_quota_monitor" "deepseek" {
  name = "deepseek-balance"
  spec = jsonencode({
    adapter            = "deepseek"
    account_id         = harborpilot_provider_account.deepseek.id
    api_key_ref        = "env:DEEPSEEK_API_KEY"
    poll_interval_secs = 600
    enabled            = true
  })
}
