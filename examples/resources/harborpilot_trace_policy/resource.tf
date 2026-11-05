resource "harborpilot_trace_policy" "three_days" {
  name = "three-days"
  spec = jsonencode({
    mode                = "masked_full"
    retention           = "3d"
    redaction_policy_id = harborpilot_privacy_policy.strict.id
  })
}
