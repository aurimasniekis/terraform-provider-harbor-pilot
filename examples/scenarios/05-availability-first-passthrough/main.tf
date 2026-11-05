# Availability first: when a provider changes its protocol and translation
# starts failing, the compatibility breaker opens and requests are passed
# through natively instead of failing. The plan lists exactly which features
# may be lost while degraded (passthrough always loses detailed usage and
# tool statistics). Privacy masking is not required here; with a required
# privacy policy, passthrough would be refused.

variable "prefix" {
  type    = string
  default = "available"
}

resource "harborpilot_provider" "anthropic" {
  name = "${var.prefix}-anthropic"
  spec = jsonencode({
    type   = "anthropic"
    models = [{ id = "claude-sonnet-4-5", display_name = "Claude Sonnet 4.5", context_window = 200000, max_output_tokens = 64000, capabilities = ["tools", "streaming"] }]
  })
}

resource "harborpilot_provider" "openrouter" {
  name = "${var.prefix}-openrouter"
  spec = jsonencode({
    type     = "openai_compatible"
    base_url = "https://openrouter.ai/api/v1"
    models   = [{ id = "anthropic/claude-sonnet-4.5", display_name = "Claude Sonnet 4.5 (OpenRouter)", context_window = 200000, max_output_tokens = 64000, capabilities = ["tools", "streaming"] }]
  })
}

resource "harborpilot_provider_account" "anthropic" {
  name = "${var.prefix}-anthropic-key"
  spec = jsonencode({
    provider_id    = harborpilot_provider.anthropic.id
    billing        = "metered_api"
    credential_ref = "env:HP_SECRET_ANTHROPIC_API_KEY"
    enabled        = true
  })
}

resource "harborpilot_provider_account" "openrouter" {
  name = "${var.prefix}-openrouter-key"
  spec = jsonencode({
    provider_id    = harborpilot_provider.openrouter.id
    billing        = "metered_api"
    credential_ref = "env:HP_SECRET_OPENROUTER_API_KEY"
    enabled        = true
  })
}

resource "harborpilot_pool" "sonnet" {
  name = "${var.prefix}-sonnet"
  spec = jsonencode({
    strategy = "ordered"
    members = [
      { account_id = harborpilot_provider_account.anthropic.id, model = "claude-sonnet-4-5", priority = 1, weight = 1 },
      { account_id = harborpilot_provider_account.openrouter.id, model = "anthropic/claude-sonnet-4.5", priority = 2, weight = 1 },
    ]
  })
}

resource "harborpilot_model_alias" "sonnet" {
  name = "${var.prefix}/sonnet"
  spec = jsonencode({ pool_id = harborpilot_pool.sonnet.id })
}

resource "harborpilot_routing_plan" "plan" {
  name = "${var.prefix}-plan"
  spec = jsonencode({ display_name = "Availability first" })
}

resource "harborpilot_routing_plan_version" "plan" {
  plan_id = harborpilot_routing_plan.plan.id
  note    = "Prefer degraded service over failure"
  document = jsonencode({
    model_mappings   = [{ match = "claude-*", alias_id = harborpilot_model_alias.sonnet.id }]
    default_alias_id = harborpilot_model_alias.sonnet.id
    fallback = {
      steps               = ["retry", "alternate_provider", "wait", "stop"]
      max_attempts        = 4
      allow_paid_fallback = true
      wait_max_seconds    = 20
    }
    compatibility = {
      on_breaker_open = "passthrough"
      allowed_losses  = ["detailed_usage", "tool_statistics", "intent_inspection"]
      unknown_usage   = "estimate"
    }
  })
}

resource "harborpilot_client_binding" "ci" {
  name = "${var.prefix}-ci"
  spec = jsonencode({
    display_name = "CI agents"
    plan_id      = harborpilot_routing_plan.plan.id
    enabled      = true
  })
  depends_on = [harborpilot_routing_plan_version.plan]
}
