resource "harborpilot_subscription_entitlement" "max" {
  name = "claude-max-alice"
  spec = jsonencode({
    account_id = harborpilot_provider_account.claude_alice.id
    plan_name  = "Max"
    fee        = "100"
    currency   = "USD"
    period     = "monthly"
    starts_on  = "2026-01-01"
    windows = [
      { name = "five_hour", seconds = 18000, unit = "percent" },
      { name = "seven_day", seconds = 604800, unit = "percent" },
    ]
  })
}
