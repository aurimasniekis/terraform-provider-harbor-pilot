package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/aurimasniekis/terraform-provider-harbor-pilot/internal/client"
	"github.com/aurimasniekis/terraform-provider-harbor-pilot/internal/specschema"
)

var (
	_ resource.ResourceWithConfigure   = (*planVersionResource)(nil)
	_ resource.ResourceWithImportState = (*planVersionResource)(nil)
)

func newPlanVersionResource() resource.Resource { return &planVersionResource{} }

// planVersionResource manages one version of a routing plan's routing
// logic. Published versions are immutable, so a changed document creates a
// new version; destroying a published version only removes it from state.
type planVersionResource struct {
	withClient
}

type planVersionModel struct {
	ID       types.String         `tfsdk:"id"`
	PlanID   types.String         `tfsdk:"plan_id"`
	Document jsontypes.Normalized `tfsdk:"document"`
	Note     types.String         `tfsdk:"note"`
	Publish  types.Bool           `tfsdk:"publish"`
	Version  types.Int64          `tfsdk:"version"`
	Status   types.String         `tfsdk:"status"`
}

func (r *planVersionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_routing_plan_version"
}

func (r *planVersionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A version of a routing plan's routing logic (model mappings, restrictions, fallback, privacy, compatibility, " +
			"intent and engine rules).\n\n" +
			"With `publish = true` (the default) the version is validated and published, superseding the previous one. " +
			"Published versions are immutable: changing `document` or `note` creates and publishes a new version. " +
			"If another version is published outside Terraform, the next plan shows `publish` changing back to `true`, " +
			"and applying it publishes a copy of this version (a rollback). " +
			"Destroying a published version leaves it in the plan's history; destroying a draft discards it.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, MarkdownDescription: "Version id (`pvr_…`)."},
			"plan_id": schema.StringAttribute{Required: true, MarkdownDescription: "Routing plan id.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"document": schema.StringAttribute{Required: true, CustomType: jsontypes.NormalizedType{},
				MarkdownDescription: "The plan document as JSON (`PlanDocument` in the API reference), usually written with `jsonencode`. " +
					"Omitted properties get the server's defaults: an omitted `fallback.steps` means the default fallback " +
					"(`alternate_account`, `alternate_model`, `stop`), `[]` means no fallback, and an omitted or `0` `fallback.max_attempts` means 3.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"note": schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString(""),
				MarkdownDescription: "Change note recorded with the version.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"publish": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(true),
				MarkdownDescription: "Publish the version (default `true`). `false` keeps it as a draft.",
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplaceIf(func(_ context.Context, req planmodifier.BoolRequest, resp *boolplanmodifier.RequiresReplaceIfFuncResponse) {
					// Unpublishing is not possible: switching to a draft creates a new one.
					resp.RequiresReplace = req.StateValue.ValueBool() && !req.PlanValue.ValueBool() && !req.PlanValue.IsUnknown()
				}, "Switching a published version to a draft creates a new draft.", "Switching a published version to a draft creates a new draft.")}},
			"version": schema.Int64Attribute{Computed: true, MarkdownDescription: "Version number within the plan."},
			"status":  schema.StringAttribute{Computed: true, MarkdownDescription: "`draft`, `published`, `superseded` or `archived`."},
		},
	}
}

func (r *planVersionResource) fill(m *planVersionModel) (client.PlanVersion, error) {
	doc, err := specschema.FillJSON("PlanDocument", []byte(m.Document.ValueString()))
	return client.PlanVersion{Document: doc, Note: m.Note.ValueString()}, err
}

func (r *planVersionResource) set(m *planVersionModel, v *client.PlanVersion) {
	m.ID = types.StringValue(v.ID)
	if m.PlanID.IsNull() || m.PlanID.IsUnknown() {
		m.PlanID = types.StringValue(v.PlanID)
	}
	m.Version, m.Status = types.Int64Value(v.Version), types.StringValue(v.Status)
	// A rollback publishes a copy with its own note; the configured note
	// stays authoritative.
	if m.Note.IsNull() || m.Note.IsUnknown() {
		m.Note = types.StringValue(v.Note)
	}
}

func (r *planVersionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m planVersionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	in, err := r.fill(&m)
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("document"), "Invalid document", err.Error())
		return
	}
	planID := m.PlanID.ValueString()
	v, err := r.c.CreateDraft(ctx, planID, in.Document, in.Note)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Cannot create routing plan version", err)
		return
	}
	if m.Publish.ValueBool() {
		pv, err := r.c.PublishVersion(ctx, planID, v.ID)
		if err != nil {
			// Do not leave an orphaned draft behind.
			_ = r.c.DiscardDraft(ctx, planID, v.ID)
			addAPIError(&resp.Diagnostics, "Cannot publish routing plan version", err)
			return
		}
		v = pv
	}
	r.set(&m, v)
	if c, err := specschema.Canonical(v.Document); err == nil {
		resp.Diagnostics.Append(resp.Private.SetKey(ctx, privateSpecKey, []byte(c))...)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *planVersionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m planVersionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	v, err := r.c.GetVersion(ctx, m.PlanID.ValueString(), m.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "Cannot read routing plan version", err)
		return
	}
	canonical, err := specschema.Canonical(v.Document)
	if err != nil {
		resp.Diagnostics.AddError("Invalid document from server", err.Error())
		return
	}
	last, d := req.Private.GetKey(ctx, privateSpecKey)
	resp.Diagnostics.Append(d...)
	if last == nil || string(last) != canonical || m.Document.IsNull() {
		// Imported, or a draft edited outside Terraform.
		m.Document = jsontypes.NewNormalizedValue(canonical)
		resp.Diagnostics.Append(resp.Private.SetKey(ctx, privateSpecKey, []byte(canonical))...)
	}
	r.set(&m, v)
	// A version published by Terraform but no longer current shows as an
	// unpublished one, so the plan offers to publish it again.
	m.Publish = types.BoolValue(v.Status == "published")
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

// Update runs only when publish switches from false to true.
func (r *planVersionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state planVersionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	planID, id := state.PlanID.ValueString(), state.ID.ValueString()
	cur, err := r.c.GetVersion(ctx, planID, id)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Cannot read routing plan version", err)
		return
	}
	v := cur
	if plan.Publish.ValueBool() {
		switch cur.Status {
		case "draft":
			v, err = r.c.PublishVersion(ctx, planID, id)
		case "published":
		default:
			v, err = r.c.Rollback(ctx, planID, id)
		}
		if err != nil {
			addAPIError(&resp.Diagnostics, "Cannot publish routing plan version", err)
			return
		}
	}
	plan.Document = state.Document
	r.set(&plan, v)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *planVersionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m planVersionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	v, err := r.c.GetVersion(ctx, m.PlanID.ValueString(), m.ID.ValueString())
	if client.IsNotFound(err) {
		return
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "Cannot read routing plan version", err)
		return
	}
	if v.Status != "draft" {
		return // published history is kept
	}
	if err := r.c.DiscardDraft(ctx, m.PlanID.ValueString(), m.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		addAPIError(&resp.Diagnostics, "Cannot discard draft", err)
	}
}

// ImportState takes "<plan_id>/<version_id>".
func (r *planVersionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	planID, id, ok := strings.Cut(req.ID, "/")
	if !ok || planID == "" || id == "" {
		resp.Diagnostics.AddError("Invalid import id", "Use <plan_id>/<version_id>.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("plan_id"), planID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}
