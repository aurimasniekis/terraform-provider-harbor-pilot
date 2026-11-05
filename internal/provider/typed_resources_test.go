package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"

	"github.com/aurimasniekis/terraform-provider-harbor-pilot/internal/client"
)

const bindingConfig = `
resource "harborpilot_routing_plan" "plan" {
  name = "cred-plan"
  spec = jsonencode({ display_name = "Credentials" })
}

resource "harborpilot_client_binding" "b" {
  name = "cred-client"
  spec = jsonencode({ plan_id = harborpilot_routing_plan.plan.id, enabled = true })
}
`

// noSecretInState fails when any attribute in state contains secret.
func noSecretInState(secret string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		for addr, rs := range s.RootModule().Resources {
			for k, v := range rs.Primary.Attributes {
				if strings.Contains(v, secret) {
					return fmt.Errorf("secret found in state at %s.%s", addr, k)
				}
			}
		}
		return nil
	}
}

func TestAccGatewayCredential(t *testing.T) {
	var credID, bindingID string
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6,
		Steps: []resource.TestStep{
			{
				Config: providerBlock + bindingConfig + `
resource "harborpilot_gateway_credential" "plain" {
  binding_id = harborpilot_client_binding.b.id
  name       = "laptop"
}

resource "harborpilot_gateway_credential" "stored" {
  binding_id           = harborpilot_client_binding.b.id
  name                 = "ci"
  scopes               = ["gateway:invoke"]
  ttl_seconds          = 86400
  bound_refs           = { "ci.pipeline" = "1234" }
  store_token_in_state = true
}`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("harborpilot_gateway_credential.plain", tfjsonpath.New("token"), knownvalue.Null()),
					statecheck.ExpectKnownValue("harborpilot_gateway_credential.stored", tfjsonpath.New("token"), knownvalue.StringRegexp(regexp.MustCompile(`^hpk_`))),
					statecheck.ExpectKnownValue("harborpilot_gateway_credential.plain", tfjsonpath.New("scopes"),
						knownvalue.SetExact([]knownvalue.Check{knownvalue.StringExact("gateway:invoke"), knownvalue.StringExact("gateway:models")})),
					statecheck.ExpectKnownValue("harborpilot_gateway_credential.stored", tfjsonpath.New("expires_at"), knownvalue.NotNull()),
					statecheck.ExpectKnownValue("harborpilot_gateway_credential.plain", tfjsonpath.New("base_urls").AtMapKey("openai"), knownvalue.StringRegexp(regexp.MustCompile(`/v1$`))),
				},
				ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
				Check: func(s *terraform.State) error {
					credID = s.RootModule().Resources["harborpilot_gateway_credential.plain"].Primary.ID
					bindingID = s.RootModule().Resources["harborpilot_client_binding.b"].Primary.ID
					return nil
				},
			},
			{
				ResourceName: "harborpilot_gateway_credential.plain",
				ImportState:  true,
				ImportStateIdFunc: func(*terraform.State) (string, error) {
					return bindingID + "/" + credID, nil
				},
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"base_urls"},
			},
			{
				// Revoked outside Terraform: a new credential is issued.
				PreConfig: func() {
					if err := srv.Client.RevokeGatewayCredential(context.Background(), bindingID, credID); err != nil {
						t.Fatal(err)
					}
				},
				Config: providerBlock + bindingConfig + `
resource "harborpilot_gateway_credential" "plain" {
  binding_id = harborpilot_client_binding.b.id
  name       = "laptop"
}`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("harborpilot_gateway_credential.plain", plancheck.ResourceActionCreate)},
				},
			},
		},
		CheckDestroy: func(*terraform.State) error {
			if bindingID == "" {
				return nil
			}
			if _, err := srv.Client.GetObject(context.Background(), "client-bindings", bindingID); !client.IsNotFound(err) {
				return fmt.Errorf("binding still exists: %w", err)
			}
			return nil
		},
	})
}

func TestAccEphemeralTokenAndWriteOnlySecrets(t *testing.T) {
	var bindingID, channelID string
	fingerprint := func() string {
		var raw struct {
			Secrets map[string]struct {
				Fingerprint string `json:"fingerprint"`
			} `json:"secrets"`
		}
		if err := srv.Client.GetJSON(context.Background(), "/notification-channels/"+channelID, &raw); err != nil {
			t.Fatal(err)
		}
		return raw.Secrets["secret"].Fingerprint
	}
	cfg := func(secret string, version int) string {
		return providerBlock + bindingConfig + fmt.Sprintf(`
ephemeral "harborpilot_gateway_token" "t" {
  binding_id  = harborpilot_client_binding.b.id
  name        = "relay"
  ttl_seconds = 600
}

resource "harborpilot_notification_channel" "relay" {
  name = "relay-webhook"
  spec = jsonencode({
    type                  = "webhook"
    webhook_url           = "http://127.0.0.1:9/relay"
    allow_private_network = true
    enabled               = false
  })
  # An ephemeral value can only flow into write-only arguments.
  secrets_wo         = { secret = %s }
  secrets_wo_version = %d
}`, secret, version)
	}
	var fp1 string
	resource.Test(t, resource.TestCase{
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.SkipBelow(tfversion.Version1_11_0)},
		ProtoV6ProviderFactories: protoV6,
		Steps: []resource.TestStep{
			{
				Config: cfg("ephemeral.harborpilot_gateway_token.t.token", 1),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("harborpilot_notification_channel.relay", tfjsonpath.New("secret_names"),
						knownvalue.SetExact([]knownvalue.Check{knownvalue.StringExact("secret")})),
				},
				Check: resource.ComposeTestCheckFunc(noSecretInState("hpk_"), func(s *terraform.State) error {
					bindingID = s.RootModule().Resources["harborpilot_client_binding.b"].Primary.ID
					channelID = s.RootModule().Resources["harborpilot_notification_channel.relay"].Primary.ID
					fp1 = fingerprint()
					// Every ephemeral token issued during plan and apply was revoked on close.
					cs, err := srv.Client.ListGatewayCredentials(context.Background(), bindingID)
					if err != nil {
						return err
					}
					if len(cs) == 0 {
						return fmt.Errorf("no ephemeral credential was issued")
					}
					for _, c := range cs {
						if c.Name == "relay" && c.RevokedAt == nil {
							return fmt.Errorf("ephemeral credential %s was not revoked", c.ID)
						}
					}
					return nil
				}),
			},
			{
				// A changed write-only value without a version bump is not sent
				// (Terraform cannot diff write-only values).
				Config: cfg(`"static-secret-two"`, 1),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: func(*terraform.State) error {
					if fingerprint() != fp1 {
						return fmt.Errorf("secret changed without a version bump")
					}
					return nil
				},
			},
			{
				// Bumping the version sends the new value.
				Config: cfg(`"static-secret-two"`, 2),
				Check: resource.ComposeTestCheckFunc(noSecretInState("static-secret-two"), func(*terraform.State) error {
					if fingerprint() == fp1 {
						return fmt.Errorf("secret was not rotated")
					}
					return nil
				}),
			},
		},
	})
}

func TestAccServicePrincipalAndAPIToken(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6,
		Steps: []resource.TestStep{
			{
				Config: providerBlock + `
resource "harborpilot_service_principal" "ci" {
  name        = "ci-runner"
  description = "CI pipelines"
  permissions = ["config.read", "tokens.manage"]
}

resource "harborpilot_api_token" "ci" {
  service_principal_id = harborpilot_service_principal.ci.id
  name                 = "pipeline"
  ttl_seconds          = 3600
  store_token_in_state = true
}`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("harborpilot_service_principal.ci", tfjsonpath.New("managed_by"), knownvalue.StringExact("terraform")),
					statecheck.ExpectKnownValue("harborpilot_api_token.ci", tfjsonpath.New("token"), knownvalue.StringRegexp(regexp.MustCompile(`^hpa_`))),
				},
				ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
			{
				Config: providerBlock + `
resource "harborpilot_service_principal" "ci" {
  name        = "ci-runner"
  description = "CI pipelines"
  permissions = ["config.read", "config.write", "tokens.manage"]
  disabled    = true
}`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("harborpilot_service_principal.ci", plancheck.ResourceActionUpdate)},
				},
			},
			{ResourceName: "harborpilot_service_principal.ci", ImportState: true, ImportStateVerify: true},
		},
	})
}

func TestServicePrincipalPermissionValidation(t *testing.T) {
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6,
		Steps: []resource.TestStep{{
			Config: `
provider "harborpilot" {
  url   = "http://127.0.0.1:9"
  token = "hpa_unused"
}

resource "harborpilot_service_principal" "bad" {
  name        = "bad"
  permissions = ["root"]
}`,
			PlanOnly:    true,
			ExpectError: regexp.MustCompile(`value must be one of`),
		}},
	})
}

func TestAccAuthSettingsAndOIDCProvider(t *testing.T) {
	cfg := func(idle int, display string) string {
		return providerBlock + fmt.Sprintf(`
resource "harborpilot_auth_settings" "s" {
  password_login           = true
  idle_timeout_seconds     = %d
  absolute_timeout_seconds = 43200
}

resource "harborpilot_oidc_provider" "sso" {
  slug                     = "corp"
  display_name             = %q
  issuer                   = "http://127.0.0.1:9/issuer"
  allow_private_network    = true
  client_id                = "harbor-pilot"
  client_secret_wo         = "oidc-client-secret-value"
  client_secret_wo_version = 1
  groups_claim             = "groups"
  role_mappings            = [{ group = "platform-admins", role = "admin" }]
  provisioning             = "jit"
  enabled                  = false
}`, idle, display)
	}
	resource.Test(t, resource.TestCase{
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.SkipBelow(tfversion.Version1_11_0)},
		ProtoV6ProviderFactories: protoV6,
		Steps: []resource.TestStep{
			{
				Config: cfg(1800, "Corporate SSO"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("harborpilot_oidc_provider.sso", tfjsonpath.New("client_secret_set"), knownvalue.Bool(true)),
					statecheck.ExpectKnownValue("harborpilot_oidc_provider.sso", tfjsonpath.New("scopes"),
						knownvalue.ListExact([]knownvalue.Check{knownvalue.StringExact("openid"), knownvalue.StringExact("profile"), knownvalue.StringExact("email")})),
					statecheck.ExpectKnownValue("harborpilot_oidc_provider.sso", tfjsonpath.New("redirect_uri"), knownvalue.StringRegexp(regexp.MustCompile(`/api/v1/auth/oidc/callback$`))),
					statecheck.ExpectKnownValue("harborpilot_auth_settings.s", tfjsonpath.New("idle_timeout_seconds"), knownvalue.Int64Exact(1800)),
				},
				Check:            noSecretInState("oidc-client-secret-value"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
			},
			{
				Config: cfg(3600, "Corp SSO"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("harborpilot_auth_settings.s", plancheck.ResourceActionUpdate),
						plancheck.ExpectResourceAction("harborpilot_oidc_provider.sso", plancheck.ResourceActionUpdate),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{ResourceName: "harborpilot_auth_settings.s", ImportState: true, ImportStateId: "auth_settings", ImportStateVerify: true},
			{ResourceName: "harborpilot_oidc_provider.sso", ImportState: true, ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{"client_secret_wo", "client_secret_wo_version"}},
		},
	})
}

func TestAccReadOnlyDataSources(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6,
		Steps: []resource.TestStep{{
			Config: providerBlock + `
data "harborpilot_quota_adapters" "all" {}
data "harborpilot_event_catalog" "all" {}

output "has_deepseek" { value = contains(data.harborpilot_quota_adapters.all.adapters[*].id, "deepseek") }
output "has_quota_event" { value = contains(data.harborpilot_event_catalog.all.events[*].name, "quota.window.exhausted") }
`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownOutputValue("has_deepseek", knownvalue.Bool(true)),
				statecheck.ExpectKnownOutputValue("has_quota_event", knownvalue.Bool(true)),
			},
		}},
	})
}

// TestAccConcurrencyConflict checks that a stale revision is refused (412)
// and reported as a change made outside Terraform. Terraform refreshes
// before every apply, so the stale write is produced directly.
func TestAccConcurrencyConflict(t *testing.T) {
	if srv == nil {
		t.Skip("acceptance tests require TF_ACC=1")
	}
	ctx := context.Background()
	spec := json.RawMessage(`{"display_name":"Conflict","description":""}`)
	o, err := srv.TF.CreateObject(ctx, "workspaces", client.WriteObject{Name: "conflict-ws", Spec: spec})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.TF.DeleteObject(ctx, "workspaces", o.ID, 0) })
	if _, err := srv.Client.ReplaceObject(ctx, "workspaces", o.ID, o.Revision, client.WriteObject{Name: o.Name, Spec: spec}); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		op   func() error
	}{
		{"replace", func() error {
			_, err := srv.TF.ReplaceObject(ctx, "workspaces", o.ID, o.Revision, client.WriteObject{Name: o.Name, Spec: spec})
			return err
		}},
		{"delete", func() error { return srv.TF.DeleteObject(ctx, "workspaces", o.ID, o.Revision) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.op()
			if !client.IsPreconditionFailed(err) {
				t.Fatalf("got %v, want 412", err)
			}
			var d diag.Diagnostics
			addAPIError(&d, "Cannot update workspace", err)
			if !d.HasError() || !strings.Contains(d.Errors()[0].Summary(), "changed outside Terraform") {
				t.Fatalf("diagnostic = %v", d)
			}
		})
	}
}
