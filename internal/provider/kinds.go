package provider

// Kind describes one generic control-plane object type.
type Kind struct {
	// TypeName is the resource name without the provider prefix.
	TypeName string
	// Plural is the API collection path segment.
	Plural string
	// Schema is the spec schema name in the OpenAPI document.
	Schema string
	Title  string
	// Secrets are the write-only secret names the kind accepts.
	Secrets []string
	// About is a short description for documentation.
	About string
}

// Kinds lists every generic managed kind.
var Kinds = []Kind{
	{TypeName: "workspace", Plural: "workspaces", Schema: "WorkspaceSpec", Title: "workspace",
		About: "A workspace groups projects for attribution and budgets."},
	{TypeName: "project", Plural: "projects", Schema: "ProjectSpec", Title: "project",
		About: "A project inside a workspace. Client bindings and tokens attribute requests to projects."},
	{TypeName: "provider", Plural: "providers", Schema: "ProviderSpec", Title: "provider",
		About: "An upstream model provider (API, OpenAI-compatible, local, or a CLI bridge) and its models."},
	{TypeName: "provider_account", Plural: "provider-accounts", Schema: "AccountSpec", Title: "provider account",
		Secrets: []string{"credential"},
		About:   "A credential for a provider. Prefer `credential_ref` (env:NAME or file:/path) in the spec; otherwise use the write-only `credential` secret."},
	{TypeName: "subscription_entitlement", Plural: "subscription-entitlements", Schema: "EntitlementSpec", Title: "subscription entitlement",
		About: "Subscription terms of an account: fee, period and known usage windows."},
	{TypeName: "pool", Plural: "pools", Schema: "PoolSpec", Title: "pool",
		About: "An ordered or weighted set of account and model members."},
	{TypeName: "model_alias", Plural: "model-aliases", Schema: "AliasSpec", Title: "model alias",
		About: "A model name clients request, resolved to a pool or another alias."},
	{TypeName: "routing_plan", Plural: "routing-plans", Schema: "PlanSpec", Title: "routing plan",
		About: "A routing plan. Its routing logic lives in versions; see `harborpilot_routing_plan_version`."},
	{TypeName: "client_binding", Plural: "client-bindings", Schema: "BindingSpec", Title: "client binding",
		About: "A client: who calls the gateway, under which plan and project, with which limits and trust."},
	{TypeName: "quota_monitor", Plural: "quota-monitors", Schema: "QuotaMonitorSpec", Title: "quota monitor",
		Secrets: []string{"api_key", "management_key", "access_token", "service_account"},
		About:   "Polls a provider's quota or balance. Prefer the `*_ref` spec fields over write-only secrets."},
	{TypeName: "budget", Plural: "budgets", Schema: "BudgetSpec", Title: "budget",
		About: "A spending limit for a scope and calendar period, enforced in the reservation path."},
	{TypeName: "privacy_policy", Plural: "privacy-policies", Schema: "PrivacyPolicySpec", Title: "privacy policy",
		Secrets: []string{"secret_values"},
		About:   "Detectors and masking rules applied before requests leave the gateway."},
	{TypeName: "trace_policy", Plural: "trace-policies", Schema: "TracePolicySpec", Title: "trace policy",
		About: "Whether and for how long encrypted request traces are kept."},
	{TypeName: "event_destination", Plural: "event-destinations", Schema: "DestinationSpec", Title: "event destination",
		Secrets: []string{"signing_secret", "previous_signing_secret", "creds"},
		About:   "A Pulse or signed-webhook destination for domain events."},
	{TypeName: "notification_channel", Plural: "notification-channels", Schema: "ChannelSpec", Title: "notification channel",
		Secrets: []string{"secret"},
		About:   "A Telegram, Slack, Discord or webhook channel for notifications."},
	{TypeName: "notification_rule", Plural: "notification-rules", Schema: "NotificationRuleSpec", Title: "notification rule",
		About: "Routes matching events to notification channels with grouping, hysteresis and quiet hours."},
}
