package provider

import (
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"

	"github.com/aurimasniekis/terraform-provider-harbor-pilot/internal/client"
)

func TestFieldPath(t *testing.T) {
	tests := []struct {
		loc, attr string
	}{
		{"body.name", "name"},
		{"body.spec", "spec"},
		{"body.spec.members[0].model", "spec"},
		{"body.document.fallback.max_attempts", "document"},
		{"body.secrets.credential", "secrets_wo"},
		{"body.other", ""},
	}
	for _, tt := range tests {
		t.Run(tt.loc, func(t *testing.T) {
			p, _ := fieldPath(tt.loc)
			want := path.Empty()
			if tt.attr != "" {
				want = path.Root(tt.attr)
			}
			if !p.Equal(want) {
				t.Fatalf("got %s, want %s", p, want)
			}
		})
	}
}

func TestAddAPIError(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		summary string
		attr    bool
	}{
		{"conflict", &client.Error{Status: 412, Message: "precondition failed"}, "changed outside Terraform", false},
		{"unauthorized", &client.Error{Status: 401, Message: "unauthorized"}, "not authenticated", false},
		{"forbidden", &client.Error{Status: 403, Message: "missing permission"}, "permission denied", false},
		{"field", &client.Error{Status: 422, Message: "validation failed", Fields: []client.FieldError{{Location: "body.spec.scope", Message: "must be global"}}}, "Cannot", true},
		{"plain", errors.New("dial tcp: refused"), "Cannot", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var d diag.Diagnostics
			addAPIError(&d, "Cannot create budget", tt.err)
			if d.ErrorsCount() != 1 || !strings.Contains(d.Errors()[0].Summary(), tt.summary) {
				t.Fatalf("diags = %v", d)
			}
			_, isAttr := d.Errors()[0].(diag.DiagnosticWithPath)
			if isAttr != tt.attr {
				t.Fatalf("attribute diagnostic = %v, want %v", isAttr, tt.attr)
			}
		})
	}
}
