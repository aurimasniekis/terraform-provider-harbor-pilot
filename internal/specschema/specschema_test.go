package specschema

import (
	"testing"
)

func TestFillJSON(t *testing.T) {
	tests := []struct {
		name, schema, in, want string
	}{
		{
			name:   "workspace fills description",
			schema: "WorkspaceSpec",
			in:     `{"display_name":"W"}`,
			want:   `{"description":"","display_name":"W"}`,
		},
		{
			name:   "enabled is never filled",
			schema: "AccountSpec",
			in:     `{"provider_id":"prv_1","billing":"metered_api"}`,
			want:   `{"allowed_models":[],"billing":"metered_api","credential_ref":"","display_name":"","labels":{},"max_concurrency":0,"provider_id":"prv_1","requests_per_minute":0}`,
		},
		{
			name:   "nested objects and array items are filled",
			schema: "PoolSpec",
			in:     `{"members":[{"account_id":"acc_1","model":"m"}]}`,
			want:   `{"affinity":"","display_name":"","members":[{"account_id":"acc_1","model":"m","priority":0,"weight":0}],"strategy":""}`,
		},
		{
			name:   "enum without empty value and positive minimum stay missing",
			schema: "PlanDocument",
			in:     `{}`,
			want:   `{"default_alias_id":"","fallback":{"allow_paid_fallback":false,"steps":[],"wait_max_seconds":0},"model_mappings":[],"restrictions":{"allowed_billing":[],"allowed_classifications":[],"allowed_provider_ids":[],"denied_provider_ids":[],"required_capabilities":[]},"schema_version":0}`,
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
