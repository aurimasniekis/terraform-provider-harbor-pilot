package specschema

import (
	"slices"
	"testing"
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
