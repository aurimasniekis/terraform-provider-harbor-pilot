resource "harborpilot_client_binding" "claude_code" {
  name = "alice-claude-code"
  spec = jsonencode({
    display_name     = "Alice's Claude Code"
    owner_kind       = "user"
    owner_id         = "usr_01h..."
    plan_id          = harborpilot_routing_plan.default.id
    project_id       = harborpilot_project.checkout.id
    expected_harness = "claude_code"
    limits           = { requests_per_minute = 120, max_concurrency = 4 }
    trust            = { session_hints = true }
    enabled          = true
  })
}
