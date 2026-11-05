resource "harborpilot_routing_plan_version" "default" {
  plan_id = harborpilot_routing_plan.default.id
  note    = "Subscription first, API fallback"
  document = jsonencode({
    model_mappings   = [{ match = "claude-*", alias_id = harborpilot_model_alias.coding.id }]
    default_alias_id = harborpilot_model_alias.coding.id
    restrictions     = { denied_provider_ids = [] }
    fallback = {
      steps               = ["retry", "alternate_account", "stop"]
      max_attempts        = 3
      allow_paid_fallback = true
    }
    privacy = {
      policy_id       = harborpilot_privacy_policy.strict.id
      trace_policy_id = harborpilot_trace_policy.three_days.id
    }
  })
}
