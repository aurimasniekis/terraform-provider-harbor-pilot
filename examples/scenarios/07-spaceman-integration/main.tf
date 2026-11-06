# SpaceMan integration using generic client and project primitives.
#
# SpaceMan is one client among others: it gets a client binding and a
# parent gateway token with the tokens:mint scope. For each run it mints a short-lived
# child token (POST /v1/harbor/credentials) with the run and space bound as
# trusted references, and hands only that token to the harness it launches.
# Headers the harness sends stay untrusted hints. Standalone clients keep
# their own bindings and are unaffected.

variable "prefix" {
  type    = string
  default = "spaceman"
}

variable "spaces" {
  description = "Spaces whose runs are attributed to their own project."
  type        = set(string)
  default     = ["payments", "platform"]
}

resource "harborpilot_workspace" "fleet" {
  name = "${var.prefix}-fleet"
  spec = jsonencode({ display_name = "Agent fleet" })
}

resource "harborpilot_project" "space" {
  for_each = var.spaces
  name     = "${var.prefix}-${each.key}"
  spec = jsonencode({
    workspace_id  = harborpilot_workspace.fleet.id
    display_name  = "Space ${each.key}"
    external_refs = { "spaceman.space" = each.key }
  })
}

data "harborpilot_routing_plan" "default" {
  name = harborpilot_routing_plan.fleet.name
}

resource "harborpilot_routing_plan" "fleet" {
  name = "${var.prefix}-plan"
  spec = jsonencode({ display_name = "Fleet runs" })
}

resource "harborpilot_provider" "api" {
  name = "${var.prefix}-anthropic"
  spec = jsonencode({
    type   = "anthropic"
    models = [{ id = "claude-sonnet-4-5", display_name = "Claude Sonnet 4.5", context_window = 200000, max_output_tokens = 64000, capabilities = ["tools", "streaming"] }]
  })
}

resource "harborpilot_provider_account" "api" {
  name = "${var.prefix}-anthropic-key"
  spec = jsonencode({
    provider_id    = harborpilot_provider.api.id
    billing        = "metered_api"
    credential_ref = "env:HP_SECRET_ANTHROPIC_API_KEY"
    enabled        = true
  })
}

resource "harborpilot_pool" "fleet" {
  name = "${var.prefix}-fleet"
  spec = jsonencode({ members = [{ account_id = harborpilot_provider_account.api.id, model = "claude-sonnet-4-5", priority = 1, weight = 1 }] })
}

resource "harborpilot_model_alias" "fleet" {
  name = "${var.prefix}/default"
  spec = jsonencode({ pool_id = harborpilot_pool.fleet.id })
}

resource "harborpilot_routing_plan_version" "fleet" {
  plan_id = harborpilot_routing_plan.fleet.id
  document = jsonencode({
    model_mappings   = [{ match = "*", alias_id = harborpilot_model_alias.fleet.id }]
    default_alias_id = harborpilot_model_alias.fleet.id
    fallback         = { steps = ["retry", "stop"], max_attempts = 2 }
  })
}

resource "harborpilot_client_binding" "spaceman" {
  name = "${var.prefix}-runner"
  spec = jsonencode({
    display_name = "SpaceMan runs"
    owner_kind   = "service"
    owner_id     = harborpilot_service_principal.spaceman.id
    plan_id      = harborpilot_routing_plan.fleet.id
    # Runs may select any space's project; the binding default covers runs
    # without one.
    project_id          = harborpilot_project.space["platform"].id
    allowed_project_ids = [for p in harborpilot_project.space : p.id]
    limits              = { max_concurrency = 16, requests_per_minute = 600 }
    trust               = { session_hints = true, project_hints = true }
    # Pin only the fleet. A spaceman.space here would bind every run to one
    # space; child tokens bind their own space instead.
    external_refs = { "spaceman.fleet" = "prod" }
    enabled       = true
  })
  depends_on = [harborpilot_routing_plan_version.fleet]
}

# A budget per space keeps one space from exhausting the fleet's spend.
resource "harborpilot_budget" "space" {
  for_each = harborpilot_project.space
  name     = "${var.prefix}-${each.key}-daily"
  spec = jsonencode({
    scope    = "project"
    scope_id = each.value.id
    period   = "day"
    limit    = "25"
    currency = "USD"
    enabled  = true
  })
}

# The orchestrator's control-plane identity (read-only), separate from the
# gateway token it uses to mint run tokens. `usage.read` covers
# GET /api/v1/budgets/status.
resource "harborpilot_service_principal" "spaceman" {
  name        = "${var.prefix}-orchestrator"
  description = "SpaceMan fleet orchestrator"
  permissions = ["config.read", "usage.read", "activity.read"]
}

# No Harbor Pilot token is kept in state: both tokens below are ephemeral and
# exist only while Terraform runs. SpaceMan's own provider receives them in
# write-only arguments, for example:
#
#   resource "spaceman_harbor_pilot" "fleet" {
#     parent_token_wo         = ephemeral.harborpilot_gateway_token.spaceman_parent.token
#     api_token_wo            = ephemeral.harborpilot_service_principal_token.spaceman.token
#     tokens_wo_version       = 1
#     project_ids             = { for k, p in harborpilot_project.space : k => p.id }
#   }
#
# Both use revoke_on_close = false so the tokens stay valid after Terraform
# finishes. Terraform opens ephemeral resources on every plan and apply, so
# each run issues new tokens; older ones expire after ttl_seconds. Bump the
# write-only version to hand SpaceMan fresh tokens before they expire.

# The parent token SpaceMan mints run tokens from. It pins only the fleet;
# each child binds its own space and one of the allowed projects.
ephemeral "harborpilot_gateway_token" "spaceman_parent" {
  binding_id      = harborpilot_client_binding.spaceman.id
  name            = "spaceman-parent"
  scopes          = ["gateway:invoke", "gateway:models", "tokens:mint"]
  bound_refs      = { "spaceman.fleet" = "prod" }
  ttl_seconds     = 30 * 24 * 3600
  revoke_on_close = false
}

# The orchestrator's API token (hpa_…) for budget status, config and activity.
ephemeral "harborpilot_service_principal_token" "spaceman" {
  service_principal_id = harborpilot_service_principal.spaceman.id
  name                 = "spaceman-orchestrator"
  ttl_seconds          = 30 * 24 * 3600
  revoke_on_close      = false
}

output "spaceman_binding_id" {
  value = harborpilot_client_binding.spaceman.id
}

output "spaceman_service_principal_id" {
  value = harborpilot_service_principal.spaceman.id
}

# One project per space, keyed by space.
output "project_ids" {
  value = { for k, p in harborpilot_project.space : k => p.id }
}

output "plan_id_from_lookup" {
  value = data.harborpilot_routing_plan.default.id
}
