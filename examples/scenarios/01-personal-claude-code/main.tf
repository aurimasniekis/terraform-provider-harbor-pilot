# Personal Claude Code setup: one Claude subscription, run through the
# vendor's own `claude` binary (the Claude Code bridge), exposed to Claude
# Code on your laptop under the model names it asks for.
#
# After apply, configure Claude Code with:
#   harbor-pilot token issue --binding <client_binding_id> > ~/.config/harbor-pilot/token
#   harbor-pilot harness setup claude --url <gateway_url> --token-file ~/.config/harbor-pilot/token

variable "prefix" {
  type    = string
  default = "personal"
}

variable "enable_quota_polling" {
  description = "Poll the subscription's usage windows (needs the token in the server environment)."
  type        = bool
  default     = true
}

resource "harborpilot_provider" "claude" {
  name = "${var.prefix}-claude-code"
  spec = jsonencode({
    type         = "claude_code"
    display_name = "Claude Code (subscription)"
    models = [
      { id = "sonnet", display_name = "Sonnet", context_window = 200000, max_output_tokens = 64000, capabilities = ["tools", "streaming"] },
      { id = "opus", display_name = "Opus", context_window = 200000, max_output_tokens = 32000, capabilities = ["tools", "streaming"] },
    ]
  })
}

resource "harborpilot_provider_account" "me" {
  name = "${var.prefix}-claude-max"
  spec = jsonencode({
    provider_id  = harborpilot_provider.claude.id
    display_name = "My Claude Max"
    billing      = "subscription"
    # A long-lived token from `claude setup-token`, read by the server from
    # its own environment. It never passes through Terraform.
    credential_ref  = "env:HP_SECRET_CLAUDE_CODE_OAUTH_TOKEN"
    max_concurrency = 2
    enabled         = true
  })
}

resource "harborpilot_subscription_entitlement" "max" {
  name = "${var.prefix}-claude-max-plan"
  spec = jsonencode({
    account_id = harborpilot_provider_account.me.id
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

# Reads the subscription's usage windows so routing knows when it is exhausted.
resource "harborpilot_quota_monitor" "max" {
  name = "${var.prefix}-claude-max-usage"
  spec = jsonencode({
    adapter                = "claude_subscription"
    account_id             = harborpilot_provider_account.me.id
    options                = { token_source = "env" }
    access_token_ref       = "env:HP_SECRET_CLAUDE_CODE_OAUTH_TOKEN"
    acknowledge_terms_risk = true
    enabled                = var.enable_quota_polling
  })
}

resource "harborpilot_pool" "claude" {
  name = "${var.prefix}-claude"
  spec = jsonencode({
    strategy = "ordered"
    members = [
      { account_id = harborpilot_provider_account.me.id, model = "sonnet", priority = 1, weight = 1 },
    ]
  })
}

resource "harborpilot_pool" "claude_opus" {
  name = "${var.prefix}-claude-opus"
  spec = jsonencode({
    members = [{ account_id = harborpilot_provider_account.me.id, model = "opus", priority = 1, weight = 1 }]
  })
}

resource "harborpilot_model_alias" "default" {
  name = "${var.prefix}/default"
  spec = jsonencode({ pool_id = harborpilot_pool.claude.id })
}

resource "harborpilot_model_alias" "opus" {
  name = "${var.prefix}/opus"
  spec = jsonencode({ pool_id = harborpilot_pool.claude_opus.id })
}

resource "harborpilot_routing_plan" "personal" {
  name = "${var.prefix}-plan"
  spec = jsonencode({ display_name = "Personal" })
}

resource "harborpilot_routing_plan_version" "personal" {
  plan_id = harborpilot_routing_plan.personal.id
  note    = "Claude Code through my subscription"
  document = jsonencode({
    model_mappings = [
      { match = "claude-opus*", alias_id = harborpilot_model_alias.opus.id },
      { match = "claude-*", alias_id = harborpilot_model_alias.default.id },
    ]
    default_alias_id = harborpilot_model_alias.default.id
    fallback         = { steps = ["retry", "stop"], max_attempts = 2 }
  })
}

resource "harborpilot_client_binding" "laptop" {
  name = "${var.prefix}-laptop"
  spec = jsonencode({
    display_name     = "Claude Code on my laptop"
    plan_id          = harborpilot_routing_plan.personal.id
    expected_harness = "claude_code"
    trust            = { session_hints = true }
    enabled          = true
  })
  depends_on = [harborpilot_routing_plan_version.personal]
}

output "client_binding_id" {
  value = harborpilot_client_binding.laptop.id
}
