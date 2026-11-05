package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework/ephemeral"
	"github.com/hashicorp/terraform-plugin-framework/ephemeral/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/aurimasniekis/terraform-provider-harbor-pilot/internal/client"
)

var (
	_ ephemeral.EphemeralResourceWithConfigure = (*gatewayTokenEphemeral)(nil)
	_ ephemeral.EphemeralResourceWithClose     = (*gatewayTokenEphemeral)(nil)
)

func newGatewayTokenEphemeral() ephemeral.EphemeralResource { return &gatewayTokenEphemeral{} }

// gatewayTokenEphemeral issues a short-lived gateway credential for the
// duration of one Terraform run and revokes it when Terraform closes it.
// The token never enters state or plan files.
type gatewayTokenEphemeral struct {
	c *client.Client
}

type gatewayTokenModel struct {
	BindingID    types.String `tfsdk:"binding_id"`
	Name         types.String `tfsdk:"name"`
	Scopes       types.Set    `tfsdk:"scopes"`
	TTLSeconds   types.Int64  `tfsdk:"ttl_seconds"`
	BoundRefs    types.Map    `tfsdk:"bound_refs"`
	Token        types.String `tfsdk:"token"`
	CredentialID types.String `tfsdk:"credential_id"`
	Prefix       types.String `tfsdk:"prefix"`
	ExpiresAt    types.String `tfsdk:"expires_at"`
	BaseURLs     types.Map    `tfsdk:"base_urls"`
}

type ephemeralPrivate struct {
	BindingID    string `json:"binding_id"`
	CredentialID string `json:"credential_id"`
}

const ephemeralKey = "credential"

func (e *gatewayTokenEphemeral) Metadata(_ context.Context, req ephemeral.MetadataRequest, resp *ephemeral.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_gateway_token"
}

func (e *gatewayTokenEphemeral) Schema(_ context.Context, _ ephemeral.SchemaRequest, resp *ephemeral.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Issues a short-lived gateway token for a client binding while Terraform runs and revokes it afterwards. " +
			"Requires Terraform 1.10 or later. Use it to pass a token to another provider or a write-only argument without storing it in state.",
		Attributes: map[string]schema.Attribute{
			"binding_id": schema.StringAttribute{Required: true, MarkdownDescription: "Client binding id."},
			"name":       schema.StringAttribute{Optional: true, MarkdownDescription: "Label (default `terraform-ephemeral`)."},
			"scopes": schema.SetAttribute{ElementType: types.StringType, Optional: true,
				MarkdownDescription: "Scopes. Default: `gateway:invoke`, `gateway:models`."},
			"ttl_seconds": schema.Int64Attribute{Optional: true,
				MarkdownDescription: "Lifetime in seconds (default 3600, at most 86400). The token is also revoked when Terraform closes it.",
				Validators:          []validator.Int64{int64validator.Between(60, 86400)}},
			"bound_refs": schema.MapAttribute{ElementType: types.StringType, Optional: true,
				MarkdownDescription: "Trusted external references bound to the token."},
			"token":         schema.StringAttribute{Computed: true, Sensitive: true, MarkdownDescription: "The bearer token."},
			"credential_id": schema.StringAttribute{Computed: true},
			"prefix":        schema.StringAttribute{Computed: true},
			"expires_at":    schema.StringAttribute{Computed: true},
			"base_urls":     schema.MapAttribute{ElementType: types.StringType, Computed: true},
		},
	}
}

func (e *gatewayTokenEphemeral) Configure(_ context.Context, req ephemeral.ConfigureRequest, resp *ephemeral.ConfigureResponse) {
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

func (e *gatewayTokenEphemeral) Open(ctx context.Context, req ephemeral.OpenRequest, resp *ephemeral.OpenResponse) {
	var m gatewayTokenModel
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
	in := client.IssueCredential{Name: name, Scopes: stringSet(ctx, m.Scopes, &resp.Diagnostics), TTLSeconds: ttl,
		BoundRefs: stringMap(ctx, m.BoundRefs, &resp.Diagnostics)}
	if resp.Diagnostics.HasError() {
		return
	}
	out, err := e.c.IssueGatewayCredential(ctx, m.BindingID.ValueString(), in)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Cannot issue gateway token", err)
		return
	}
	m.Token = types.StringValue(out.Token)
	m.CredentialID = types.StringValue(out.Credential.ID)
	m.Prefix = types.StringValue(out.Credential.Prefix)
	m.ExpiresAt = timeString(out.Credential.ExpiresAt)
	m.BaseURLs = mapOf(out.BaseURLs)
	resp.Diagnostics.Append(resp.Result.Set(ctx, &m)...)
	priv, _ := json.Marshal(ephemeralPrivate{BindingID: m.BindingID.ValueString(), CredentialID: out.Credential.ID})
	resp.Diagnostics.Append(resp.Private.SetKey(ctx, ephemeralKey, priv)...)
}

func (e *gatewayTokenEphemeral) Close(ctx context.Context, req ephemeral.CloseRequest, resp *ephemeral.CloseResponse) {
	raw, d := req.Private.GetKey(ctx, ephemeralKey)
	resp.Diagnostics.Append(d...)
	if raw == nil {
		return
	}
	var p ephemeralPrivate
	if err := json.Unmarshal(raw, &p); err != nil {
		resp.Diagnostics.AddError("Invalid private data", err.Error())
		return
	}
	if err := e.c.RevokeGatewayCredential(ctx, p.BindingID, p.CredentialID); err != nil && !client.IsNotFound(err) {
		addAPIError(&resp.Diagnostics, "Cannot revoke gateway token", err)
	}
}
