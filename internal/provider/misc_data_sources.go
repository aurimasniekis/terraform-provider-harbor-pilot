package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/aurimasniekis/terraform-provider-harbor-pilot/internal/client"
)

type dsBase struct{ c *client.Client }

func (d *dsBase) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

// ---------- quota adapters ----------

func newQuotaAdaptersDataSource() datasource.DataSource { return &quotaAdaptersDataSource{} }

type quotaAdaptersDataSource struct{ dsBase }

var quotaAdapterType = map[string]attr.Type{"id": types.StringType, "name": types.StringType, "tier": types.StringType, "family": types.StringType}

func (d *quotaAdaptersDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_quota_adapters"
}

func (d *quotaAdaptersDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists the quota adapters a `harborpilot_quota_monitor` can use, with their tier (`official`, `unofficial`, …).",
		Attributes: map[string]schema.Attribute{
			"adapters": schema.ListNestedAttribute{Computed: true, NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
				"id": schema.StringAttribute{Computed: true}, "name": schema.StringAttribute{Computed: true},
				"tier": schema.StringAttribute{Computed: true}, "family": schema.StringAttribute{Computed: true},
			}}},
		},
	}
}

func (d *quotaAdaptersDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	var raw json.RawMessage
	if err := d.c.GetJSON(ctx, "/quota/adapters", &raw); err != nil {
		addAPIError(&resp.Diagnostics, "Cannot list quota adapters", err)
		return
	}
	var items []map[string]any
	var wrapped struct {
		Items []map[string]any `json:"items"`
	}
	if json.Unmarshal(raw, &wrapped) == nil && wrapped.Items != nil {
		items = wrapped.Items
	} else {
		_ = json.Unmarshal(raw, &items)
	}
	vals := make([]attr.Value, 0, len(items))
	for _, it := range items {
		str := func(k string) types.String {
			s, _ := it[k].(string)
			return types.StringValue(s)
		}
		name := str("name")
		if name.ValueString() == "" {
			name = str("display_name")
		}
		vals = append(vals, types.ObjectValueMust(quotaAdapterType, map[string]attr.Value{"id": str("id"), "name": name, "tier": str("tier"), "family": str("family")}))
	}
	list, diags := types.ListValue(types.ObjectType{AttrTypes: quotaAdapterType}, vals)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, pathRoot("adapters"), list)...)
}

// ---------- event catalog ----------

func newEventCatalogDataSource() datasource.DataSource { return &eventCatalogDataSource{} }

type eventCatalogDataSource struct{ dsBase }

var eventKindType = map[string]attr.Type{"name": types.StringType, "type": types.StringType, "subject": types.StringType,
	"severity": types.StringType, "description": types.StringType}

func (d *eventCatalogDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_event_catalog"
}

func (d *eventCatalogDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists the domain event kinds the server emits, for building event filters on destinations and notification rules.",
		Attributes: map[string]schema.Attribute{
			"events": schema.ListNestedAttribute{Computed: true, NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
				"name": schema.StringAttribute{Computed: true}, "type": schema.StringAttribute{Computed: true},
				"subject": schema.StringAttribute{Computed: true}, "severity": schema.StringAttribute{Computed: true},
				"description": schema.StringAttribute{Computed: true},
			}}},
		},
	}
}

func (d *eventCatalogDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	var out struct {
		Items []struct {
			Name, Type, Subject, Severity, Description string
		} `json:"items"`
	}
	if err := d.c.GetJSON(ctx, "/events/catalog", &out); err != nil {
		addAPIError(&resp.Diagnostics, "Cannot read the event catalog", err)
		return
	}
	vals := make([]attr.Value, 0, len(out.Items))
	for _, k := range out.Items {
		vals = append(vals, types.ObjectValueMust(eventKindType, map[string]attr.Value{"name": types.StringValue(k.Name), "type": types.StringValue(k.Type),
			"subject": types.StringValue(k.Subject), "severity": types.StringValue(k.Severity), "description": types.StringValue(k.Description)}))
	}
	list, diags := types.ListValue(types.ObjectType{AttrTypes: eventKindType}, vals)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, pathRoot("events"), list)...)
}
