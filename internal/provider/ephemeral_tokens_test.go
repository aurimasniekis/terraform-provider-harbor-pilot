package provider

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"
)

// TestAccEphemeralTokensRevokeOnClose checks both ephemeral token types:
// by default they are revoked when Terraform closes them, and with
// revoke_on_close = false they stay valid (for example a parent gateway
// token with tokens:mint). No token reaches state.
func TestAccEphemeralTokensRevokeOnClose(t *testing.T) {
	cfg := providerBlock + bindingConfig + `
resource "harborpilot_service_principal" "orch" {
  name        = "orchestrator"
  permissions = ["config.read", "usage.read", "activity.read"]
}

ephemeral "harborpilot_gateway_token" "parent" {
  binding_id      = harborpilot_client_binding.b.id
  name            = "parent"
  scopes          = ["gateway:invoke", "tokens:mint"]
  bound_refs      = { "spaceman.fleet" = "prod" }
  ttl_seconds     = 604800
  revoke_on_close = false
}

ephemeral "harborpilot_gateway_token" "short" {
  binding_id = harborpilot_client_binding.b.id
  name       = "short"
}

ephemeral "harborpilot_service_principal_token" "kept" {
  service_principal_id = harborpilot_service_principal.orch.id
  name                 = "kept"
  ttl_seconds          = 86400
  revoke_on_close      = false
}

ephemeral "harborpilot_service_principal_token" "short" {
  service_principal_id = harborpilot_service_principal.orch.id
  name                 = "short"
}

resource "harborpilot_notification_channel" "sink" {
  name = "token-sink"
  spec = jsonencode({
    type                  = "webhook"
    webhook_url           = "http://127.0.0.1:9/sink"
    allow_private_network = true
    enabled               = false
  })
  # A webhook channel has one secret; join every token into it so each
  # ephemeral resource is opened.
  secrets_wo = { secret = join(",", concat([
    ephemeral.harborpilot_gateway_token.parent.token,
    ephemeral.harborpilot_gateway_token.short.token,
    ephemeral.harborpilot_service_principal_token.kept.token,
    ephemeral.harborpilot_service_principal_token.short.token,
  ], tolist(ephemeral.harborpilot_service_principal_token.kept.permissions))) }
  secrets_wo_version = 1
}`
	check := func(s *terraform.State) error {
		ctx := context.Background()
		bindingID := s.RootModule().Resources["harborpilot_client_binding.b"].Primary.ID
		spID := s.RootModule().Resources["harborpilot_service_principal.orch"].Primary.ID
		creds, err := srv.Client.ListGatewayCredentials(ctx, bindingID)
		if err != nil {
			return err
		}
		active := map[string]int{}
		issued := map[string]int{}
		for _, c := range creds {
			issued[c.Name]++
			if c.RevokedAt != nil {
				continue
			}
			active[c.Name]++
			if c.Name == "parent" && (!slices.Contains(c.Scopes, "tokens:mint") || c.BoundRefs["spaceman.fleet"] != "prod" || c.ExpiresAt == nil) {
				return fmt.Errorf("parent credential = %+v", c)
			}
		}
		if active["parent"] == 0 || issued["short"] == 0 || active["short"] != 0 {
			return fmt.Errorf("gateway tokens issued %v, active %v; want parent kept and short revoked", issued, active)
		}
		toks, err := srv.Client.ListAPITokens(ctx, spID)
		if err != nil {
			return err
		}
		active, issued = map[string]int{}, map[string]int{}
		for _, tok := range toks {
			issued[tok.Name]++
			if tok.RevokedAt == nil {
				active[tok.Name]++
			}
		}
		if active["kept"] == 0 || issued["short"] == 0 || active["short"] != 0 {
			return fmt.Errorf("API tokens issued %v, active %v; want kept kept and short revoked", issued, active)
		}
		return nil
	}
	resource.Test(t, resource.TestCase{
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.SkipBelow(tfversion.Version1_11_0)},
		ProtoV6ProviderFactories: protoV6,
		Steps: []resource.TestStep{{
			Config: cfg,
			Check:  resource.ComposeTestCheckFunc(noSecretInState("hpk_"), noSecretInState("hpa_"), check),
		}},
	})
}

func TestEphemeralTokenTTLValidation(t *testing.T) {
	resource.UnitTest(t, resource.TestCase{
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.SkipBelow(tfversion.Version1_10_0)},
		ProtoV6ProviderFactories: protoV6,
		Steps: []resource.TestStep{{
			Config: `
provider "harborpilot" {
  url   = "http://127.0.0.1:9"
  token = "hpa_unused"
}

ephemeral "harborpilot_service_principal_token" "t" {
  service_principal_id = "sp_unused"
  ttl_seconds          = 63072001
  revoke_on_close      = false
}`,
			PlanOnly:    true,
			ExpectError: regexp.MustCompile(`must be between 60 and 63072000`),
		}},
	})
}

func TestRevokeOnCloseDefault(t *testing.T) {
	for _, c := range []struct {
		in   types.Bool
		want bool
	}{{types.BoolNull(), true}, {types.BoolUnknown(), true}, {types.BoolValue(true), true}, {types.BoolValue(false), false}} {
		if got := revokeOnClose(c.in); got != c.want {
			t.Errorf("revokeOnClose(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}
