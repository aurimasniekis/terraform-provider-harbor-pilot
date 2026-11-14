# Design decisions

Decisions are numbered `TD-<n>`, so they can be referenced from code, tests and the changelog.

## TD-1 Module, license and dependencies
- **Module:** `github.com/aurimasniekis/terraform-provider-harbor-pilot`, built with Go 1.27 and licensed Apache-2.0.
- **Dependencies:** the Terraform Plugin Framework, plus `terraform-plugin-framework-jsontypes`, `terraform-plugin-framework-validators`, `terraform-plugin-log` and `terraform-plugin-testing`.
- **Documentation:** generated with `tfplugindocs`.
- **Boundary:** the provider talks only to the server's public control-plane API. It never imports application packages and never touches the database.

## TD-2 A thin hand-written HTTP client
`internal/client` is about 400 lines over `net/http`. A generated client was considered. It was rejected because it would add tens of thousands of lines and hide the behaviors that matter.

The client's behavior is explicit:
- Every request sends `X-HP-Managed-By: terraform`.
- Writes send `If-Match: "r<revision>"`.
- Requests use a bounded timeout: 30s by default, at most 5m.
- Only idempotent `GET` requests are retried, on 429 and 503, with jittered backoff.
- Redirects are never followed, so the token cannot leak to another origin.
- Error envelopes are decoded into the error code, message, request id and field locations. The `value` the server echoes for invalid fields is deliberately not decoded, because it can contain submitted secrets.
- Request and response bodies are never logged.

`api/openapi.yaml` is a vendored copy of the server's spec. `tools/genschema` derives `internal/specschema/schemas.json` from it.

## TD-3 Provider configuration
- **Arguments:** `url`, `token`, `token_file`, `ca_cert_file`, `insecure_skip_verify` and `timeout`.
- **Environment variables:** `HP_SERVER`, `HP_API_TOKEN` and `HP_API_TOKEN_FILE`. These are the same names the `harbor-pilot` CLI uses.
- **Token precedence:** a token in configuration wins over a token file. When neither is set, the provider falls back to the environment.
- **Deferred configuration:** when values are unknown at plan time, for example because they come from resources not yet created, configuration is deferred.

## TD-4 One generic resource for every control-plane kind
These kinds each get a resource and a data source:
- workspace, project
- provider, provider account, subscription entitlement
- pool, model alias, routing plan, client binding
- quota monitor, budget
- privacy policy, trace policy
- event destination, notification channel, notification rule

One implementation is instantiated per kind (`internal/provider/kinds.go`). Each resource has a `name` and a `spec`, which is a JSON document.

Typed HCL schemas were considered and rejected:
- They would duplicate about 1,500 lines of nested server types.
- They would drift with every server release.
- The server already validates specs precisely and reports field paths, which the provider turns into attribute diagnostics.

Some older server schemas mark every property as required, including optional ones (see the app report). To keep configurations short, the provider fills properties the user left out with their zero value, using the vendored schema. Three cases are deliberately not filled:
- `enabled` must always be explicit, because silently disabling an account would be dangerous.
- Enums without an empty value are left for the server to report.
- Numbers with a positive minimum are left for the server to report.

One consequence: an omitted list is sent empty, not "default". The documentation calls this out for `fallback.steps`.

**Amendment (2026-11-08):** from the Harbor Pilot 0.1.4 re-vendor, the vendored schema marks only truly required properties as required. Omitted optional properties are left out of the request and the server's documented defaults apply. For example, an omitted `fallback.steps` in a routing plan version document now means the server's default fallback (`alternate_account`, `alternate_model`, `stop`), and `steps = []` means no fallback. The owner chose to follow the API rather than keep the old zero-fill for this field; the CHANGELOG lists it as a behaviour change.

## TD-5 Convergence and drift
The server canonicalizes specs: it adds defaults, sorts lists and changes case. The provider handles this as follows:
1. After each create or update, the user's `spec` is kept in state exactly as written.
2. The server's canonical form is kept in resource private state.
3. On read, if the server's canonical spec equals the stored one, the user's spec stays in state and the plan is empty.
4. If they differ, state receives the server's spec, so the next plan shows exactly what changed outside Terraform.

The server's `drift_state` is exposed as a computed attribute. After an import, the canonical spec appears in state; the first apply aligns it with the configuration.

## TD-6 Secrets never default into state
- **References first.** Spec fields such as `credential_ref` and `api_key_ref` carry `env:` or `file:` references that the server resolves.
- **Stored secrets use write-only arguments.** These are `secrets_wo` and `client_secret_wo`, and they require Terraform 1.11. The value is sent on create and whenever `*_wo_version` changes. It never appears in plan or state.
- **Issued tokens are opt-in.** `harborpilot_gateway_credential` and `harborpilot_api_token` keep the token in state only with `store_token_in_state = true`.
- **Ephemeral tokens.** `harborpilot_gateway_token` and `harborpilot_service_principal_token` require Terraform 1.10. They issue a token per run and revoke it when Terraform closes it.
- **Kept ephemeral tokens.** With `revoke_on_close = false` the token outlives the run until `ttl_seconds` (at most two years, the server's limit). This hands a long-lived token, such as a parent gateway token with `tokens:mint`, to another provider's write-only argument without putting it in state. Terraform opens ephemeral resources on every plan and apply, so each run issues a new token and earlier ones stay valid until they expire; the TTL bounds how many are live.
- **Data sources** expose only secret names.

## TD-7 Routing plan versions
Published versions are immutable, so `harborpilot_routing_plan_version` behaves as follows:
- Changing `document` or `note` creates and publishes a new version.
- With `publish = false`, the version stays a draft.
- Switching from published to draft replaces the resource with a new draft.
- If another version is published outside Terraform, the refresh reports `publish = false`. Applying then publishes a copy of this version through the rollback API.
- Destroying a draft discards it. Destroying a published version leaves it in the plan's history.
- If publishing fails after the draft was created, the draft is discarded so it is not orphaned.

## TD-8 Credentials and tokens
For gateway credentials and API tokens:
- Every argument forces replacement: a new credential is issued and the old one is revoked.
- If a credential or token is revoked or expires outside Terraform, it is removed from state, and the next apply issues a new one.

## TD-9 Deletes wait for concurrent dereferencing
Terraform deletes independent resources concurrently. For example, it may delete a model alias that a published plan still references while deleting that plan in the same run.

To handle this, deletes retry for up to two minutes on two status codes:
- 409, when the object is referenced;
- 422, when the delete would invalidate a published plan.

After the timeout, the server's error lists the referencing objects.

## TD-10 Authentication settings are a singleton
`harborpilot_auth_settings` has the fixed id `auth_settings`.
- Create adopts the current settings and writes them.
- Destroy only forgets the resource, because the settings cannot be deleted.

## TD-11 Schema versions and state upgrades
All schemas are at version 0. An incompatible schema change must do three things:
1. Bump the version.
2. Add a `ResourceWithUpgradeState` upgrader from every earlier version, with a unit test that upgrades a recorded prior state.
3. Note the change in the changelog.

No upgraders exist yet. None are invented for versions that never shipped.

## TD-12 Acceptance tests use a real server
`internal/acctest` prepares the server:
1. It builds `harbor-pilot` from `../go-harbor-pilot`. Override this with `HP_APP_DIR` or `HP_APP_BIN`.
2. It starts `serve` on a free loopback port, with a temporary data directory and a master key file with mode 0600.
3. It creates the first administrator through the one-time setup flow.
4. It issues an API token for a service principal that holds every configuration permission.

For every generic kind, the tests cover:
- create and update;
- a second plan that is empty (convergence);
- import by id and by name;
- drift after an out-of-band change;
- recreation after an out-of-band delete;
- republishing a superseded plan version;
- destroy.

Further tests cover:
- gateway credentials: token handling, import, and recreation after revocation;
- write-only secret rotation;
- the ephemeral tokens, checked to be revoked after the run, or kept with `revoke_on_close = false`;
- that no scenario keeps a Harbor Pilot token in state (scenario 07 also checks its kept parent and API tokens);
- service principals and API tokens;
- authentication settings and SSO providers;
- a provider model's upstream request fields (Harbor Pilot D-046): round-trip with exact numbers, update, import, drift, clearing when omitted, and the server's 422.
- a provider model's reasoning option (Harbor Pilot D-047): round-trip, the server's normalised form without a diff, update, import, drift, clearing when omitted (no `reasoning` key stored), and the server's 422.

412 conflicts are tested directly against the server with a stale revision, because Terraform refreshes before every apply and cannot produce one on its own.

Scenario L runs each example in `examples/scenarios` exactly as written. It uses a locally built provider through `dev_overrides` and checks that a second plan is empty.

## TD-13 No Registry publication claim
Releases are built by goreleaser, with GPG-signed checksums and a `terraform-registry-manifest.json`. Nothing is published to the Terraform Registry until the owner signs in and publishes it, so the documentation makes no publication claim.
