resource "harborpilot_budget" "checkout" {
  name = "checkout-monthly"
  spec = jsonencode({
    scope       = "project"
    scope_id    = harborpilot_project.checkout.id
    period      = "month"
    limit       = "200"
    currency    = "USD"
    enforcement = "block"
    warn_at     = 0.8
    enabled     = true
  })
}
