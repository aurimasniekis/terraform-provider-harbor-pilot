resource "harborpilot_pool" "coding" {
  name = "coding"
  spec = jsonencode({
    strategy = "ordered" # ordered, weighted, reset_soonest, least_used, round_robin
    affinity = "session"
    members = [
      { account_id = harborpilot_provider_account.subscription.id, model = "sonnet", priority = 1, weight = 1 },
      { account_id = harborpilot_provider_account.anthropic.id, model = "claude-sonnet-4-5", priority = 2, weight = 1 },
    ]
  })
}
