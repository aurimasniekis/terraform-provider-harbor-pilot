package specschema

import (
	"maps"
	"os"
	"slices"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestFillJSON(t *testing.T) {
	tests := []struct {
		name, schema, in, want string
	}{
		{
			name:   "required properties are filled",
			schema: "ProjectSpec",
			in:     `{"display_name":"P"}`,
			want:   `{"display_name":"P","workspace_id":""}`,
		},
		{
			name:   "optional properties are left out",
			schema: "WorkspaceSpec",
			in:     `{"display_name":"W"}`,
			want:   `{"display_name":"W"}`,
		},
		{
			name:   "enabled is never filled",
			schema: "AccountSpec",
			in:     `{"billing":"metered_api"}`,
			want:   `{"billing":"metered_api","provider_id":""}`,
		},
		{
			name:   "nested objects and array items are filled",
			schema: "ProviderSpec",
			in:     `{"type":"openai_compatible","models":[{"pricing":{"input_per_mtok":"1"}}]}`,
			want:   `{"models":[{"id":"","pricing":{"currency":"","input_per_mtok":"1","output_per_mtok":""}}],"type":"openai_compatible"}`,
		},
		{
			name:   "required strings in array items are filled",
			schema: "PlanDocument",
			in:     `{"model_mappings":[{}]}`,
			want:   `{"model_mappings":[{"alias_id":"","match":""}]}`,
		},
		{
			// unit is an enum without "" and seconds has minimum 60: both
			// stay missing so the server names them; name is filled.
			name:   "enum without empty value and positive minimum stay missing",
			schema: "EntitlementSpec",
			in:     `{"account_id":"a","fee":"0","currency":"USD","period":"monthly","starts_on":"2026-01-01","windows":[{}]}`,
			want:   `{"account_id":"a","currency":"USD","fee":"0","period":"monthly","starts_on":"2026-01-01","windows":[{"name":""}]}`,
		},
		{
			// An omitted fallback.steps gets the server's default fallback;
			// filling it with [] would mean "no fallback".
			name:   "omitted fallback steps are not filled",
			schema: "PlanDocument",
			in:     `{"fallback":{"max_attempts":2}}`,
			want:   `{"fallback":{"max_attempts":2}}`,
		},
		{
			// period (enum without "") is left for the server to name; the
			// optional one-time window fields are not sent, which the server
			// reads like "" (unset).
			name:   "budget period and one-time window are not filled",
			schema: "BudgetSpec",
			in:     `{"scope":"global","enabled":true}`,
			want:   `{"currency":"","enabled":true,"limit":"","scope":"global"}`,
		},
		{
			name:   "one-time budget keeps its window",
			schema: "BudgetSpec",
			in:     `{"scope":"global","period":"once","limit":"10","currency":"USD","ends_at":"2027-01-01T00:00:00Z","enabled":true}`,
			want:   `{"currency":"USD","enabled":true,"ends_at":"2027-01-01T00:00:00Z","limit":"10","period":"once","scope":"global"}`,
		},
		{
			// D-046 (Harbor Pilot 0.1.6): the three optional model fields are
			// sent as written. request_fields values are free-form JSON, so
			// large integers and nested objects pass through unchanged.
			name:   "model request fields are sent as written",
			schema: "ProviderSpec",
			in:     `{"type":"openai_compatible","models":[{"id":"vendor/model-a","provider_routing":{"only":["provider-a"],"allow_fallbacks":false,"max_price":{"prompt":1.5}},"request_fields":{"seed":12345678901234567890,"temperature":0.2,"metadata":{"b":1,"a":[1e3]}},"request_field_policy":"client"}]}`,
			want:   `{"models":[{"id":"vendor/model-a","provider_routing":{"allow_fallbacks":false,"max_price":{"prompt":1.5},"only":["provider-a"]},"request_field_policy":"client","request_fields":{"metadata":{"a":[1e3],"b":1},"seed":12345678901234567890,"temperature":0.2}}],"type":"openai_compatible"}`,
		},
		{
			name:   "model request fields left out stay out",
			schema: "ProviderSpec",
			in:     `{"type":"openai_compatible","models":[{"id":"vendor/model-a"}]}`,
			want:   `{"models":[{"id":"vendor/model-a"}],"type":"openai_compatible"}`,
		},
		{
			// D-047 (Harbor Pilot 0.1.7): the reasoning option has no required
			// properties, so only the fields written are sent.
			name:   "model reasoning is sent as written",
			schema: "ProviderSpec",
			in:     `{"type":"openai_compatible","models":[{"id":"vendor/model-a","reasoning":{"mode":"client","style":"openrouter_reasoning","effort":"medium","efforts":["low","medium","high"]}}]}`,
			want:   `{"models":[{"id":"vendor/model-a","reasoning":{"effort":"medium","efforts":["low","medium","high"],"mode":"client","style":"openrouter_reasoning"}}],"type":"openai_compatible"}`,
		},
		{
			name:   "unknown kinds pass through",
			schema: "NoSuchSpec",
			in:     `{"a":1}`,
			want:   `{"a":1}`,
		},
		{
			name:   "explicit values are kept",
			schema: "WorkspaceSpec",
			in:     `{"display_name":"W","description":"d"}`,
			want:   `{"description":"d","display_name":"W"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FillJSON(tt.schema, []byte(tt.in))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Fatalf("got  %s\nwant %s", got, tt.want)
			}
		})
	}
}

func TestFillJSONRejectsNonObjects(t *testing.T) {
	for _, in := range []string{`[]`, `"x"`, `null`, `{`} {
		if _, err := FillJSON("WorkspaceSpec", []byte(in)); err == nil {
			t.Errorf("%s: expected an error", in)
		}
	}
}

func TestCanonical(t *testing.T) {
	a, _ := Canonical([]byte(`{ "b": [1, 2], "a": {"y": 1, "x": 2} }`))
	b, _ := Canonical([]byte(`{"a":{"x":2,"y":1},"b":[1,2]}`))
	if a != b {
		t.Fatalf("%s != %s", a, b)
	}
}

func TestBudgetSchemaFromV014(t *testing.T) {
	n := Get("BudgetSpec")
	if n == nil {
		t.Fatal("no BudgetSpec schema")
	}
	if p := n.Props["period"]; p == nil || !slices.Contains(p.Enum, "once") || slices.Contains(p.Enum, "") {
		t.Fatalf("period enum = %v, want once and no empty value", p)
	}
	for _, k := range []string{"starts_at", "ends_at"} {
		if p := n.Props[k]; p == nil || p.Type != "string" {
			t.Errorf("%s: %+v, want an optional string", k, p)
		}
		if slices.Contains(n.Required, k) {
			t.Errorf("%s is required", k)
		}
	}
}

func TestCanonicalKeepsNumbers(t *testing.T) {
	got, err := Canonical([]byte(`{"seed": 12345678901234567890, "t": 0.2}`))
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"seed":12345678901234567890,"t":0.2}`; got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
	if _, err := Canonical([]byte(`{} {}`)); err == nil {
		t.Fatal("trailing data accepted")
	}
}

func TestProviderModelSchemaFromV016(t *testing.T) {
	n := Get("ProviderSpec")
	if n == nil || n.Props["models"] == nil || n.Props["models"].Items == nil {
		t.Fatal("no ProviderSpec models schema")
	}
	m := n.Props["models"].Items
	for _, k := range []string{"provider_routing", "request_fields", "request_field_policy"} {
		if m.Props[k] == nil {
			t.Fatalf("model property %s missing", k)
		}
		if slices.Contains(m.Required, k) {
			t.Errorf("%s is required", k)
		}
	}
	if !m.Props["request_fields"].Map {
		t.Error("request_fields is not a map")
	}
	if p := m.Props["request_field_policy"]; !slices.Equal(p.Enum, []string{"configured", "client", "reject", ""}) {
		t.Errorf("request_field_policy enum = %v", p.Enum)
	}
	r := m.Props["provider_routing"]
	for _, k := range []string{"order", "only", "ignore", "sort", "allow_fallbacks", "require_parameters",
		"data_collection", "zdr", "quantizations", "max_price"} {
		if r.Props[k] == nil {
			t.Errorf("provider_routing.%s missing", k)
		}
	}
	if len(r.Required) != 0 {
		t.Errorf("provider_routing requires %v", r.Required)
	}
}

func TestProviderModelReasoningSchemaFromV017(t *testing.T) {
	n := Get("ProviderSpec")
	if n == nil || n.Props["models"] == nil || n.Props["models"].Items == nil {
		t.Fatal("no ProviderSpec models schema")
	}
	m := n.Props["models"].Items
	r := m.Props["reasoning"]
	if r == nil {
		t.Fatal("model property reasoning missing")
	}
	if slices.Contains(m.Required, "reasoning") {
		t.Error("reasoning is required")
	}
	for _, k := range []string{"mode", "style", "effort", "budget_tokens", "max_budget_tokens", "efforts", "history", "history_field"} {
		if r.Props[k] == nil {
			t.Errorf("reasoning.%s missing", k)
		}
	}
	if len(r.Required) != 0 {
		t.Errorf("reasoning requires %v", r.Required)
	}
	if p := r.Props["mode"]; p == nil || !slices.Equal(p.Enum, []string{"auto", "client", "fixed", "off", ""}) {
		t.Errorf("reasoning.mode enum = %v", p)
	}
}

// TestEnumsMatchOpenAPI fails when enums.json is stale: it re-derives the
// enums from the vendored OpenAPI document without the generator.
func TestEnumsMatchOpenAPI(t *testing.T) {
	b, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var d struct {
		Paths      map[string]map[string]any `yaml:"paths"`
		Components struct {
			Schemas map[string]struct {
				Properties map[string]struct {
					Enum  []string `yaml:"enum"`
					Items struct {
						Enum []string `yaml:"enum"`
					} `yaml:"items"`
				} `yaml:"properties"`
			} `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(b, &d); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{}
	for _, item := range d.Paths {
		for _, op := range item {
			if m, ok := op.(map[string]any); ok {
				if p, _ := m["x-permission"].(string); p != "" && p != "public" && p != "session" {
					want[p] = true
				}
			}
		}
	}
	for _, g := range d.Components.Schemas["GrantsInBody"].Properties["grants"].Items.Enum {
		want[g] = true
	}
	wantList := slices.Sorted(maps.Keys(want))
	if got := Enum("permissions"); !slices.Equal(got, wantList) {
		t.Errorf("permissions = %v, OpenAPI has %v (run make gen)", got, wantList)
	}
	for _, p := range []string{"traces.content.read", "traces.content.unredacted", "traces.export"} {
		if !slices.Contains(Enum("permissions"), p) {
			t.Errorf("permissions lack %s", p)
		}
	}
	oidc := d.Components.Schemas["OidcBody"].Properties
	if got := Enum("oidc_default_role"); !slices.Equal(got, oidc["default_role"].Enum) {
		t.Errorf("oidc_default_role = %v, OpenAPI has %v", got, oidc["default_role"].Enum)
	}
	if got := Enum("oidc_provisioning"); !slices.Equal(got, oidc["provisioning"].Enum) {
		t.Errorf("oidc_provisioning = %v, OpenAPI has %v", got, oidc["provisioning"].Enum)
	}
	// RoleMapping.role has no enum of its own: it takes the built-in roles.
	if got, w := Enum("oidc_role"), slices.DeleteFunc(slices.Clone(oidc["default_role"].Enum), func(s string) bool { return s == "" }); len(w) == 0 || !slices.Equal(got, w) {
		t.Errorf("oidc_role = %v, OpenAPI has %v", got, w)
	}
}
