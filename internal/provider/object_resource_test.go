package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"

	"github.com/aurimasniekis/terraform-provider-harbor-pilot/internal/client"
)

// allKindsConfig builds one configuration that uses every generic kind,
// wired together by references. label changes display names for updates.
func allKindsConfig(prefix, label string) string {
	return providerBlock + fmt.Sprintf(`
locals { p = %[1]q }

resource "harborpilot_workspace" "w" {
  name = "${local.p}-ws"
  spec = jsonencode({ display_name = "Workspace %[2]s" })
}

resource "harborpilot_project" "p" {
  name = "${local.p}-proj"
  spec = jsonencode({
    workspace_id  = harborpilot_workspace.w.id
    display_name  = "Project %[2]s"
    external_refs = { ticket = "T-1" }
  })
}

resource "harborpilot_provider" "api" {
  name = "${local.p}-api"
  spec = jsonencode({
    type                  = "openai_compatible"
    display_name          = "API %[2]s"
    base_url              = "http://127.0.0.1:9/v1"
    allow_private_network = true
    models = [{
      id                = "model-a"
      display_name      = "Model A"
      context_window    = 128000
      max_output_tokens = 8192
      capabilities      = ["tools", "streaming"]
    }]
  })
}

resource "harborpilot_provider_account" "a" {
  name = "${local.p}-acct"
  spec = jsonencode({
    provider_id    = harborpilot_provider.api.id
    billing        = "metered_api"
    credential_ref = "env:ACC_UPSTREAM_KEY"
    enabled        = true
    labels         = { team = "platform" }
  })
}

resource "harborpilot_subscription_entitlement" "e" {
  name = "${local.p}-ent"
  spec = jsonencode({
    account_id = harborpilot_provider_account.a.id
    plan_name  = "Team"
    fee        = "100"
    currency   = "USD"
    period     = "monthly"
    starts_on  = "2026-01-01"
    windows    = [{ name = "five_hour", seconds = 18000, unit = "percent" }]
  })
}

resource "harborpilot_pool" "pool" {
  name = "${local.p}-pool"
  spec = jsonencode({
    display_name = "Pool %[2]s"
    strategy     = "ordered"
    members      = [{ account_id = harborpilot_provider_account.a.id, model = "model-a", priority = 1, weight = 1 }]
  })
}

resource "harborpilot_model_alias" "alias" {
  name = "${local.p}/default"
  spec = jsonencode({ pool_id = harborpilot_pool.pool.id, description = "Default %[2]s" })
}

resource "harborpilot_privacy_policy" "priv" {
  name = "${local.p}-privacy"
  spec = jsonencode({
    required = false
    rules    = [{ name = "ticket", pattern = "TICKET-[0-9]+", action = "mask" }]
  })
}

resource "harborpilot_trace_policy" "trace" {
  name = "${local.p}-traces"
  spec = jsonencode({ mode = "masked_diagnostic", retention = "3d" })
}

resource "harborpilot_routing_plan" "plan" {
  name = "${local.p}-plan"
  spec = jsonencode({ display_name = "Plan %[2]s" })
}

resource "harborpilot_routing_plan_version" "v" {
  plan_id = harborpilot_routing_plan.plan.id
  note    = "initial"
  document = jsonencode({
    model_mappings   = [{ match = "claude-*", alias_id = harborpilot_model_alias.alias.id }]
    default_alias_id = harborpilot_model_alias.alias.id
    fallback         = { steps = ["alternate_account", "stop"], max_attempts = 2 }
    privacy          = { policy_id = harborpilot_privacy_policy.priv.id, trace_policy_id = harborpilot_trace_policy.trace.id }
  })
}

resource "harborpilot_client_binding" "b" {
  name = "${local.p}-client"
  spec = jsonencode({
    display_name        = "Client %[2]s"
    plan_id             = harborpilot_routing_plan.plan.id
    project_id          = harborpilot_project.p.id
    allowed_project_ids = [harborpilot_project.p.id]
    expected_harness    = "claude_code"
    limits              = { requests_per_minute = 60 }
    trust               = { session_hints = true }
    enabled             = true
  })
  depends_on = [harborpilot_routing_plan_version.v]
}

resource "harborpilot_quota_monitor" "q" {
  name = "${local.p}-quota"
  spec = jsonencode({
    adapter     = "deepseek"
    account_id  = harborpilot_provider_account.a.id
    api_key_ref = "env:ACC_DEEPSEEK_KEY"
    enabled     = false
  })
}

resource "harborpilot_budget" "budget" {
  name = "${local.p}-budget"
  spec = jsonencode({
    scope    = "project"
    scope_id = harborpilot_project.p.id
    period   = "month"
    limit    = "50"
    currency = "usd"
    enabled  = true
  })
}

resource "harborpilot_event_destination" "hook" {
  name = "${local.p}-hook"
  spec = jsonencode({
    type                  = "webhook"
    webhook_url           = "http://127.0.0.1:9/hook"
    allow_private_network = true
    enabled               = false
  })
  secrets_wo         = { signing_secret = "acc-signing-secret-1" }
  secrets_wo_version = 1
}

resource "harborpilot_notification_channel" "ch" {
  name = "${local.p}-channel"
  spec = jsonencode({
    type                  = "webhook"
    webhook_url           = "http://127.0.0.1:9/notify"
    allow_private_network = true
    enabled               = false
  })
}

resource "harborpilot_notification_rule" "rule" {
  name = "${local.p}-rule"
  spec = jsonencode({
    enabled     = true
    channel_ids = [harborpilot_notification_channel.ch.id]
    filter      = { events = ["quota.*", "budget.*"], min_severity = "warning" }
  })
}

data "harborpilot_workspace" "by_name" {
  name = harborpilot_workspace.w.name
}
`, prefix, label)
}

var allKindAddrs = []struct{ addr, plural string }{
	{"harborpilot_workspace.w", "workspaces"},
	{"harborpilot_project.p", "projects"},
	{"harborpilot_provider.api", "providers"},
	{"harborpilot_provider_account.a", "provider-accounts"},
	{"harborpilot_subscription_entitlement.e", "subscription-entitlements"},
	{"harborpilot_pool.pool", "pools"},
	{"harborpilot_model_alias.alias", "model-aliases"},
	{"harborpilot_privacy_policy.priv", "privacy-policies"},
	{"harborpilot_trace_policy.trace", "trace-policies"},
	{"harborpilot_routing_plan.plan", "routing-plans"},
	{"harborpilot_client_binding.b", "client-bindings"},
	{"harborpilot_quota_monitor.q", "quota-monitors"},
	{"harborpilot_budget.budget", "budgets"},
	{"harborpilot_event_destination.hook", "event-destinations"},
	{"harborpilot_notification_channel.ch", "notification-channels"},
	{"harborpilot_notification_rule.rule", "notification-rules"},
}

func checkAllDestroyed(s *terraform.State) error {
	ctx := context.Background()
	for _, rs := range s.RootModule().Resources {
		for _, k := range allKindAddrs {
			if rs.Type != addrType(k.addr) {
				continue
			}
			_, err := srv.Client.GetObject(ctx, k.plural, rs.Primary.ID)
			if err == nil {
				return fmt.Errorf("%s %s still exists", rs.Type, rs.Primary.ID)
			}
			if !client.IsNotFound(err) {
				return err
			}
		}
	}
	return nil
}

func addrType(addr string) string {
	for i := range addr {
		if addr[i] == '.' {
			return addr[:i]
		}
	}
	return addr
}

func stateID(s *terraform.State, addr string) string {
	if r := s.RootModule().Resources[addr]; r != nil {
		return r.Primary.ID
	}
	return ""
}

// TestAccObjectKinds covers create, convergence, update, import, drift,
// out-of-band deletion and destroy for every generic kind.
func TestAccObjectKinds(t *testing.T) {
	var ids = map[string]string{}
	capture := func(s *terraform.State) error {
		for _, k := range allKindAddrs {
			ids[k.addr] = stateID(s, k.addr)
		}
		return nil
	}
	steps := []resource.TestStep{
		{
			// Create; a second plan must be empty (convergence).
			Config: allKindsConfig("acc", "one"),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue("harborpilot_workspace.w", tfjsonpath.New("managed_by"), knownvalue.StringExact("terraform")),
				statecheck.ExpectKnownValue("harborpilot_workspace.w", tfjsonpath.New("drift_state"), knownvalue.StringExact("in_sync")),
				statecheck.ExpectKnownValue("harborpilot_event_destination.hook", tfjsonpath.New("secret_names"),
					knownvalue.SetExact([]knownvalue.Check{knownvalue.StringExact("signing_secret")})),
				statecheck.ExpectKnownValue("harborpilot_event_destination.hook", tfjsonpath.New("secrets_wo"), knownvalue.Null()),
				statecheck.ExpectKnownValue("harborpilot_routing_plan_version.v", tfjsonpath.New("status"), knownvalue.StringExact("published")),
				statecheck.ExpectKnownValue("data.harborpilot_workspace.by_name", tfjsonpath.New("spec"),
					knownvalue.StringExact(`{"description":"","display_name":"Workspace one"}`)),
			},
			Check: capture,
		},
		{
			// Update in place: ids are kept.
			Config: allKindsConfig("acc", "two"),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("harborpilot_workspace.w", plancheck.ResourceActionUpdate),
					plancheck.ExpectResourceAction("harborpilot_pool.pool", plancheck.ResourceActionUpdate),
					plancheck.ExpectResourceAction("harborpilot_routing_plan_version.v", plancheck.ResourceActionNoop),
				},
				PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
			},
			Check: func(s *terraform.State) error {
				for _, k := range allKindAddrs {
					if got := stateID(s, k.addr); got != ids[k.addr] {
						return fmt.Errorf("%s was replaced (%s -> %s)", k.addr, ids[k.addr], got)
					}
				}
				return nil
			},
		},
		{
			// Out-of-band edit: the refresh shows the server's spec and the
			// server flags the object as drifted.
			PreConfig: func() {
				ctx := context.Background()
				o, err := srv.Client.GetObject(ctx, "workspaces", ids["harborpilot_workspace.w"])
				if err != nil {
					t.Fatal(err)
				}
				spec, _ := json.Marshal(map[string]string{"display_name": "Edited in the UI", "description": "manual"})
				if _, err := srv.Client.ReplaceObject(ctx, "workspaces", o.ID, o.Revision, client.WriteObject{Name: o.Name, Spec: spec}); err != nil {
					t.Fatal(err)
				}
			},
			RefreshState:       true,
			ExpectNonEmptyPlan: true,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("harborpilot_workspace.w", "drift_state", "drifted"),
				resource.TestCheckResourceAttr("harborpilot_workspace.w", "spec", `{"description":"manual","display_name":"Edited in the UI"}`),
			),
		},
		{
			// The plan shows the drift, and applying restores the configuration.
			Config: allKindsConfig("acc", "two"),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction("harborpilot_workspace.w", plancheck.ResourceActionUpdate)},
				PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue("harborpilot_workspace.w", tfjsonpath.New("drift_state"), knownvalue.StringExact("in_sync")),
			},
		},
		{
			// Deleted outside Terraform: the next apply recreates it.
			PreConfig: func() {
				if err := srv.Client.DeleteObject(context.Background(), "notification-rules", ids["harborpilot_notification_rule.rule"], 0); err != nil {
					t.Fatal(err)
				}
			},
			Config: allKindsConfig("acc", "two"),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction("harborpilot_notification_rule.rule", plancheck.ResourceActionCreate)},
				PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
			},
		},
		{
			// A published version replaced outside Terraform is published again.
			PreConfig: func() {
				ctx := context.Background()
				planID := ids["harborpilot_routing_plan.plan"]
				vs, err := srv.Client.ListVersions(ctx, planID)
				if err != nil || len(vs) == 0 {
					t.Fatalf("versions: %v", err)
				}
				d, err := srv.Client.CreateDraft(ctx, planID, vs[0].Document, "manual")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := srv.Client.PublishVersion(ctx, planID, d.ID); err != nil {
					t.Fatal(err)
				}
			},
			Config: allKindsConfig("acc", "two"),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction("harborpilot_routing_plan_version.v", plancheck.ResourceActionUpdate)},
				PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
			},
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue("harborpilot_routing_plan_version.v", tfjsonpath.New("status"), knownvalue.StringExact("published")),
			},
		},
	}
	// Import every kind by id; a plan after import converges with one apply.
	for _, k := range allKindAddrs {
		ignore := []string{"spec"}
		if k.addr == "harborpilot_event_destination.hook" {
			ignore = append(ignore, "secrets_wo", "secrets_wo_version")
		}
		steps = append(steps, resource.TestStep{
			Config:                  allKindsConfig("acc", "two"),
			ResourceName:            k.addr,
			ImportState:             true,
			ImportStateVerify:       true,
			ImportStateVerifyIgnore: ignore,
		})
	}
	// Import by name works too.
	steps = append(steps, resource.TestStep{
		Config:            allKindsConfig("acc", "two"),
		ResourceName:      "harborpilot_workspace.w",
		ImportState:       true,
		ImportStateId:     "acc-ws",
		ImportStateVerify: true, ImportStateVerifyIgnore: []string{"spec"},
	})
	steps = append(steps, resource.TestStep{
		Config:       allKindsConfig("acc", "two"),
		ResourceName: "harborpilot_routing_plan_version.v",
		ImportState:  true,
		ImportStateIdFunc: func(s *terraform.State) (string, error) {
			return ids["harborpilot_routing_plan.plan"] + "/" + stateID(s, "harborpilot_routing_plan_version.v"), nil
		},
		ImportStateVerify:       true,
		ImportStateVerifyIgnore: []string{"document", "plan_id", "note"},
	})
	resource.Test(t, resource.TestCase{
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.SkipBelow(tfversion.Version1_11_0)},
		ProtoV6ProviderFactories: protoV6,
		CheckDestroy:             checkAllDestroyed,
		Steps:                    steps,
	})
}

func TestAccObjectValidationErrors(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6,
		Steps: []resource.TestStep{
			{
				Config: providerBlock + `
resource "harborpilot_budget" "bad" {
  name = "acc-bad-budget"
  spec = jsonencode({ scope = "galaxy", period = "month", limit = "1", currency = "USD", enabled = true })
}`,
				ExpectError: regexp.MustCompile(`spec\.scope`),
			},
			{
				Config: providerBlock + `
resource "harborpilot_workspace" "bad" {
  name = "acc-bad-ws"
  spec = jsonencode({ display_name = "x", colour = "blue" })
}`,
				ExpectError: regexp.MustCompile(`spec\.colour: unexpected property`),
			},
		},
	})
}
