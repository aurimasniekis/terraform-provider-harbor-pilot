package provider

import (
	"context"
	"slices"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
)

func TestServicePrincipalPermissionsValidation(t *testing.T) {
	var resp resource.SchemaResponse
	(&servicePrincipalResource{}).Schema(context.Background(), resource.SchemaRequest{}, &resp)
	attr := resp.Schema.Attributes["permissions"].(schema.SetAttribute)

	for _, p := range []string{"traces.content.read", "traces.content.unredacted", "traces.export", "config.read"} {
		if !slices.Contains(permissions, p) {
			t.Errorf("permissions lack %q", p)
		}
	}
	check := func(vals ...string) bool {
		set := setOf(vals)
		req := validator.SetRequest{Path: path.Root("permissions"), ConfigValue: set,
			Config: tfsdk.Config{}}
		var out validator.SetResponse
		for _, v := range attr.Validators {
			v.ValidateSet(context.Background(), req, &out)
		}
		return !out.Diagnostics.HasError()
	}
	if !check(permissions...) {
		t.Error("every server permission must be accepted")
	}
	if !check("traces.content.read") {
		t.Error("traces.content.read must be accepted")
	}
	if check("config.read", "nope.write") {
		t.Error("an unknown permission must be rejected")
	}
}
