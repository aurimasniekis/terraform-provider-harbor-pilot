package provider

import (
	"context"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/aurimasniekis/terraform-provider-harbor-pilot/internal/client"
	"github.com/aurimasniekis/terraform-provider-harbor-pilot/internal/specschema"
)

// Enums the server defines, generated from the vendored OpenAPI document
// (make gen), so they cannot drift from it.
var (
	// permissions a service principal can hold.
	permissions      = specschema.Enum("permissions")
	oidcRoles        = specschema.Enum("oidc_role")
	oidcDefaultRoles = specschema.Enum("oidc_default_role")
	oidcProvisioning = specschema.Enum("oidc_provisioning")
)

// ---------- service principal ----------

func newServicePrincipalResource() resource.Resource { return &servicePrincipalResource{} }

type servicePrincipalResource struct{ withClient }

type servicePrincipalModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Permissions types.Set    `tfsdk:"permissions"`
	Disabled    types.Bool   `tfsdk:"disabled"`
	Revision    types.Int64  `tfsdk:"revision"`
	ManagedBy   types.String `tfsdk:"managed_by"`
}

func (r *servicePrincipalResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_service_principal"
}

func (r *servicePrincipalResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A non-human API principal (for automation such as CI or an orchestrator). Issue its tokens with `harborpilot_api_token`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"name": schema.StringAttribute{Required: true, MarkdownDescription: "Unique name; changing it creates a new principal.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"description": schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("")},
			"permissions": schema.SetAttribute{ElementType: types.StringType, Required: true,
				MarkdownDescription: "Granted permissions: " + quoteList(permissions) + ".",
				Validators:          []validator.Set{setvalidator.ValueStringsAre(stringvalidator.OneOf(permissions...))}},
			"disabled":   schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false)},
			"revision":   schema.Int64Attribute{Computed: true},
			"managed_by": schema.StringAttribute{Computed: true},
		},
	}
}

func (r *servicePrincipalResource) body(m *servicePrincipalModel) client.ServicePrincipalBody {
	return client.ServicePrincipalBody{Name: m.Name.ValueString(), Description: m.Description.ValueString(), Disabled: m.Disabled.ValueBool()}
}

func (r *servicePrincipalResource) set(m *servicePrincipalModel, sp *client.ServicePrincipal) {
	m.ID, m.Name, m.Description = types.StringValue(sp.ID), types.StringValue(sp.Name), types.StringValue(sp.Description)
	perms := append([]string(nil), sp.Permissions...)
	sort.Strings(perms)
	m.Permissions = setOf(perms)
	m.Disabled, m.Revision, m.ManagedBy = types.BoolValue(sp.Disabled), types.Int64Value(sp.Revision), types.StringValue(sp.ManagedBy)
}

func (r *servicePrincipalResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m servicePrincipalModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	b := r.body(&m)
	b.Permissions = stringSet(ctx, m.Permissions, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	sp, err := r.c.CreateServicePrincipal(ctx, b)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Cannot create service principal", err)
		return
	}
	r.set(&m, sp)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *servicePrincipalResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m servicePrincipalModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	sp, err := r.c.GetServicePrincipal(ctx, m.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "Cannot read service principal", err)
		return
	}
	r.set(&m, sp)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *servicePrincipalResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state servicePrincipalModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	b := r.body(&plan)
	b.Permissions = stringSet(ctx, plan.Permissions, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	sp, err := r.c.ReplaceServicePrincipal(ctx, state.ID.ValueString(), state.Revision.ValueInt64(), b)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Cannot update service principal", err)
		return
	}
	r.set(&plan, sp)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *servicePrincipalResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m servicePrincipalModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if err := r.c.DeleteServicePrincipal(ctx, m.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		addAPIError(&resp.Diagnostics, "Cannot delete service principal", err)
	}
}

func (r *servicePrincipalResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// ---------- API token ----------

func newAPITokenResource() resource.Resource { return &apiTokenResource{} }

type apiTokenResource struct{ withClient }

type apiTokenModel struct {
	ID                 types.String `tfsdk:"id"`
	ServicePrincipalID types.String `tfsdk:"service_principal_id"`
	Name               types.String `tfsdk:"name"`
	TTLSeconds         types.Int64  `tfsdk:"ttl_seconds"`
	StoreTokenInState  types.Bool   `tfsdk:"store_token_in_state"`
	Token              types.String `tfsdk:"token"`
	Prefix             types.String `tfsdk:"prefix"`
	ExpiresAt          types.String `tfsdk:"expires_at"`
}

func (r *apiTokenResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_api_token"
}

func (r *apiTokenResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	rr := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	keep := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "A control-plane API token for a service principal. Every argument change issues a new token and revokes the old one. " +
			"The plaintext is kept in state only with `store_token_in_state = true`.",
		Attributes: map[string]schema.Attribute{
			"id":                   schema.StringAttribute{Computed: true, PlanModifiers: keep},
			"service_principal_id": schema.StringAttribute{Required: true, PlanModifiers: rr},
			"name":                 schema.StringAttribute{Required: true, PlanModifiers: rr},
			"ttl_seconds": schema.Int64Attribute{Optional: true, Computed: true, Default: int64default.StaticInt64(0),
				MarkdownDescription: "Lifetime in seconds; `0` = no expiry. Expired tokens are removed from state and issued again.",
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.RequiresReplace()}},
			"store_token_in_state": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false),
				MarkdownDescription: "Keep the plaintext token in state (`token`). Off by default.",
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.RequiresReplace()}},
			"token":      schema.StringAttribute{Computed: true, Sensitive: true, PlanModifiers: keep, MarkdownDescription: "Plaintext token when `store_token_in_state` is true."},
			"prefix":     schema.StringAttribute{Computed: true, PlanModifiers: keep},
			"expires_at": schema.StringAttribute{Computed: true, PlanModifiers: keep},
		},
	}
}

func (r *apiTokenResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m apiTokenModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	t, plain, err := r.c.IssueAPIToken(ctx, m.ServicePrincipalID.ValueString(), m.Name.ValueString(), m.TTLSeconds.ValueInt64())
	if err != nil {
		addAPIError(&resp.Diagnostics, "Cannot issue API token", err)
		return
	}
	m.ID, m.Prefix, m.ExpiresAt = types.StringValue(t.ID), types.StringValue(t.Prefix), timeString(t.ExpiresAt)
	m.Token = types.StringNull()
	if m.StoreTokenInState.ValueBool() {
		m.Token = types.StringValue(plain)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *apiTokenResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m apiTokenModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ts, err := r.c.ListAPITokens(ctx, m.ServicePrincipalID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "Cannot read API tokens", err)
		return
	}
	for _, t := range ts {
		if t.ID == m.ID.ValueString() && t.RevokedAt == nil && !expired(t.ExpiresAt) {
			m.Name, m.Prefix, m.ExpiresAt = types.StringValue(t.Name), types.StringValue(t.Prefix), timeString(t.ExpiresAt)
			resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
			return
		}
	}
	resp.State.RemoveResource(ctx)
}

func (r *apiTokenResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m apiTokenModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *apiTokenResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m apiTokenModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if err := r.c.RevokeAPIToken(ctx, m.ServicePrincipalID.ValueString(), m.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		addAPIError(&resp.Diagnostics, "Cannot revoke API token", err)
	}
}

// ---------- auth settings (singleton) ----------

func newAuthSettingsResource() resource.Resource { return &authSettingsResource{} }

type authSettingsResource struct{ withClient }

type authSettingsModel struct {
	ID                     types.String `tfsdk:"id"`
	PasswordLogin          types.Bool   `tfsdk:"password_login"`
	IdleTimeoutSeconds     types.Int64  `tfsdk:"idle_timeout_seconds"`
	AbsoluteTimeoutSeconds types.Int64  `tfsdk:"absolute_timeout_seconds"`
	Revision               types.Int64  `tfsdk:"revision"`
}

const authSettingsID = "auth_settings"

func (r *authSettingsResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_auth_settings"
}

func (r *authSettingsResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The server's authentication settings (a singleton). Creating it adopts the current settings; " +
			"destroying it only removes it from state and leaves the settings as they are. " +
			"Disabling password sign-in requires an enabled SSO provider.",
		Attributes: map[string]schema.Attribute{
			"id":                       schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"password_login":           schema.BoolAttribute{Required: true, MarkdownDescription: "Allow local password sign-in."},
			"idle_timeout_seconds":     schema.Int64Attribute{Required: true, MarkdownDescription: "Session idle timeout (300–86400)."},
			"absolute_timeout_seconds": schema.Int64Attribute{Required: true, MarkdownDescription: "Session absolute lifetime (300–2592000)."},
			"revision":                 schema.Int64Attribute{Computed: true},
		},
	}
}

func (r *authSettingsResource) put(ctx context.Context, m *authSettingsModel, rev int64, diags *resourceDiags) {
	s, err := r.c.PutAuthSettings(ctx, rev, client.AuthSettings{PasswordLogin: m.PasswordLogin.ValueBool(),
		IdleTimeoutSeconds: m.IdleTimeoutSeconds.ValueInt64(), AbsoluteTimeoutSeconds: m.AbsoluteTimeoutSeconds.ValueInt64()})
	if err != nil {
		addAPIError(diags.d, "Cannot update authentication settings", err)
		return
	}
	r.set(m, s)
}

func (r *authSettingsResource) set(m *authSettingsModel, s *client.AuthSettings) {
	m.ID = types.StringValue(authSettingsID)
	m.PasswordLogin, m.IdleTimeoutSeconds = types.BoolValue(s.PasswordLogin), types.Int64Value(s.IdleTimeoutSeconds)
	m.AbsoluteTimeoutSeconds, m.Revision = types.Int64Value(s.AbsoluteTimeoutSeconds), types.Int64Value(s.Revision)
}

func (r *authSettingsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m authSettingsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	cur, err := r.c.GetAuthSettings(ctx)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Cannot read authentication settings", err)
		return
	}
	r.put(ctx, &m, cur.Revision, &resourceDiags{&resp.Diagnostics})
	if !resp.Diagnostics.HasError() {
		resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
	}
}

func (r *authSettingsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m authSettingsModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	s, err := r.c.GetAuthSettings(ctx)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Cannot read authentication settings", err)
		return
	}
	r.set(&m, s)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *authSettingsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state authSettingsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	r.put(ctx, &plan, state.Revision.ValueInt64(), &resourceDiags{&resp.Diagnostics})
	if !resp.Diagnostics.HasError() {
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	}
}

func (r *authSettingsResource) Delete(context.Context, resource.DeleteRequest, *resource.DeleteResponse) {
}

func (r *authSettingsResource) ImportState(ctx context.Context, _ resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), authSettingsID)...)
}

// ---------- OIDC provider ----------

func newOIDCProviderResource() resource.Resource { return &oidcProviderResource{} }

type oidcProviderResource struct{ withClient }

type roleMappingModel struct {
	Group types.String `tfsdk:"group"`
	Role  types.String `tfsdk:"role"`
}

type oidcProviderModel struct {
	ID                    types.String       `tfsdk:"id"`
	Slug                  types.String       `tfsdk:"slug"`
	DisplayName           types.String       `tfsdk:"display_name"`
	Issuer                types.String       `tfsdk:"issuer"`
	ClientID              types.String       `tfsdk:"client_id"`
	ClientSecretRef       types.String       `tfsdk:"client_secret_ref"`
	ClientSecretWO        types.String       `tfsdk:"client_secret_wo"`
	ClientSecretWOVersion types.Int64        `tfsdk:"client_secret_wo_version"`
	ClientSecretSet       types.Bool         `tfsdk:"client_secret_set"`
	Scopes                types.List         `tfsdk:"scopes"`
	GroupsClaim           types.String       `tfsdk:"groups_claim"`
	RoleMappings          []roleMappingModel `tfsdk:"role_mappings"`
	DefaultRole           types.String       `tfsdk:"default_role"`
	Provisioning          types.String       `tfsdk:"provisioning"`
	AllowedDomains        types.Set          `tfsdk:"allowed_domains"`
	AllowedAlgs           types.List         `tfsdk:"allowed_algs"`
	AllowPrivateNetwork   types.Bool         `tfsdk:"allow_private_network"`
	Enabled               types.Bool         `tfsdk:"enabled"`
	RedirectURI           types.String       `tfsdk:"redirect_uri"`
	Revision              types.Int64        `tfsdk:"revision"`
}

func (r *oidcProviderResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_oidc_provider"
}

func (r *oidcProviderResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	roles := []validator.String{stringvalidator.OneOf(oidcRoles...)}
	resp.Schema = schema.Schema{
		MarkdownDescription: "An OpenID Connect single sign-on provider. Register `redirect_uri` with the identity provider. " +
			"Give the client secret as a reference (`client_secret_ref`) or write-only (`client_secret_wo`, Terraform 1.11+); it is never stored in state.",
		Attributes: map[string]schema.Attribute{
			"id":           schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"slug":         schema.StringAttribute{Required: true, MarkdownDescription: "URL slug used in sign-in links."},
			"display_name": schema.StringAttribute{Required: true},
			"issuer":       schema.StringAttribute{Required: true, MarkdownDescription: "Issuer URL (https; http only with `allow_private_network`)."},
			"client_id":    schema.StringAttribute{Required: true},
			"client_secret_ref": schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString(""),
				MarkdownDescription: "`env:NAME` or `file:/path` resolved by the server. Env variables must start with an allowed prefix (default `HP_SECRET_`)."},
			"client_secret_wo": schema.StringAttribute{Optional: true, Sensitive: true, WriteOnly: true,
				MarkdownDescription: "Write-only client secret (Terraform 1.11+). Sent on create and when `client_secret_wo_version` changes."},
			"client_secret_wo_version": schema.Int64Attribute{Optional: true, MarkdownDescription: "Change to send `client_secret_wo` again."},
			"client_secret_set":        schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether a stored client secret exists."},
			"scopes": schema.ListAttribute{ElementType: types.StringType, Optional: true, Computed: true,
				MarkdownDescription: "Requested scopes (default `openid`, `profile`, `email`). Add `groups` when group claims need it.",
				PlanModifiers:       []planmodifier.List{listplanmodifier.UseStateForUnknown()}},
			"groups_claim": schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString(""),
				MarkdownDescription: "Dotted path of the groups claim, e.g. `groups`."},
			"role_mappings": schema.ListNestedAttribute{Optional: true, MarkdownDescription: "Group to role mappings, first match wins.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"group": schema.StringAttribute{Required: true},
					"role":  schema.StringAttribute{Required: true, Validators: roles},
				}}},
			"default_role": schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString(""),
				Validators: []validator.String{stringvalidator.OneOf(oidcDefaultRoles...)}},
			"provisioning": schema.StringAttribute{Required: true, Validators: []validator.String{stringvalidator.OneOf(oidcProvisioning...)},
				MarkdownDescription: "`preprovisioned` (users must exist) or `jit` (created on first sign-in; needs `allowed_domains` or `role_mappings`)."},
			"allowed_domains": schema.SetAttribute{ElementType: types.StringType, Optional: true},
			"allowed_algs": schema.ListAttribute{ElementType: types.StringType, Optional: true, Computed: true,
				MarkdownDescription: "Accepted ID token algorithms (default ES256, RS256, EdDSA).",
				PlanModifiers:       []planmodifier.List{listplanmodifier.UseStateForUnknown()}},
			"allow_private_network": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false)},
			"enabled":               schema.BoolAttribute{Required: true},
			"redirect_uri":          schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"revision":              schema.Int64Attribute{Computed: true},
		},
	}
}

func (r *oidcProviderResource) body(ctx context.Context, m *oidcProviderModel, d *resourceDiags) client.OIDCProviderBody {
	b := client.OIDCProviderBody{Slug: m.Slug.ValueString(), DisplayName: m.DisplayName.ValueString(), Issuer: m.Issuer.ValueString(),
		ClientID: m.ClientID.ValueString(), ClientSecretRef: m.ClientSecretRef.ValueString(), Scopes: stringList(ctx, m.Scopes, d.d),
		GroupsClaim: m.GroupsClaim.ValueString(), DefaultRole: m.DefaultRole.ValueString(), Provisioning: m.Provisioning.ValueString(),
		AllowedDomains: stringSet(ctx, m.AllowedDomains, d.d), AllowedAlgs: stringList(ctx, m.AllowedAlgs, d.d),
		AllowPrivateNetwork: m.AllowPrivateNetwork.ValueBool(), Enabled: m.Enabled.ValueBool()}
	for _, rm := range m.RoleMappings {
		b.RoleMappings = append(b.RoleMappings, client.RoleMapping{Group: rm.Group.ValueString(), Role: rm.Role.ValueString()})
	}
	return b
}

func (r *oidcProviderResource) set(m *oidcProviderModel, p *client.OIDCProvider) {
	m.ID, m.Slug, m.DisplayName = types.StringValue(p.ID), types.StringValue(p.Slug), types.StringValue(p.DisplayName)
	m.Issuer, m.ClientID, m.ClientSecretRef = types.StringValue(p.Issuer), types.StringValue(p.ClientID), types.StringValue(p.ClientSecretRef)
	m.ClientSecretSet, m.Scopes, m.GroupsClaim = types.BoolValue(p.ClientSecretSet), listOf(p.Scopes), types.StringValue(p.GroupsClaim)
	m.DefaultRole, m.Provisioning = types.StringValue(p.DefaultRole), types.StringValue(p.Provisioning)
	if len(p.AllowedDomains) > 0 || !m.AllowedDomains.IsNull() {
		m.AllowedDomains = setOf(p.AllowedDomains)
	}
	m.AllowedAlgs, m.AllowPrivateNetwork, m.Enabled = listOf(p.AllowedAlgs), types.BoolValue(p.AllowPrivateNetwork), types.BoolValue(p.Enabled)
	m.RedirectURI, m.Revision = types.StringValue(p.RedirectURI), types.Int64Value(p.Revision)
	if len(p.RoleMappings) > 0 || m.RoleMappings != nil {
		m.RoleMappings = make([]roleMappingModel, 0, len(p.RoleMappings))
		for _, rm := range p.RoleMappings {
			m.RoleMappings = append(m.RoleMappings, roleMappingModel{Group: types.StringValue(rm.Group), Role: types.StringValue(rm.Role)})
		}
	}
	m.ClientSecretWO = types.StringNull()
}

func (r *oidcProviderResource) secret(ctx context.Context, cfg getAttr, d *resourceDiags) *string {
	var s types.String
	d.d.Append(cfg.GetAttribute(ctx, path.Root("client_secret_wo"), &s)...)
	if s.IsNull() || s.IsUnknown() {
		return nil
	}
	v := s.ValueString()
	return &v
}

func (r *oidcProviderResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m oidcProviderModel
	d := &resourceDiags{&resp.Diagnostics}
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	b := r.body(ctx, &m, d)
	b.ClientSecret = r.secret(ctx, req.Config, d)
	if resp.Diagnostics.HasError() {
		return
	}
	p, err := r.c.CreateOIDCProvider(ctx, b)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Cannot create SSO provider", err)
		return
	}
	r.set(&m, p)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *oidcProviderResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m oidcProviderModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	p, err := r.c.GetOIDCProvider(ctx, m.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "Cannot read SSO provider", err)
		return
	}
	r.set(&m, p)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *oidcProviderResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state oidcProviderModel
	d := &resourceDiags{&resp.Diagnostics}
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	b := r.body(ctx, &plan, d)
	if !plan.ClientSecretWOVersion.Equal(state.ClientSecretWOVersion) {
		b.ClientSecret = r.secret(ctx, req.Config, d)
	}
	if resp.Diagnostics.HasError() {
		return
	}
	p, err := r.c.ReplaceOIDCProvider(ctx, state.ID.ValueString(), state.Revision.ValueInt64(), b)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Cannot update SSO provider", err)
		return
	}
	r.set(&plan, p)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *oidcProviderResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m oidcProviderModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if err := r.c.DeleteOIDCProvider(ctx, m.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		addAPIError(&resp.Diagnostics, "Cannot delete SSO provider", err)
	}
}

func (r *oidcProviderResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// resourceDiags wraps a diagnostics pointer for helpers.
type resourceDiags struct{ d *diag.Diagnostics }
