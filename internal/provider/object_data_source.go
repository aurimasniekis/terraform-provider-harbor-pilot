package provider

import (
	"context"
	"fmt"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework-validators/datasourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/aurimasniekis/terraform-provider-harbor-pilot/internal/client"
	"github.com/aurimasniekis/terraform-provider-harbor-pilot/internal/specschema"
)

var (
	_ datasource.DataSourceWithConfigure        = (*objectDataSource)(nil)
	_ datasource.DataSourceWithConfigValidators = (*objectDataSource)(nil)
)

// objectDataSource looks up one object of a generic kind by id or name.
type objectDataSource struct {
	kind Kind
	c    *client.Client
}

type objectDataModel struct {
	ID          types.String         `tfsdk:"id"`
	Name        types.String         `tfsdk:"name"`
	Spec        jsontypes.Normalized `tfsdk:"spec"`
	Revision    types.Int64          `tfsdk:"revision"`
	ManagedBy   types.String         `tfsdk:"managed_by"`
	SecretNames types.Set            `tfsdk:"secret_names"`
}

func (d *objectDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + d.kind.TypeName
}

func (d *objectDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up an existing " + d.kind.Title + " by `id` or `name`. Secret values are never returned.",
		Attributes: map[string]schema.Attribute{
			"id":   schema.StringAttribute{Optional: true, Computed: true, MarkdownDescription: "Object id. Set `id` or `name`."},
			"name": schema.StringAttribute{Optional: true, Computed: true, MarkdownDescription: "Object name. Set `id` or `name`."},
			"spec": schema.StringAttribute{Computed: true, CustomType: jsontypes.NormalizedType{},
				MarkdownDescription: "Canonical specification as JSON; decode with `jsondecode`."},
			"revision":     schema.Int64Attribute{Computed: true},
			"managed_by":   schema.StringAttribute{Computed: true},
			"secret_names": schema.SetAttribute{Computed: true, ElementType: types.StringType, MarkdownDescription: "Names of secrets that are set."},
		},
	}
}

func (d *objectDataSource) ConfigValidators(context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{datasourcevalidator.ExactlyOneOf(path.MatchRoot("id"), path.MatchRoot("name"))}
}

func (d *objectDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("%T", req.ProviderData))
		return
	}
	d.c = c
}

func (d *objectDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m objectDataModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	key := m.ID.ValueString()
	if key == "" {
		key = m.Name.ValueString()
	}
	o, err := d.c.GetObject(ctx, d.kind.Plural, key)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Cannot read "+d.kind.Title+" "+key, err)
		return
	}
	canonical, err := specschema.Canonical(o.Spec)
	if err != nil {
		resp.Diagnostics.AddError("Invalid spec from server", err.Error())
		return
	}
	m.ID, m.Name = types.StringValue(o.ID), types.StringValue(o.Name)
	m.Spec = jsontypes.NewNormalizedValue(canonical)
	m.Revision, m.ManagedBy = types.Int64Value(o.Revision), types.StringValue(o.ManagedBy)
	names := []string{}
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
	set, diags := types.SetValue(types.StringType, elems)
	resp.Diagnostics.Append(diags...)
	m.SecretNames = set
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
