# Privacy and 3-day traces: secrets and personal data are masked before a
# request leaves the gateway, masking is required (a request that cannot be
# masked is refused rather than sent raw), and masked request traces are
# kept for three days for debugging, then cryptographically erased.

variable "prefix" {
  type    = string
  default = "private"
}

resource "harborpilot_privacy_policy" "strict" {
  name = "${var.prefix}-strict"
  spec = jsonencode({
    description = "Mask credentials, keys and customer identifiers"
    required    = true
    rules = [
      { name = "customer-id", pattern = "CUST-[0-9]{6,}", action = "mask" },
      { name = "internal-host", pattern = "[a-z0-9-]+\\.corp\\.example\\.com", action = "mask" },
      { name = "private-key", pattern = "-----BEGIN [A-Z ]*PRIVATE KEY-----", action = "block" },
    ]
    allow = [
      { name = "docs-host", pattern = "docs\\.corp\\.example\\.com" },
    ]
    # Configured secret values to mask exactly, one per line, read by the
    # server from a file it controls.
    secret_values_ref = "file:/etc/harbor-pilot/secrets/mask-values.txt"
  })
}

resource "harborpilot_trace_policy" "three_days" {
  name = "${var.prefix}-3-day-traces"
  spec = jsonencode({
    mode                = "masked_full"
    retention           = "3d"
    redaction_policy_id = harborpilot_privacy_policy.strict.id
    sample_rate         = 1
    allow_export        = false
  })
}

resource "harborpilot_provider" "api" {
  name = "${var.prefix}-openai"
  spec = jsonencode({
    type   = "openai"
    models = [{ id = "gpt-5", display_name = "GPT-5", context_window = 400000, max_output_tokens = 128000, capabilities = ["tools", "streaming"] }]
  })
}

resource "harborpilot_provider_account" "api" {
  name = "${var.prefix}-openai-key"
  spec = jsonencode({
    provider_id    = harborpilot_provider.api.id
    billing        = "metered_api"
    credential_ref = "env:OPENAI_API_KEY"
    enabled        = true
  })
}

resource "harborpilot_pool" "api" {
  name = "${var.prefix}-openai"
  spec = jsonencode({ members = [{ account_id = harborpilot_provider_account.api.id, model = "gpt-5", priority = 1, weight = 1 }] })
}

resource "harborpilot_model_alias" "default" {
  name = "${var.prefix}/default"
  spec = jsonencode({ pool_id = harborpilot_pool.api.id })
}

resource "harborpilot_routing_plan" "plan" {
  name = "${var.prefix}-plan"
  spec = jsonencode({ display_name = "Masked, 3-day traces" })
}

resource "harborpilot_routing_plan_version" "plan" {
  plan_id = harborpilot_routing_plan.plan.id
  document = jsonencode({
    default_alias_id = harborpilot_model_alias.default.id
    fallback         = { steps = ["retry", "stop"], max_attempts = 2 }
    privacy = {
      policy_id       = harborpilot_privacy_policy.strict.id
      trace_policy_id = harborpilot_trace_policy.three_days.id
    }
  })
}

resource "harborpilot_client_binding" "agents" {
  name = "${var.prefix}-agents"
  spec = jsonencode({
    plan_id = harborpilot_routing_plan.plan.id
    enabled = true
  })
  depends_on = [harborpilot_routing_plan_version.plan]
}
