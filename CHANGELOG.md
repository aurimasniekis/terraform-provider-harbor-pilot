# Changelog

## Unreleased

- New ephemeral resource `harborpilot_service_principal_token`: issues a service principal API token (`hpa_…`) during a run without storing it in state. The token carries the principal's permissions (for example `usage.read` for `GET /api/v1/budgets/status`).
- `harborpilot_gateway_token`: new `revoke_on_close` argument (default `true`). With `false` the token stays valid until `ttl_seconds`, so it can serve as a parent token with `tokens:mint`. `ttl_seconds` now accepts up to 63072000 (two years) instead of 86400.
- Scenario 07 (SpaceMan integration) keeps no Harbor Pilot token in state: the parent gateway token and the orchestrator's API token come from ephemeral resources. The parent pins only `spaceman.fleet`, and the binding allows every space's project. New outputs `project_ids` (one per space), `spaceman_binding_id` and `spaceman_service_principal_id`; the `spaceman_parent_token` and `spaceman_gateway_base_urls` outputs are removed.
- The scenario acceptance test fails if any scenario's state contains a Harbor Pilot token.

## 0.1.0 (2026-11-06)

The first release, for Harbor Pilot 0.1.

- Initial provider: generic resources and data sources for every control-plane kind; routing plan versions; gateway credentials and an ephemeral gateway token; service principals and API tokens; authentication settings and SSO providers; quota adapter and event catalog data sources.
