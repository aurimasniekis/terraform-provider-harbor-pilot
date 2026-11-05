package provider

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework-validators/mapvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/aurimasniekis/terraform-provider-harbor-pilot/internal/client"
	"github.com/aurimasniekis/terraform-provider-harbor-pilot/internal/specschema"
)

// privateSpecKey holds the server-canonical spec from the last apply.
const privateSpecKey = "canonical_spec"

var (
	_ resource.ResourceWithConfigure   = (*objectResource)(nil)
	_ resource.ResourceWithImportState = (*objectResource)(nil)
)

// objectResource manages one generic control-plane kind.
type objectResource struct {
	kind Kind
	c    *client.Client
}

type objectModel struct {
	ID         types.String         `tfsdk:"id"`
	Name       types.String         `tfsdk:"name"`
	Spec       jsontypes.Normalized `tfsdk:"spec"`
	Revision   types.Int64          `tfsdk:"revision"`
	ManagedBy  types.String         `tfsdk:"managed_by"`
	DriftState types.String         `tfsdk:"drift_state"`
}

// objectSecretModel is used by kinds that accept write-only secrets.
type objectSecretModel struct {
	objectModel
	SecretsWO        types.Map   `tfsdk:"secrets_wo"`
	SecretsWOVersion types.Int64 `tfsdk:"secrets_wo_version"`
	SecretNames      types.Set   `tfsdk:"secret_names"`
}

type getter interface {
	Get(context.Context, any) diag.Diagnostics
}

type setter interface {
	Set(context.Context, any) diag.Diagnostics
}

func (r *objectResource) hasSecrets() bool { return len(r.kind.Secrets) > 0 }

func (r *objectResource) load(ctx context.Context, src getter) (*objectSecretModel, diag.Diagnostics) {
	m := &objectSecretModel{SecretsWO: types.MapNull(types.StringType), SecretNames: types.SetNull(types.StringType)}
	if r.hasSecrets() {
		return m, src.Get(ctx, m)
	}
	return m, src.Get(ctx, &m.objectModel)
}

func (r *objectResource) save(ctx context.Context, dst setter, m *objectSecretModel) diag.Diagnostics {
	if r.hasSecrets() {
		m.SecretsWO = types.MapNull(types.StringType) // write-only: never persisted
		return dst.Set(ctx, m)
	}
	return dst.Set(ctx, &m.objectModel)
}

func (r *objectResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + r.kind.TypeName
}

func (r *objectResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	attrs := map[string]schema.Attribute{
		"id": schema.StringAttribute{Computed: true, MarkdownDescription: "Stable object id.",
			PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"name": schema.StringAttribute{Required: true, MarkdownDescription: "Unique name (lowercase letters, digits, `.`, `_`, `-`).",
			Validators: []validator.String{stringvalidator.LengthBetween(1, 255)}},
		"spec": schema.StringAttribute{Required: true, CustomType: jsontypes.NormalizedType{},
			MarkdownDescription: "The " + r.kind.Title + " specification as JSON, usually written with `jsonencode({...})`. " +
				"See the `" + r.kind.Schema + "` schema in the Harbor Pilot API reference. Properties you leave out are sent as their zero value " +
				"(empty string, list or map, `0`, `false`), except `enabled`, which must be set."},
		"revision": schema.Int64Attribute{Computed: true, MarkdownDescription: "Server revision, used for optimistic concurrency."},
		"managed_by": schema.StringAttribute{Computed: true,
			MarkdownDescription: "Source of the last change (`terraform` after an apply)."},
		"drift_state": schema.StringAttribute{Computed: true,
			MarkdownDescription: "`drifted` when the object was changed outside Terraform since Terraform last wrote it."},
	}
	if r.hasSecrets() {
		attrs["secrets_wo"] = schema.MapAttribute{ElementType: types.StringType, Optional: true, Sensitive: true, WriteOnly: true,
			MarkdownDescription: "Write-only secret values by name (" + quoteList(r.kind.Secrets) + "). Never stored in state or plan. " +
				"Requires Terraform 1.11 or later. Sent on create and whenever `secrets_wo_version` changes. Prefer secret references in the spec.",
			Validators: []validator.Map{mapvalidator.KeysAre(stringvalidator.OneOf(r.kind.Secrets...))}}
		attrs["secrets_wo_version"] = schema.Int64Attribute{Optional: true,
			MarkdownDescription: "Change this number to send `secrets_wo` again (for example after rotating a secret)."}
		attrs["secret_names"] = schema.SetAttribute{ElementType: types.StringType, Computed: true,
			MarkdownDescription: "Names of the secrets currently set on the server (stored or referenced). Values are never returned."}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: r.kind.About + "\n\nChanges made outside Terraform are detected on refresh and shown as a diff of `spec`.",
		Attributes:          attrs,
	}
}

func quoteList(xs []string) string {
	q := make([]string, len(xs))
	for i, x := range xs {
		q[i] = "`" + x + "`"
	}
	return strings.Join(q, ", ")
}

func (r *objectResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("%T", req.ProviderData))
		return
	}
	r.c = c
}

func (r *objectResource) body(m *objectSecretModel, secrets map[string]*string) (client.WriteObject, error) {
	filled, err := specschema.FillJSON(r.kind.Schema, []byte(m.Spec.ValueString()))
	if err != nil {
		return client.WriteObject{}, err
	}
	return client.WriteObject{Name: m.Name.ValueString(), Spec: filled, Secrets: secrets}, nil
}

// configSecrets reads write-only secrets from the configuration.
func (r *objectResource) configSecrets(ctx context.Context, cfg getAttr, diags *diag.Diagnostics) map[string]*string {
	if !r.hasSecrets() {
		return nil
	}
	var m types.Map
	diags.Append(cfg.GetAttribute(ctx, path.Root("secrets_wo"), &m)...)
	if m.IsNull() || m.IsUnknown() {
		return nil
	}
	out := map[string]*string{}
	for k, v := range m.Elements() {
		s, ok := v.(types.String)
		if !ok || s.IsUnknown() {
			continue
		}
		val := s.ValueString()
		out[k] = &val
	}
	return out
}

type getAttr interface {
	GetAttribute(context.Context, path.Path, any) diag.Diagnostics
}

// apply copies server fields into the model. The user's spec is kept when
// the server's canonical spec matches what Terraform last wrote; otherwise
// the server's spec replaces it so the next plan shows the difference.
func (r *objectResource) apply(ctx context.Context, m *objectSecretModel, o *client.Object, lastCanonical []byte) (canonical string, diags diag.Diagnostics) {
	canonical, err := specschema.Canonical(o.Spec)
	if err != nil {
		diags.AddError("Invalid spec from server", err.Error())
		return "", diags
	}
	if lastCanonical == nil || string(lastCanonical) != canonical || m.Spec.IsNull() || m.Spec.IsUnknown() {
		m.Spec = jsontypes.NewNormalizedValue(canonical)
	}
	m.ID = types.StringValue(o.ID)
	m.Name = types.StringValue(o.Name)
	m.Revision = types.Int64Value(o.Revision)
	m.ManagedBy = types.StringValue(o.ManagedBy)
	m.DriftState = types.StringValue(o.DriftState)
	if r.hasSecrets() {
		names := make([]string, 0, len(o.Secrets))
		for k, s := range o.Secrets {
			if s.Set {
				names = append(names, k)
			}
		}
		sort.Strings(names)
		elems := make([]attr.Value, len(names))
		for i, n := range names {
			elems[i] = types.StringValue(n)
		}
		set, d := types.SetValue(types.StringType, elems)
		diags.Append(d...)
		m.SecretNames = set
	}
	return canonical, diags
}

func (r *objectResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	m, d := r.load(ctx, req.Plan)
	resp.Diagnostics.Append(d...)
	secrets := r.configSecrets(ctx, req.Config, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	body, err := r.body(m, secrets)
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("spec"), "Invalid spec", err.Error())
		return
	}
	o, err := r.c.CreateObject(ctx, r.kind.Plural, body)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Cannot create "+r.kind.Title, err)
		return
	}
	userSpec := m.Spec
	canonical, d := r.apply(ctx, m, o, nil)
	resp.Diagnostics.Append(d...)
	m.Spec = userSpec
	resp.Diagnostics.Append(resp.Private.SetKey(ctx, privateSpecKey, []byte(canonical))...)
	resp.Diagnostics.Append(r.save(ctx, &resp.State, m)...)
}

func (r *objectResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	m, d := r.load(ctx, req.State)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	o, err := r.c.GetObject(ctx, r.kind.Plural, m.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "Cannot read "+r.kind.Title, err)
		return
	}
	last, d := req.Private.GetKey(ctx, privateSpecKey)
	resp.Diagnostics.Append(d...)
	canonical, d := r.apply(ctx, m, o, last)
	resp.Diagnostics.Append(d...)
	if string(last) != canonical {
		resp.Diagnostics.Append(resp.Private.SetKey(ctx, privateSpecKey, []byte(canonical))...)
	}
	resp.Diagnostics.Append(r.save(ctx, &resp.State, m)...)
}

func (r *objectResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	plan, d := r.load(ctx, req.Plan)
	resp.Diagnostics.Append(d...)
	state, d := r.load(ctx, req.State)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	var secrets map[string]*string
	if r.hasSecrets() && !plan.SecretsWOVersion.Equal(state.SecretsWOVersion) {
		secrets = r.configSecrets(ctx, req.Config, &resp.Diagnostics)
	}
	body, err := r.body(plan, secrets)
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("spec"), "Invalid spec", err.Error())
		return
	}
	o, err := r.c.ReplaceObject(ctx, r.kind.Plural, state.ID.ValueString(), state.Revision.ValueInt64(), body)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Cannot update "+r.kind.Title, err)
		return
	}
	userSpec := plan.Spec
	canonical, d := r.apply(ctx, plan, o, nil)
	resp.Diagnostics.Append(d...)
	plan.Spec = userSpec
	resp.Diagnostics.Append(resp.Private.SetKey(ctx, privateSpecKey, []byte(canonical))...)
	resp.Diagnostics.Append(r.save(ctx, &resp.State, plan)...)
}

func (r *objectResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	m, d := r.load(ctx, req.State)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := deleteWithRetry(ctx, func() error {
		return r.c.DeleteObject(ctx, r.kind.Plural, m.ID.ValueString(), m.Revision.ValueInt64())
	})
	if err != nil && !client.IsNotFound(err) {
		addAPIError(&resp.Diagnostics, "Cannot delete "+r.kind.Title, err)
	}
}

// deleteTimeout bounds how long a delete waits for references to go away.
var deleteTimeout = 2 * time.Minute

// deleteWithRetry retries a delete refused because other objects still
// reference this one (409) or because removing it would invalidate a
// published plan (422). Within one destroy, Terraform deletes independent
// objects concurrently, so the referencing object is usually being removed
// at the same time.
func deleteWithRetry(ctx context.Context, del func() error) error {
	deadline := time.Now().Add(deleteTimeout)
	wait := 250 * time.Millisecond
	for {
		err := del()
		if !client.IsReferenced(err) || time.Now().After(deadline) {
			return err
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(wait):
		}
		if wait < 4*time.Second {
			wait *= 2
		}
	}
}

// ImportState accepts an id or a name.
func (r *objectResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	o, err := r.c.GetObject(ctx, r.kind.Plural, req.ID)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Cannot import "+r.kind.Title, err)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), o.ID)...)
}
