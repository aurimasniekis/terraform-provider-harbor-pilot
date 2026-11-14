package provider

import (
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
)

// The reasoning option (Harbor Pilot 0.1.7, D-047) is a model property in
// the provider's JSON spec; these tests reuse requestFieldsConfig.
const reasoningClient = `
      reasoning = {
        mode    = "client"
        style   = "openrouter_reasoning"
        effort  = "medium"
        efforts = ["low", "medium", "high"]
      }`

const reasoningUpdated = `
      reasoning = {
        mode              = "client"
        style             = "openrouter_reasoning"
        budget_tokens     = 2048
        max_budget_tokens = 8192
        history           = "field"
      }`

// reasoningUnordered is reasoningClient as the server does not store it:
// efforts out of order and repeated, and the default history spelled out.
const reasoningUnordered = `
      reasoning = {
        mode    = "client"
        style   = "openrouter_reasoning"
        effort  = "medium"
        efforts = ["high", "low", "medium", "low"]
        history = "drop"
      }`

// checkNoStoredKey checks that the stored model has no key at all (not even
// null or an empty object).
func checkNoStoredKey(key string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs := s.RootModule().Resources["harborpilot_provider.router"]
		if rs == nil {
			return fmt.Errorf("provider not in state")
		}
		m, err := storedModel(rs.Primary.ID)
		if err != nil {
			return err
		}
		if v, ok := m[key]; ok {
			return fmt.Errorf("stored model has %s = %s, want no key", key, v)
		}
		return nil
	}
}

// TestAccProviderReasoning covers a model's reasoning option: create,
// convergence, the server's normalised form, update, import, drift, and
// removal, which clears the option on the server like any omitted property.
func TestAccProviderReasoning(t *testing.T) {
	empty := []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}
	update := []plancheck.PlanCheck{plancheck.ExpectResourceAction("harborpilot_provider.router", plancheck.ResourceActionUpdate)}
	clientStored := map[string]string{
		"reasoning": `{"mode":"client","style":"openrouter_reasoning","effort":"medium","efforts":["low","medium","high"]}`,
	}
	updatedStored := map[string]string{
		"reasoning": `{"mode":"client","style":"openrouter_reasoning","budget_tokens":2048,"max_budget_tokens":8192,"history":"field"}`,
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6,
		Steps: []resource.TestStep{
			{
				Config:           requestFieldsConfig(reasoningClient),
				ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: empty},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("harborpilot_provider.router", tfjsonpath.New("drift_state"), knownvalue.StringExact("in_sync")),
				},
				Check: checkStoredModel(clientStored),
			},
			{
				// Re-planning the same configuration is still empty.
				Config:   requestFieldsConfig(reasoningClient),
				PlanOnly: true,
			},
			{
				// The server sorts and de-duplicates efforts and drops the
				// default history; the stored form causes no diff.
				Config: requestFieldsConfig(reasoningUnordered),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             update,
					PostApplyPostRefresh: empty,
				},
				Check: checkStoredModel(clientStored),
			},
			{
				Config:   requestFieldsConfig(reasoningUnordered),
				PlanOnly: true,
			},
			{
				Config: requestFieldsConfig(reasoningUpdated),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             update,
					PostApplyPostRefresh: empty,
				},
				Check: checkStoredModel(updatedStored),
			},
			{
				// Import by id: the server's spec carries the option.
				Config:            requestFieldsConfig(reasoningUpdated),
				ResourceName:      "harborpilot_provider.router",
				ImportState:       true,
				ImportStateVerify: true, ImportStateVerifyIgnore: []string{"spec"},
				ImportStateCheck: checkImportedReasoning,
			},
			{
				// Import by name.
				Config:            requestFieldsConfig(reasoningUpdated),
				ResourceName:      "harborpilot_provider.router",
				ImportState:       true,
				ImportStateId:     "acc-router",
				ImportStateVerify: true, ImportStateVerifyIgnore: []string{"spec"},
				ImportStateCheck: checkImportedReasoning,
			},
			{
				// Changed in the UI: the plan shows it and the apply restores
				// the configuration.
				PreConfig: func() {
					setModelFieldsOutOfBand(t, map[string]any{"reasoning": map[string]any{"mode": "off"}})
				},
				Config: requestFieldsConfig(reasoningUpdated),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             update,
					PostApplyPostRefresh: empty,
				},
				Check: checkStoredModel(updatedStored),
			},
			{
				// Removed from the configuration: the apply clears it, and the
				// server stores no reasoning key.
				Config: requestFieldsConfig(""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             update,
					PostApplyPostRefresh: empty,
				},
				Check: checkNoStoredKey("reasoning"),
			},
			{
				// An option that changes nothing (mode auto alone) is stored
				// as no option at all, and still converges.
				Config: requestFieldsConfig(`reasoning = { mode = "auto" }`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             update,
					PostApplyPostRefresh: empty,
				},
				Check: checkNoStoredKey("reasoning"),
			},
			{
				// Set only in the UI on a model whose configuration leaves it
				// out: the next apply clears it, like any drift.
				PreConfig: func() {
					setModelFieldsOutOfBand(t, map[string]any{"reasoning": map[string]any{"mode": "off"}})
				},
				Config: requestFieldsConfig(""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             update,
					PostApplyPostRefresh: empty,
				},
				Check: checkNoStoredKey("reasoning"),
			},
		},
	})
}

func checkImportedReasoning(states []*terraform.InstanceState) error {
	if len(states) != 1 {
		return fmt.Errorf("imported %d instances, want 1", len(states))
	}
	var spec struct {
		Models []struct {
			Reasoning *struct {
				Mode            string   `json:"mode"`
				Style           string   `json:"style"`
				Effort          string   `json:"effort"`
				BudgetTokens    int      `json:"budget_tokens"`
				MaxBudgetTokens int      `json:"max_budget_tokens"`
				Efforts         []string `json:"efforts"`
				History         string   `json:"history"`
			} `json:"reasoning"`
		} `json:"models"`
	}
	if err := json.Unmarshal([]byte(states[0].Attributes["spec"]), &spec); err != nil {
		return err
	}
	if len(spec.Models) != 1 {
		return fmt.Errorf("imported %d models", len(spec.Models))
	}
	r := spec.Models[0].Reasoning
	if r == nil || r.Mode != "client" || r.Style != "openrouter_reasoning" || r.Effort != "" || r.BudgetTokens != 2048 ||
		r.MaxBudgetTokens != 8192 || len(r.Efforts) != 0 || r.History != "field" {
		return fmt.Errorf("imported reasoning %+v", r)
	}
	return nil
}

// TestAccProviderReasoningUnset checks that a model without the option is
// stored with no reasoning key and plans no diff.
func TestAccProviderReasoningUnset(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6,
		Steps: []resource.TestStep{
			{
				Config:           requestFieldsConfig(""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
				Check: resource.ComposeTestCheckFunc(checkNoStoredKey("reasoning"), func(s *terraform.State) error {
					// The spec in state is the configuration as written.
					spec := s.RootModule().Resources["harborpilot_provider.router"].Primary.Attributes["spec"]
					if regexp.MustCompile(`"reasoning"`).MatchString(spec) {
						return fmt.Errorf("state spec has reasoning: %s", spec)
					}
					return nil
				}),
			},
			{
				Config:   requestFieldsConfig(""),
				PlanOnly: true,
			},
		},
	})
}

// TestAccProviderReasoningErrors checks that the server's validation of the
// option reaches the user as an error on spec, with its message and path.
func TestAccProviderReasoningErrors(t *testing.T) {
	cases := []struct{ model, want string }{
		{
			// A fixed budget with a style that sends only effort levels.
			model: `reasoning = { mode = "fixed", style = "deepseek_thinking", budget_tokens = 1024 }`,
			want:  `(?s)spec\.models\[0\]\.reasoning\.budget_tokens.*deepseek_thinking\s+sends\s+an\s+effort\s+level,\s+not\s+a\s+budget`,
		},
		{
			// A style the provider type does not accept.
			model: `reasoning = { mode = "client", style = "anthropic_thinking" }`,
			want:  `(?s)spec\.models\[0\]\.reasoning\.style.*openai_compatible`,
		},
		{
			model: `reasoning = { mode = "fixed", style = "openrouter_reasoning", effort = "high", budget_tokens = 1024 }`,
			want:  `(?s)spec\.models\[0\]\.reasoning\.budget_tokens.*set\s+effort\s+or\s+budget_tokens,\s+not\s+both`,
		},
	}
	steps := make([]resource.TestStep, 0, len(cases))
	for _, c := range cases {
		steps = append(steps, resource.TestStep{Config: requestFieldsConfig(c.model), ExpectError: regexp.MustCompile(c.want)})
	}
	resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: protoV6, Steps: steps})
}
