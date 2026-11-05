package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/aurimasniekis/terraform-provider-harbor-pilot/internal/client"
)

var (
	_ resource.ResourceWithConfigure   = (*gatewayCredentialResource)(nil)
	_ resource.ResourceWithImportState = (*gatewayCredentialResource)(nil)
)

func newGatewayCredentialResource() resource.Resource { return &gatewayCredentialResource{} }

// gatewayCredentialResource manages gateway credential metadata. The
// token is shown by the server exactly once; it is kept in state only when
// store_token_in_state is true.
type gatewayCredentialResource struct {
	withClient
}

type gatewayCredentialModel struct {
	ID                types.String `tfsdk:"id"`
	BindingID         types.String `tfsdk:"binding_id"`
	Name              types.String `tfsdk:"name"`
	Scopes            types.Set    `tfsdk:"scopes"`
	TTLSeconds        types.Int64  `tfsdk:"ttl_seconds"`
	BoundRefs         types.Map    `tfsdk:"bound_refs"`
	StoreTokenInState types.Bool   `tfsdk:"store_token_in_state"`
	Token             types.String `tfsdk:"token"`
	Prefix            types.String `tfsdk:"prefix"`
	ExpiresAt         types.String `tfsdk:"expires_at"`
	BaseURLs          types.Map    `tfsdk:"base_urls"`
}

func (r *gatewayCredentialResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_gateway_credential"
}

func credentialAttrs() map[string]schema.Attribute {
	rr := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	return map[string]schema.Attribute{
		"id": schema.StringAttribute{Computed: true, MarkdownDescription: "Credential id.",
			PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"binding_id": schema.StringAttribute{Required: true, PlanModifiers: rr, MarkdownDescription: "Client binding id."},
		"name":       schema.StringAttribute{Required: true, PlanModifiers: rr, MarkdownDescription: "Label shown in the UI and activity."},
		"scopes": schema.SetAttribute{ElementType: types.StringType, Optional: true, Computed: true,
			MarkdownDescription: "Scopes. Default: `gateway:invoke`, `gateway:models`.",
			PlanModifiers:       []planmodifier.Set{setplanmodifier.UseStateForUnknown(), setplanmodifier.RequiresReplace()}},
		"ttl_seconds": schema.Int64Attribute{Optional: true, Computed: true, Default: int64default.StaticInt64(0),
			MarkdownDescription: "Lifetime in seconds; `0` = no expiry. An expired credential is removed from state and issued again on the next apply.",
			PlanModifiers:       []planmodifier.Int64{int64planmodifier.RequiresReplace()}},
		"bound_refs": schema.MapAttribute{ElementType: types.StringType, Optional: true,
			MarkdownDescription: "Trusted external references bound at issue time (for example a run id). Requests using the token carry them as trusted context.",
			PlanModifiers:       []planmodifier.Map{mapplanmodifier.RequiresReplace()}},
		"prefix":     schema.StringAttribute{Computed: true, MarkdownDescription: "Non-secret token prefix.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"expires_at": schema.StringAttribute{Computed: true, MarkdownDescription: "Expiry (RFC 3339), null when it never expires.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"base_urls": schema.MapAttribute{ElementType: types.StringType, Computed: true,
			MarkdownDescription: "Gateway base URLs by client protocol.", PlanModifiers: []planmodifier.Map{mapplanmodifier.UseStateForUnknown()}},
	}
}

func (r *gatewayCredentialResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	attrs := credentialAttrs()
	attrs["store_token_in_state"] = schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false),
		MarkdownDescription: "Keep the issued token in Terraform state (`token`). Off by default: state is not a secret store. " +
			"Prefer the `harborpilot_gateway_token` ephemeral resource (Terraform 1.10+) to hand a token to another provider without storing it.",
		PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplace()}}
	attrs["token"] = schema.StringAttribute{Computed: true, Sensitive: true,
		MarkdownDescription: "The bearer token, only when `store_token_in_state = true`; otherwise null.",
		PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()}}
	resp.Schema = schema.Schema{
		MarkdownDescription: "A gateway credential (bearer token) for a client binding. Every argument change issues a new credential and revokes the old one. " +
			"The token itself is not stored unless `store_token_in_state` is set.",
		Attributes: attrs,
	}
}

func (r *gatewayCredentialResource) setMeta(m *gatewayCredentialModel, c *client.Credential) {
	m.ID, m.Prefix, m.Name = types.StringValue(c.ID), types.StringValue(c.Prefix), types.StringValue(c.Name)
	m.Scopes = setOf(c.Scopes)
	m.ExpiresAt = timeString(c.ExpiresAt)
	if len(c.BoundRefs) > 0 {
		m.BoundRefs = mapOf(c.BoundRefs)
	} else if !m.BoundRefs.IsNull() && len(m.BoundRefs.Elements()) > 0 {
		m.BoundRefs = types.MapNull(types.StringType)
	}
}

func (r *gatewayCredentialResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m gatewayCredentialModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	in := client.IssueCredential{Name: m.Name.ValueString(), Scopes: stringSet(ctx, m.Scopes, &resp.Diagnostics),
		TTLSeconds: m.TTLSeconds.ValueInt64(), BoundRefs: stringMap(ctx, m.BoundRefs, &resp.Diagnostics)}
	if resp.Diagnostics.HasError() {
		return
	}
	out, err := r.c.IssueGatewayCredential(ctx, m.BindingID.ValueString(), in)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Cannot issue gateway credential", err)
		return
	}
	r.setMeta(&m, out.Credential)
	m.BaseURLs = mapOf(out.BaseURLs)
	m.Token = types.StringNull()
	if m.StoreTokenInState.ValueBool() {
		m.Token = types.StringValue(out.Token)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *gatewayCredentialResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m gatewayCredentialModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	cs, err := r.c.ListGatewayCredentials(ctx, m.BindingID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "Cannot read gateway credentials", err)
		return
	}
	for i := range cs {
		c := &cs[i]
		if c.ID != m.ID.ValueString() {
			continue
		}
		if c.RevokedAt != nil || expired(c.ExpiresAt) {
			break // revoked or expired outside Terraform: issue a new one
		}
		r.setMeta(&m, c)
		if m.BaseURLs.IsNull() {
			m.BaseURLs = types.MapValueMust(types.StringType, nil)
		}
		resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
		return
	}
	resp.State.RemoveResource(ctx)
}

func (r *gatewayCredentialResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Every argument requires replacement; nothing to do in place.
	var m gatewayCredentialModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *gatewayCredentialResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m gatewayCredentialModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.c.RevokeGatewayCredential(ctx, m.BindingID.ValueString(), m.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		addAPIError(&resp.Diagnostics, "Cannot revoke gateway credential", err)
	}
}

// ImportState takes "<binding_id>/<credential_id>". The token cannot be
// recovered, so an imported credential never has one in state.
func (r *gatewayCredentialResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	binding, id, ok := strings.Cut(req.ID, "/")
	if !ok || binding == "" || id == "" {
		resp.Diagnostics.AddError("Invalid import id", "Use <binding_id>/<credential_id>.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("binding_id"), binding)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("store_token_in_state"), false)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("ttl_seconds"), int64(0))...)
}
