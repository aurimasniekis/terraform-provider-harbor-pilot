# Project budgets with API fallback: requests go to a subscription first and
# fall back to a metered API key only when the subscription cannot serve
# them. Each project has a monthly budget that blocks new paid attempts once
# it would be exceeded; a global budget warns early.

variable "prefix" {
  type    = string
  default = "budgets"
}

resource "harborpilot_workspace" "eng" {
  name = "${var.prefix}-engineering"
  spec = jsonencode({ display_name = "Engineering" })
}

resource "harborpilot_project" "app" {
  for_each = toset(["checkout", "search"])
  name     = "${var.prefix}-${each.key}"
  spec = jsonencode({
    workspace_id = harborpilot_workspace.eng.id
    display_name = title(each.key)
  })
}

resource "harborpilot_provider" "claude_code" {
  name = "${var.prefix}-claude-code"
  spec = jsonencode({
    type   = "claude_code"
    models = [{ id = "sonnet", display_name = "Sonnet", context_window = 200000, max_output_tokens = 64000, capabilities = ["tools", "streaming"] }]
  })
}

resource "harborpilot_provider" "anthropic" {
  name = "${var.prefix}-anthropic-api"
  spec = jsonencode({
    type = "anthropic"
    models = [{
      id                = "claude-sonnet-4-5"
      display_name      = "Claude Sonnet 4.5 (API)"
      context_window    = 200000
      max_output_tokens = 64000
      capabilities      = ["tools", "streaming"]
      # List prices make the cost of every fallback visible before the invoice.
      pricing = {
        currency            = "USD"
        input_per_mtok      = "3"
        output_per_mtok     = "15"
        cache_read_per_mtok = "0.3"
        price_version       = "2026-11"
      }
    }]
  })
}

resource "harborpilot_provider_account" "subscription" {
  name = "${var.prefix}-claude-team"
  spec = jsonencode({
    provider_id    = harborpilot_provider.claude_code.id
    billing        = "subscription"
    credential_ref = "env:CLAUDE_CODE_OAUTH_TOKEN"
    enabled        = true
  })
}

resource "harborpilot_provider_account" "api" {
  name = "${var.prefix}-anthropic-key"
  spec = jsonencode({
    provider_id         = harborpilot_provider.anthropic.id
    billing             = "metered_api"
    credential_ref      = "env:ANTHROPIC_API_KEY"
    requests_per_minute = 120
    enabled             = true
  })
}

resource "harborpilot_pool" "coding" {
  name = "${var.prefix}-coding"
  spec = jsonencode({
    strategy = "ordered"
    members = [
      { account_id = harborpilot_provider_account.subscription.id, model = "sonnet", priority = 1, weight = 1 },
      { account_id = harborpilot_provider_account.api.id, model = "claude-sonnet-4-5", priority = 2, weight = 1 },
    ]
  })
}

resource "harborpilot_model_alias" "coding" {
  name = "${var.prefix}/coding"
  spec = jsonencode({ pool_id = harborpilot_pool.coding.id })
}

resource "harborpilot_routing_plan" "plan" {
  name = "${var.prefix}-plan"
  spec = jsonencode({ display_name = "Subscription first, API fallback" })
}

resource "harborpilot_routing_plan_version" "plan" {
  plan_id = harborpilot_routing_plan.plan.id
  note    = "Paid fallback allowed, budgets enforce the ceiling"
  document = jsonencode({
    model_mappings   = [{ match = "claude-*", alias_id = harborpilot_model_alias.coding.id }]
    default_alias_id = harborpilot_model_alias.coding.id
    fallback = {
      steps               = ["retry", "alternate_account", "stop"]
      max_attempts        = 3
      allow_paid_fallback = true
    }
    # If a provider's usage cannot be measured, budgets cannot be enforced:
    # refuse rather than spend blindly.
    compatibility = { unenforceable_budget = "block" }
  })
}

resource "harborpilot_client_binding" "project" {
  for_each = harborpilot_project.app
  name     = "${var.prefix}-${each.key}-agents"
  spec = jsonencode({
    display_name = "${title(each.key)} agents"
    plan_id      = harborpilot_routing_plan.plan.id
    project_id   = each.value.id
    enabled      = true
  })
  depends_on = [harborpilot_routing_plan_version.plan]
}

resource "harborpilot_budget" "project" {
  for_each = harborpilot_project.app
  name     = "${var.prefix}-${each.key}-monthly"
  spec = jsonencode({
    scope       = "project"
    scope_id    = each.value.id
    period      = "month"
    limit       = "200"
    currency    = "USD"
    enforcement = "block"
    warn_at     = 0.75
    enabled     = true
  })
}

resource "harborpilot_budget" "global" {
  name = "${var.prefix}-global-warning"
  spec = jsonencode({
    scope       = "global"
    period      = "month"
    limit       = "1000"
    currency    = "USD"
    enforcement = "warn"
    # Also count subscription usage priced at list rates, for comparison.
    cost_kinds = ["actual_api_charge", "estimated_api_charge", "list_equivalent"]
    enabled    = true
  })
}
