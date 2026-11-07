package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework/ephemeral"
	"github.com/hashicorp/terraform-plugin-framework/ephemeral/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/aurimasniekis/terraform-provider-harbor-pilot/internal/client"
)

var (
	_ ephemeral.EphemeralResourceWithConfigure = (*servicePrincipalTokenEphemeral)(nil)
	_ ephemeral.EphemeralResourceWithClose     = (*servicePrincipalTokenEphemeral)(nil)
)

func newServicePrincipalTokenEphemeral() ephemeral.EphemeralResource {
	return &servicePrincipalTokenEphemeral{}
}

// servicePrincipalTokenEphemeral issues a control-plane API token (hpa_…)
// for a service principal while Terraform runs, without storing it in state.
// The token carries the principal's permissions.
type servicePrincipalTokenEphemeral struct {
	c *client.Client
}

type servicePrincipalTokenModel struct {
	ServicePrincipalID types.String `tfsdk:"service_principal_id"`
	Name               types.String `tfsdk:"name"`
	TTLSeconds         types.Int64  `tfsdk:"ttl_seconds"`
	RevokeOnClose      types.Bool   `tfsdk:"revoke_on_close"`
	Token              types.String `tfsdk:"token"`
	TokenID            types.String `tfsdk:"token_id"`
	Prefix             types.String `tfsdk:"prefix"`
	ExpiresAt          types.String `tfsdk:"expires_at"`
	Permissions        types.Set    `tfsdk:"permissions"`
}

type servicePrincipalTokenPrivate struct {
	ServicePrincipalID string `json:"service_principal_id"`
	TokenID            string `json:"token_id"`
}

const servicePrincipalTokenKey = "api_token"

func (e *servicePrincipalTokenEphemeral) Metadata(_ context.Context, req ephemeral.MetadataRequest, resp *ephemeral.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_service_principal_token"
}

func (e *servicePrincipalTokenEphemeral) Schema(_ context.Context, _ ephemeral.SchemaRequest, resp *ephemeral.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Issues a control-plane API token (`hpa_…`) for a service principal while Terraform runs and, by default, revokes it afterwards. " +
			"Requires Terraform 1.10 or later. The token is never stored in state or plan files; pass it to another provider or a write-only argument.\n\n" +
			"A token carries its service principal's permissions (for example `usage.read` for `GET /api/v1/budgets/status`, " +
			"`config.read`, `activity.read`); grant them on `harborpilot_service_principal`. " +
			"Terraform opens an ephemeral resource on every plan and apply, so each run issues a new token; " +
			"with `revoke_on_close = false` earlier tokens stay valid until `ttl_seconds` passes.",
		Attributes: map[string]schema.Attribute{
			"service_principal_id": schema.StringAttribute{Required: true, MarkdownDescription: "Service principal id."},
			"name":                 schema.StringAttribute{Optional: true, MarkdownDescription: "Label (default `terraform-ephemeral`)."},
			"ttl_seconds": schema.Int64Attribute{Optional: true,
				MarkdownDescription: "Lifetime in seconds (default 3600, at most 63072000, two years).",
				Validators:          []validator.Int64{int64validator.Between(60, maxTokenTTL)}},
			"revoke_on_close": schema.BoolAttribute{Optional: true,
				MarkdownDescription: "Revoke the token when Terraform closes the ephemeral resource (default `true`). " +
					"Set to `false` to keep it valid until `ttl_seconds` passes, for a token another system keeps."},
			"token":       schema.StringAttribute{Computed: true, Sensitive: true, MarkdownDescription: "The bearer token."},
			"token_id":    schema.StringAttribute{Computed: true},
			"prefix":      schema.StringAttribute{Computed: true},
			"expires_at":  schema.StringAttribute{Computed: true},
			"permissions": schema.SetAttribute{ElementType: types.StringType, Computed: true, MarkdownDescription: "Permissions the token carries (the service principal's)."},
		},
	}
}

func (e *servicePrincipalTokenEphemeral) Configure(_ context.Context, req ephemeral.ConfigureRequest, resp *ephemeral.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("%T", req.ProviderData))
		return
	}
	e.c = c
}

func (e *servicePrincipalTokenEphemeral) Open(ctx context.Context, req ephemeral.OpenRequest, resp *ephemeral.OpenResponse) {
	var m servicePrincipalTokenModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	name := m.Name.ValueString()
	if name == "" {
		name = "terraform-ephemeral"
	}
	ttl := m.TTLSeconds.ValueInt64()
	if ttl == 0 {
		ttl = 3600
	}
	spID := m.ServicePrincipalID.ValueString()
	sp, err := e.c.GetServicePrincipal(ctx, spID)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Cannot read service principal", err)
		return
	}
	t, plain, err := e.c.IssueAPIToken(ctx, spID, name, ttl)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Cannot issue API token", err)
		return
	}
	perms := append([]string(nil), sp.Permissions...)
	sort.Strings(perms)
	m.Token = types.StringValue(plain)
	m.TokenID = types.StringValue(t.ID)
	m.Prefix = types.StringValue(t.Prefix)
	m.ExpiresAt = timeString(t.ExpiresAt)
	m.Permissions = setOf(perms)
	resp.Diagnostics.Append(resp.Result.Set(ctx, &m)...)
	if !revokeOnClose(m.RevokeOnClose) {
		return
	}
	priv, _ := json.Marshal(servicePrincipalTokenPrivate{ServicePrincipalID: spID, TokenID: t.ID})
	resp.Diagnostics.Append(resp.Private.SetKey(ctx, servicePrincipalTokenKey, priv)...)
}

func (e *servicePrincipalTokenEphemeral) Close(ctx context.Context, req ephemeral.CloseRequest, resp *ephemeral.CloseResponse) {
	raw, d := req.Private.GetKey(ctx, servicePrincipalTokenKey)
	resp.Diagnostics.Append(d...)
	if raw == nil {
		return
	}
	var p servicePrincipalTokenPrivate
	if err := json.Unmarshal(raw, &p); err != nil {
		resp.Diagnostics.AddError("Invalid private data", err.Error())
		return
	}
	if err := e.c.RevokeAPIToken(ctx, p.ServicePrincipalID, p.TokenID); err != nil && !client.IsNotFound(err) {
		addAPIError(&resp.Diagnostics, "Cannot revoke API token", err)
	}
}
