# Changelog

## 0.1.3 (2026-11-13)

For Harbor Pilot 0.1.6 or later. Per-model request fields need 0.1.6; everything else also works with older Harbor Pilot 0.1 servers.

**Exact numbers:** numbers in `spec` and plan version documents are now sent and compared with their exact text. Before, they went through a float64, so an integer above 2^53 (for example a `request_fields` `seed`) lost digits.

- Supports per-model upstream request fields (requires Harbor Pilot ≥ 0.1.6, D-046). A provider's `spec.models[]` entries accept `provider_routing`, `request_fields` and `request_field_policy`. They are written in `spec` like any other property, round-trip with an empty second plan, and come back on import. **An omitted field is cleared on apply**, as the API's `PUT` replaces the whole spec. That includes values set only in the UI. The server validates the per-provider-type allowlist, and its 422 is shown with the field path. See the `harborpilot_provider` resource page.
- Re-vendored the OpenAPI document from Harbor Pilot 0.1.6.

## 0.1.2 (2026-11-08)

For Harbor Pilot 0.1.4 or later. One-time budgets need 0.1.4; everything else also works with older Harbor Pilot 0.1 servers.

**Behaviour change:** in a `harborpilot_routing_plan_version` document, an omitted `fallback.steps` now gets the server's default fallback (`alternate_account`, `alternate_model`, `stop`) instead of no fallback. For no fallback, write `fallback = { steps = [] }`. Only new plan versions are affected: published versions are immutable, and the next change to `document` publishes a new one. (Unchanged from 0.1.1: an omitted or `0` `fallback.max_attempts` becomes 3.)

- Supports one-time budgets (requires Harbor Pilot ≥ 0.1.4): `harborpilot_budget` accepts `period = "once"` with optional `starts_at` and `ends_at`. An older server rejects `period = "once"` with a 422. See the resource page for how replacement, **Start over** and `timezone` behave.
- Re-vendored the OpenAPI document from Harbor Pilot 0.1.4; optional fields are no longer sent as zero values. The earlier copy listed many spec properties as required (for example a workspace's `description` or a pool member's `weight`), so the provider filled them with zero values; it now leaves them out and the server applies its documented default. For most properties that default is the zero value, so those configurations plan no changes.

## 0.1.1 (2026-11-07)

For Harbor Pilot 0.1.1 or later.

**Breaking for scenario 07 users:** the scenario's `spaceman_parent_token` and `spaceman_gateway_base_urls` outputs are removed. Pass the parent token to the orchestrator's write-only argument from `ephemeral.harborpilot_gateway_token.spaceman_parent.token` instead (the API token likewise from `ephemeral.harborpilot_service_principal_token.spaceman.token`), and use the new `project_ids` output for the space projects.

**Trade-off of `revoke_on_close = false`:** Terraform opens an ephemeral resource on every plan and apply, so each run issues a new token, and with `revoke_on_close = false` the earlier tokens stay valid until `ttl_seconds` passes. Pick a TTL that matches how often you rotate.

- New ephemeral resource `harborpilot_service_principal_token`: issues a service principal API token (`hpa_…`) during a run without storing it in state. The token carries the principal's permissions (for example `usage.read` for `GET /api/v1/budgets/status`).
- `harborpilot_gateway_token`: new `revoke_on_close` argument (default `true`). With `false` the token stays valid until `ttl_seconds`, so it can serve as a parent token with `tokens:mint`. `ttl_seconds` now accepts up to 63072000 (two years) instead of 86400.
- Scenario 07 (SpaceMan integration) keeps no Harbor Pilot token in state: the parent gateway token and the orchestrator's API token come from ephemeral resources. The parent pins only `spaceman.fleet`, and the binding allows every space's project. New outputs `project_ids` (one per space), `spaceman_binding_id` and `spaceman_service_principal_id`; the `spaceman_parent_token` and `spaceman_gateway_base_urls` outputs are removed.
- The scenario acceptance test fails if any scenario's state contains a Harbor Pilot token.

## 0.1.0 (2026-11-06)

The first release, for Harbor Pilot 0.1.

- Initial provider: generic resources and data sources for every control-plane kind; routing plan versions; gateway credentials and an ephemeral gateway token; service principals and API tokens; authentication settings and SSO providers; quota adapter and event catalog data sources.
