# Several harnesses (Claude Code, Codex, OpenCode and Pi) share two Claude
# subscriptions and one ChatGPT subscription. Each harness is its own client
# binding, so activity, limits and tokens stay separate, while all of them
# draw on the same pools. The pool moves a conversation to the other
# subscription when one is exhausted.

variable "prefix" {
  type    = string
  default = "team"
}

variable "enable_quota_polling" {
  type    = bool
  default = true
}

resource "harborpilot_provider" "claude" {
  name = "${var.prefix}-claude-code"
  spec = jsonencode({
    type = "claude_code"
    models = [
      { id = "sonnet", display_name = "Sonnet", context_window = 200000, max_output_tokens = 64000, capabilities = ["tools", "streaming"] },
    ]
  })
}

resource "harborpilot_provider" "codex" {
  name = "${var.prefix}-codex"
  spec = jsonencode({
    type = "codex"
    models = [
      { id = "gpt-5-codex", display_name = "GPT-5 Codex", context_window = 272000, max_output_tokens = 128000, capabilities = ["tools", "streaming"] },
    ]
  })
}

locals {
  claude_accounts = {
    alice = "env:HP_SECRET_CLAUDE_TOKEN_ALICE"
    bob   = "env:HP_SECRET_CLAUDE_TOKEN_BOB"
  }
}

resource "harborpilot_provider_account" "claude" {
  for_each = local.claude_accounts
  name     = "${var.prefix}-claude-${each.key}"
  spec = jsonencode({
    provider_id     = harborpilot_provider.claude.id
    display_name    = "Claude Max (${each.key})"
    billing         = "subscription"
    credential_ref  = each.value
    max_concurrency = 3
    enabled         = true
  })
}

resource "harborpilot_provider_account" "chatgpt" {
  name = "${var.prefix}-chatgpt-pro"
  spec = jsonencode({
    provider_id  = harborpilot_provider.codex.id
    display_name = "ChatGPT Pro"
    billing      = "subscription"
    # Codex reads its login from a per-account home prepared by `codex login`.
    credential_ref = "file:/var/lib/harbor-pilot/secrets/codex-auth.json"
    enabled        = true
  })
}

# Monitors feed "five hours left" style windows into routing.
resource "harborpilot_quota_monitor" "claude" {
  for_each = local.claude_accounts
  name     = "${var.prefix}-claude-${each.key}-usage"
  spec = jsonencode({
    adapter                = "claude_subscription"
    account_id             = harborpilot_provider_account.claude[each.key].id
    options                = { token_source = "env" }
    access_token_ref       = each.value
    acknowledge_terms_risk = true
    exhausted_below        = 0.05
    enabled                = var.enable_quota_polling
  })
}

resource "harborpilot_pool" "claude" {
  name = "${var.prefix}-claude-shared"
  spec = jsonencode({
    display_name = "Shared Claude subscriptions"
    # Prefer the account whose window resets soonest; keep sessions sticky
    # for prompt caching.
    strategy = "reset_soonest"
    affinity = "session"
    members = [for k, a in harborpilot_provider_account.claude : {
      account_id = a.id, model = "sonnet", priority = 1, weight = 1
    }]
  })
}

resource "harborpilot_pool" "codex" {
  name = "${var.prefix}-codex"
  spec = jsonencode({
    members = [{ account_id = harborpilot_provider_account.chatgpt.id, model = "gpt-5-codex", priority = 1, weight = 1 }]
  })
}

resource "harborpilot_model_alias" "claude" {
  name = "${var.prefix}/claude"
  spec = jsonencode({ pool_id = harborpilot_pool.claude.id, required_capabilities = ["tools"] })
}

resource "harborpilot_model_alias" "codex" {
  name = "${var.prefix}/codex"
  spec = jsonencode({ pool_id = harborpilot_pool.codex.id })
}

resource "harborpilot_routing_plan" "shared" {
  name = "${var.prefix}-shared"
  spec = jsonencode({ display_name = "Shared subscriptions" })
}

resource "harborpilot_routing_plan_version" "shared" {
  plan_id = harborpilot_routing_plan.shared.id
  document = jsonencode({
    model_mappings = [
      { match = "claude-*", alias_id = harborpilot_model_alias.claude.id },
      { match = "gpt-*", alias_id = harborpilot_model_alias.codex.id },
    ]
    default_alias_id = harborpilot_model_alias.claude.id
    # Subscriptions only: never fall back to a paid API.
    restrictions = { allowed_billing = ["subscription"] }
    fallback     = { steps = ["alternate_account", "stop"], max_attempts = 3 }
  })
}

locals {
  harnesses = {
    "claude-code" = "claude_code"
    "codex"       = "codex"
    "opencode"    = "opencode"
    "pi"          = "pi"
  }
}

resource "harborpilot_client_binding" "harness" {
  for_each = local.harnesses
  name     = "${var.prefix}-${each.key}"
  spec = jsonencode({
    display_name     = each.key
    plan_id          = harborpilot_routing_plan.shared.id
    expected_harness = each.value
    limits           = { max_concurrency = 4 }
    trust            = { session_hints = true }
    enabled          = true
  })
  depends_on = [harborpilot_routing_plan_version.shared]
}

output "client_binding_ids" {
  value = { for k, b in harborpilot_client_binding.harness : k => b.id }
}
