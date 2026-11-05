package provider

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"

	"github.com/aurimasniekis/terraform-provider-harbor-pilot/internal/client"
)

// conflictDetail explains a 412.
const conflictDetail = "The object was changed outside Terraform since the last refresh, so the update was refused to avoid overwriting that change. " +
	"Run `terraform plan` (which refreshes) to review the difference, then apply again."

// addAPIError converts an API error into diagnostics. Field errors become
// attribute diagnostics; values are never included (only locations and
// messages from the server).
func addAPIError(diags *diag.Diagnostics, summary string, err error) {
	var e *client.Error
	if !errors.As(err, &e) {
		diags.AddError(summary, err.Error())
		return
	}
	switch e.Status {
	case http.StatusPreconditionFailed:
		diags.AddError(summary+": changed outside Terraform since the last refresh", conflictDetail+requestID(e))
		return
	case http.StatusUnauthorized:
		diags.AddError(summary+": not authenticated", "The API token was rejected. Check token, token_file or "+EnvToken+"."+requestID(e))
		return
	case http.StatusForbidden:
		diags.AddError(summary+": permission denied", e.Message+". The service principal needs the permission for this operation."+requestID(e))
		return
	}
	if len(e.Fields) == 0 {
		diags.AddError(summary, e.Error())
		return
	}
	for _, f := range e.Fields {
		attr, detail := fieldPath(f.Location)
		msg := fmt.Sprintf("%s: %s%s", detail, f.Message, requestID(e))
		if attr.Equal(path.Empty()) {
			diags.AddError(summary, msg)
			continue
		}
		diags.AddAttributeError(attr, summary, msg)
	}
}

func requestID(e *client.Error) string {
	if e.RequestID == "" {
		return ""
	}
	return " (request " + e.RequestID + ")"
}

// fieldPath maps an API location (body.spec.members[0].model) to the
// Terraform attribute that carries it and a readable detail.
func fieldPath(loc string) (path.Path, string) {
	l := strings.TrimPrefix(loc, "body.")
	switch {
	case l == "name":
		return path.Root("name"), "name"
	case l == "spec" || strings.HasPrefix(l, "spec.") || strings.HasPrefix(l, "spec["):
		return path.Root("spec"), l
	case l == "document" || strings.HasPrefix(l, "document."):
		return path.Root("document"), l
	case strings.HasPrefix(l, "secrets"):
		return path.Root("secrets_wo"), l
	}
	return path.Empty(), l
}
