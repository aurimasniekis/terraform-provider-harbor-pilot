package provider

import (
	"bytes"
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

	"github.com/aurimasniekis/terraform-provider-harbor-pilot/internal/client"
)

// requestFieldsConfig is an OpenAI-compatible provider whose model carries
// the D-046 fields (Harbor Pilot 0.1.6). model is HCL for the model's
// extra properties, or "" for none.
func requestFieldsConfig(model string) string {
	return providerBlock + fmt.Sprintf(`
resource "harborpilot_provider" "router" {
  name = "acc-router"
  spec = jsonencode({
    type                  = "openai_compatible"
    display_name          = "Router"
    base_url              = "http://127.0.0.1:9/v1"
    allow_private_network = true
    models = [{
      id = "vendor/model-a"
      %s
    }]
  })
}
`, model)
}

const requestFieldsAll = `
      provider_routing = {
        order              = ["provider-a", "provider-b"]
        only               = ["provider-a", "provider-b"]
        ignore             = ["provider-c"]
        sort               = "price"
        allow_fallbacks    = false
        require_parameters = true
        data_collection    = "deny"
        zdr                = true
        quantizations      = ["fp8"]
        max_price          = { prompt = 1.5, completion = 4 }
      }
      request_fields = {
        temperature = 0.2
        seed        = 12345678901234567890
        transforms  = ["middle-out"]
        metadata    = { team = "platform", tier = "a" }
      }
      request_field_policy = "client"`

const requestFieldsUpdated = `
      provider_routing     = { only = ["provider-b"], allow_fallbacks = true }
      request_fields       = { top_p = 0.9, temperature = 0 }
      request_field_policy = "reject"`

// storedModel reads the provider's first model from the server, keeping
// raw JSON values so numbers are compared exactly.
func storedModel(id string) (map[string]json.RawMessage, error) {
	o, err := srv.Client.GetObject(context.Background(), "providers", id)
	if err != nil {
		return nil, err
	}
	var spec struct {
		Models []map[string]json.RawMessage `json:"models"`
	}
	if err := json.Unmarshal(o.Spec, &spec); err != nil {
		return nil, err
	}
	if len(spec.Models) != 1 {
		return nil, fmt.Errorf("stored %d models, want 1", len(spec.Models))
	}
	return spec.Models[0], nil
}

// semantic decodes JSON keeping exact number text and drops empty values
// (the server stores provider_routing with its zero-valued properties), then
// re-encodes it with sorted keys. Unset, null and empty values give "".
func semantic(raw []byte) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return "", err
	}
	v = dropEmpty(v)
	if v == nil {
		return "", nil
	}
	b, err := json.Marshal(v)
	return string(b), err
}

func dropEmpty(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			if e = dropEmpty(e); e == nil {
				delete(x, k)
			} else {
				x[k] = e
			}
		}
		if len(x) == 0 {
			return nil
		}
	case []any:
		if len(x) == 0 {
			return nil
		}
	case string:
		if x == "" {
			return nil
		}
	}
	return v
}

// checkStoredModel compares the stored model's fields with want (JSON;
// "" means unset or empty). Numbers are compared exactly.
func checkStoredModel(want map[string]string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs := s.RootModule().Resources["harborpilot_provider.router"]
		if rs == nil {
			return fmt.Errorf("provider not in state")
		}
		m, err := storedModel(rs.Primary.ID)
		if err != nil {
			return err
		}
		for k, w := range want {
			got, err := semantic(m[k])
			if err != nil {
				return fmt.Errorf("%s: %w", k, err)
			}
			if w, err = semantic([]byte(w)); err != nil {
				return fmt.Errorf("%s: want: %w", k, err)
			}
			if got != w {
				return fmt.Errorf("%s: stored %s, want %s", k, got, w)
			}
		}
		return nil
	}
}

// setModelFieldsOutOfBand writes model fields as an operator would in the UI.
func setModelFieldsOutOfBand(t *testing.T, fields map[string]any) {
	t.Helper()
	ctx := context.Background()
	o, err := srv.Client.GetObject(ctx, "providers", "acc-router")
	if err != nil {
		t.Fatal(err)
	}
	var spec map[string]any
	if err := json.Unmarshal(o.Spec, &spec); err != nil {
		t.Fatal(err)
	}
	model := spec["models"].([]any)[0].(map[string]any)
	for k, v := range fields {
		model[k] = v
	}
	b, _ := json.Marshal(spec)
	if _, err := srv.Client.ReplaceObject(ctx, "providers", o.ID, o.Revision, client.WriteObject{Name: o.Name, Spec: b}); err != nil {
		t.Fatal(err)
	}
}

// TestAccProviderRequestFields covers a model's provider_routing,
// request_fields and request_field_policy (Harbor Pilot 0.1.6, D-046):
// create, convergence, exact numbers, update, import, drift, and removal,
// which clears the fields on the server like any omitted spec property.
func TestAccProviderRequestFields(t *testing.T) {
	empty := []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}
	update := []plancheck.PlanCheck{plancheck.ExpectResourceAction("harborpilot_provider.router", plancheck.ResourceActionUpdate)}
	// Omitted fields are cleared; the policy then reads as the server's
	// default, configured.
	cleared := map[string]string{"provider_routing": "", "request_fields": "", "request_field_policy": `"configured"`}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6,
		Steps: []resource.TestStep{
			{
				Config:           requestFieldsConfig(requestFieldsAll),
				ConfigPlanChecks: resource.ConfigPlanChecks{PostApplyPostRefresh: empty},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("harborpilot_provider.router", tfjsonpath.New("drift_state"), knownvalue.StringExact("in_sync")),
				},
				Check: checkStoredModel(map[string]string{
					"provider_routing": `{"order":["provider-a","provider-b"],"only":["provider-a","provider-b"],"ignore":["provider-c"],` +
						`"sort":"price","allow_fallbacks":false,"require_parameters":true,"data_collection":"deny","zdr":true,` +
						`"quantizations":["fp8"],"max_price":{"prompt":1.5,"completion":4}}`,
					"request_fields":       `{"temperature":0.2,"seed":12345678901234567890,"transforms":["middle-out"],"metadata":{"team":"platform","tier":"a"}}`,
					"request_field_policy": `"client"`,
				}),
			},
			{
				// Re-planning the same configuration is still empty.
				Config:   requestFieldsConfig(requestFieldsAll),
				PlanOnly: true,
			},
			{
				Config: requestFieldsConfig(requestFieldsUpdated),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             update,
					PostApplyPostRefresh: empty,
				},
				Check: checkStoredModel(map[string]string{
					"provider_routing":     `{"only":["provider-b"],"allow_fallbacks":true}`,
					"request_fields":       `{"top_p":0.9,"temperature":0}`,
					"request_field_policy": `"reject"`,
				}),
			},
			{
				// Import by id: the server's spec carries the fields.
				Config:            requestFieldsConfig(requestFieldsUpdated),
				ResourceName:      "harborpilot_provider.router",
				ImportState:       true,
				ImportStateVerify: true, ImportStateVerifyIgnore: []string{"spec"},
				ImportStateCheck: checkImportedRequestFields,
			},
			{
				// Import by name.
				Config:            requestFieldsConfig(requestFieldsUpdated),
				ResourceName:      "harborpilot_provider.router",
				ImportState:       true,
				ImportStateId:     "acc-router",
				ImportStateVerify: true, ImportStateVerifyIgnore: []string{"spec"},
				ImportStateCheck: checkImportedRequestFields,
			},
			{
				// Changed in the UI: the plan shows it and the apply restores
				// the configuration.
				PreConfig: func() {
					setModelFieldsOutOfBand(t, map[string]any{"request_fields": map[string]any{"top_p": 0.5}})
				},
				Config: requestFieldsConfig(requestFieldsUpdated),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             update,
					PostApplyPostRefresh: empty,
				},
				Check: checkStoredModel(map[string]string{"request_fields": `{"top_p":0.9,"temperature":0}`}),
			},
			{
				// Removed from the configuration: the apply clears them.
				Config: requestFieldsConfig(""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             update,
					PostApplyPostRefresh: empty,
				},
				Check: checkStoredModel(cleared),
			},
			{
				// Set only in the UI on a model whose configuration leaves
				// them out: the next apply clears them, like any drift.
				PreConfig: func() {
					setModelFieldsOutOfBand(t, map[string]any{
						"provider_routing":     map[string]any{"only": []string{"provider-a"}},
						"request_fields":       map[string]any{"temperature": 1},
						"request_field_policy": "client",
					})
				},
				Config: requestFieldsConfig(""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             update,
					PostApplyPostRefresh: empty,
				},
				Check: checkStoredModel(cleared),
			},
		},
	})
}

func checkImportedRequestFields(states []*terraform.InstanceState) error {
	if len(states) != 1 {
		return fmt.Errorf("imported %d instances, want 1", len(states))
	}
	var spec struct {
		Models []struct {
			ProviderRouting struct {
				Only           []string `json:"only"`
				AllowFallbacks *bool    `json:"allow_fallbacks"`
			} `json:"provider_routing"`
			RequestFields      map[string]json.RawMessage `json:"request_fields"`
			RequestFieldPolicy string                     `json:"request_field_policy"`
		} `json:"models"`
	}
	if err := json.Unmarshal([]byte(states[0].Attributes["spec"]), &spec); err != nil {
		return err
	}
	if len(spec.Models) != 1 {
		return fmt.Errorf("imported %d models", len(spec.Models))
	}
	m := spec.Models[0]
	if fmt.Sprint(m.ProviderRouting.Only) != "[provider-b]" || m.ProviderRouting.AllowFallbacks == nil || !*m.ProviderRouting.AllowFallbacks ||
		string(m.RequestFields["top_p"]) != "0.9" || m.RequestFieldPolicy != "reject" {
		return fmt.Errorf("imported model %+v", m)
	}
	return nil
}

// TestAccProviderRequestFieldsErrors checks that the server's validation
// (the per-provider-type allowlist and the policy values) reaches the user
// as an error on spec, with the server's message and field path.
func TestAccProviderRequestFieldsErrors(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6,
		Steps: []resource.TestStep{
			{
				Config:      requestFieldsConfig(`request_fields = { max_tokens = 100000 }`),
				ExpectError: regexp.MustCompile(`(?s)spec\.models\[0\]\.request_fields.*max_tokens: cannot be configured`),
			},
			{
				Config:      requestFieldsConfig(`request_field_policy = "sometimes"`),
				ExpectError: regexp.MustCompile(`spec\.models\[0\]\.request_field_policy`),
			},
		},
	})
}
