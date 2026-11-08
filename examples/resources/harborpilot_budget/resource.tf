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

# A one-time budget: a total limit that never resets. Spend counts from the
# budget's creation unless starts_at is set; ends_at is optional. Leave
# timezone out. Needs Harbor Pilot 0.1.4 or later.
resource "harborpilot_budget" "trial_credit" {
  name = "trial-credit"
  spec = jsonencode({
    scope    = "project"
    scope_id = harborpilot_project.checkout.id
    period   = "once"
    limit    = "50"
    currency = "USD"
    ends_at  = "2027-06-30T23:59:59Z"
    enabled  = true
  })
}
