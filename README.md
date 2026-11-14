<p align="center">
  <img src="https://raw.githubusercontent.com/aurimasniekis/go-harbor-pilot/main/site/public/logo.svg" alt="" width="64" height="64">
</p>

<h1 align="center">Terraform provider for Harbor Pilot</h1>

<p align="center">
  Manage <a href="https://github.com/aurimasniekis/go-harbor-pilot">Harbor Pilot</a>, the self-hosted LLM gateway and
  policy control plane for coding agents, as code: providers and accounts, routing plans, clients, limits, privacy
  and alerting.
</p>

<p align="center">
  <a href="https://registry.terraform.io/providers/aurimasniekis/harbor-pilot/latest">Terraform Registry</a> ·
  <a href="https://aurimasniekis.github.io/go-harbor-pilot/operators/terraform">Guide</a> ·
  <a href="docs/index.md">Reference</a> ·
  <a href="examples/scenarios">Examples</a>
</p>

> **0.1.4 is the latest release**: use it with Harbor Pilot 0.1.7 or later (the per-model reasoning option needs
> 0.1.7). See the [changelog](CHANGELOG.md). Built on the HashiCorp [terraform-plugin-framework](https://developer.hashicorp.com/terraform/plugin/framework).

## Quick start

```hcl
terraform {
  required_providers {
    harborpilot = {
      source  = "aurimasniekis/harbor-pilot"
      version = "~> 0.1"
    }
  }
}

# Reads HP_SERVER and HP_API_TOKEN_FILE (or HP_API_TOKEN).
provider "harborpilot" {}

resource "harborpilot_provider" "anthropic" {
  name = "anthropic"
  spec = jsonencode({ type = "anthropic", display_name = "Anthropic" })
}

resource "harborpilot_provider_account" "team" {
  name = "anthropic-team"
  spec = jsonencode({
    provider_id    = harborpilot_provider.anthropic.id
    billing        = "metered_api"
    credential_ref = "env:HP_SECRET_ANTHROPIC_KEY" # resolved on the server, never stored in state
    enabled        = true
  })
}
```

Create the token under **Settings → API tokens** in Harbor Pilot, with the permissions for the resources you manage.

## What it manages

Each resource is one Harbor Pilot object. The middle column is the name the web UI uses.

| Resource | In the UI | API |
|---|---|---|
| `harborpilot_workspace`, `harborpilot_project` | Workspaces and projects | `/workspaces`, `/projects` |
| `harborpilot_provider` | Providers | `/providers` |
| `harborpilot_provider_account` | Accounts (write-only `secrets_wo` for keys) | `/provider-accounts` |
| `harborpilot_subscription_entitlement` | Subscriptions | `/subscription-entitlements` |
| `harborpilot_pool` | Fallback groups | `/pools` |
| `harborpilot_model_alias` | Model names | `/model-aliases` |
| `harborpilot_routing_plan` | Routing plans | `/routing-plans` |
| `harborpilot_routing_plan_version` | A plan's published version (draft, validate, publish) | `/routing-plans/{id}/versions` |
| `harborpilot_client_binding` | Clients | `/client-bindings` |
| `harborpilot_gateway_credential` | A client's access tokens (the token stays out of state unless `store_token_in_state`) | `/client-bindings/{id}/credentials` |
| `harborpilot_quota_monitor` | Quota tracking | `/quota-monitors` |
| `harborpilot_budget` | Budgets | `/budgets` |
| `harborpilot_privacy_policy` | Masking policies | `/privacy-policies` |
| `harborpilot_trace_policy` | Content capture rules | `/trace-policies` |
| `harborpilot_event_destination` | Event relays (Pulse, signed webhooks) | `/event-destinations` |
| `harborpilot_notification_channel`, `harborpilot_notification_rule` | Alert channels and rules | `/notification-channels`, `/notification-rules` |
| `harborpilot_service_principal`, `harborpilot_api_token` | API tokens (automation accounts) | `/service-principals` |
| `harborpilot_auth_settings`, `harborpilot_oidc_provider` | Sign-in settings and SSO providers (write-only client secret) | `/auth/settings`, `/auth/oidc-providers` |

Ephemeral resources (Terraform 1.10 or later) issue tokens while Terraform runs without storing them in state:

| Ephemeral resource | Issues | API |
|---|---|---|
| `harborpilot_gateway_token` | An access token for a client, including a parent token with `tokens:mint` | `/client-bindings/{id}/credentials` |
| `harborpilot_service_principal_token` | An API token (`hpa_…`) carrying a service principal's permissions | `/service-principals/{id}/tokens` |

Both revoke the token when the run ends. With `revoke_on_close = false` the token stays valid until `ttl_seconds`
passes, so another tool (for example an orchestrator's own provider, through a write-only argument) can keep it.

Data sources: one per object kind above, looked up by name or id, plus `harborpilot_quota_adapters` and
`harborpilot_event_catalog`.

Never managed: requests, usage, captured content, alert history and other runtime state. Secrets are never read back.

## Behaviour in brief

- **Specs are JSON.** Each object's settings go in `spec` with `jsonencode`, as the API takes them. The server fills
  defaults and normalises the spec; the provider remembers the server's version, so a second `terraform plan` stays
  empty and only a change made outside Terraform shows up as drift.
- **Secrets stay out of state.** Use secret references (`env:HP_SECRET_…` or `file:…`, resolved on the server) or the
  write-only `secrets_wo` map, rotated by bumping `secrets_wo_version` (Terraform 1.11 or later).
- **No silent overwrites.** Updates send `If-Match` with the revision Terraform last saw; a change made in the UI in
  between is reported instead of overwritten.
- **Imports** by id or by name for every resource.

[`docs/DECISIONS.md`](docs/DECISIONS.md) explains the design.

## Examples

[`examples/scenarios`](examples/scenarios) has seven complete configurations, each applied to a real server by the
acceptance tests:

1. Personal Claude Code setup.
2. Several tools sharing subscription pools.
3. Project budgets with API fallback.
4. Privacy with 3-day captured content.
5. Availability-first passthrough.
6. Pulse events and alert routing.
7. SpaceMan integration with run-scoped tokens and no Harbor Pilot token in state.

## Requirements

- Terraform 1.0 or later; write-only secrets need 1.11 and the ephemeral tokens need 1.10.
- A Harbor Pilot server and an API token.
- Go 1.27 to build.

## Development

```sh
make build       # provider binary in ./bin
make test        # unit tests
make testacc     # acceptance tests against a real harbor-pilot server
make docs        # regenerate docs/ with tfplugindocs
make lint
```

The acceptance tests build and start Harbor Pilot from a checkout of the application:

- `HP_APP_DIR`: path to the application checkout (default `../go-harbor-pilot`).
- `HP_APP_BIN`: a prebuilt server binary instead.
- `TF_ACC_TERRAFORM_PATH`: a specific Terraform binary; otherwise the latest 1.x is downloaded.

To use a local build instead of the Registry release:

```hcl
# ~/.terraformrc
provider_installation {
  dev_overrides {
    "aurimasniekis/harbor-pilot" = "/path/to/terraform-provider-harbor-pilot/bin"
  }
  direct {}
}
```

## License

Apache-2.0. See [LICENSE](LICENSE).
