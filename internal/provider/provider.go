// Package provider implements the Harbor Pilot Terraform provider.
package provider

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/ephemeral"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/aurimasniekis/terraform-provider-harbor-pilot/internal/client"
)

// Environment variables shared with the harbor-pilot CLI.
const (
	EnvServer    = "HP_SERVER"
	EnvToken     = "HP_API_TOKEN"      //nolint:gosec // environment variable name
	EnvTokenFile = "HP_API_TOKEN_FILE" //nolint:gosec // environment variable name
)

var (
	_ provider.Provider                       = (*hpProvider)(nil)
	_ provider.ProviderWithEphemeralResources = (*hpProvider)(nil)
)

type hpProvider struct {
	version string
}

// New returns a provider factory.
func New(version string) func() provider.Provider {
	return func() provider.Provider { return &hpProvider{version: version} }
}

type providerModel struct {
	URL                types.String `tfsdk:"url"`
	Token              types.String `tfsdk:"token"`
	TokenFile          types.String `tfsdk:"token_file"`
	CACertFile         types.String `tfsdk:"ca_cert_file"`
	InsecureSkipVerify types.Bool   `tfsdk:"insecure_skip_verify"`
	Timeout            types.String `tfsdk:"timeout"`
}

func (p *hpProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "harborpilot"
	resp.Version = p.version
}

func (p *hpProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages Harbor Pilot configuration through its control-plane API. " +
			"Authenticate with a service principal API token that has the permissions for the resources you manage.",
		Attributes: map[string]schema.Attribute{
			"url": schema.StringAttribute{Optional: true,
				MarkdownDescription: "Server origin, e.g. `https://pilot.example.com`. Defaults to `$" + EnvServer + "`."},
			"token": schema.StringAttribute{Optional: true, Sensitive: true,
				MarkdownDescription: "Service principal API token (`hpa_…`). Prefer `token_file` or `$" + EnvToken + "`; a token written here is stored in plan files."},
			"token_file": schema.StringAttribute{Optional: true,
				MarkdownDescription: "File containing the API token. Defaults to `$" + EnvTokenFile + "`."},
			"ca_cert_file": schema.StringAttribute{Optional: true,
				MarkdownDescription: "PEM bundle used to verify the server certificate instead of the system roots."},
			"insecure_skip_verify": schema.BoolAttribute{Optional: true,
				MarkdownDescription: "Disable TLS certificate verification. For testing only."},
			"timeout": schema.StringAttribute{Optional: true,
				MarkdownDescription: "Per-request timeout as a Go duration (default `30s`, at most `5m`)."},
		},
	}
}

func (p *hpProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var m providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if m.URL.IsUnknown() || m.Token.IsUnknown() || m.TokenFile.IsUnknown() {
		// Values from not-yet-created resources: defer configuration.
		return
	}
	url := m.URL.ValueString()
	if url == "" {
		url = os.Getenv(EnvServer)
	}
	if url == "" {
		resp.Diagnostics.AddAttributeError(path.Root("url"), "Missing server URL", "Set url or "+EnvServer+".")
	}
	token := m.Token.ValueString()
	tokenFile := m.TokenFile.ValueString()
	if token != "" && tokenFile != "" {
		resp.Diagnostics.AddAttributeError(path.Root("token_file"), "Conflicting token settings", "Set token or token_file, not both.")
		return
	}
	if token == "" && tokenFile == "" {
		token = os.Getenv(EnvToken)
		tokenFile = os.Getenv(EnvTokenFile)
	}
	if token == "" && tokenFile != "" {
		b, err := os.ReadFile(tokenFile) //nolint:gosec // operator-chosen token file
		if err != nil {
			resp.Diagnostics.AddAttributeError(path.Root("token_file"), "Cannot read the token file", err.Error())
			return
		}
		token = strings.TrimSpace(string(b))
	}
	if token == "" {
		resp.Diagnostics.AddAttributeError(path.Root("token"), "Missing API token", "Set token_file, token, "+EnvTokenFile+" or "+EnvToken+".")
	}
	timeout := 30 * time.Second
	if s := m.Timeout.ValueString(); s != "" {
		d, err := time.ParseDuration(s)
		if err != nil || d <= 0 || d > 5*time.Minute {
			resp.Diagnostics.AddAttributeError(path.Root("timeout"), "Invalid timeout", "Use a duration between 1s and 5m, e.g. 30s.")
		}
		timeout = d
	}
	if resp.Diagnostics.HasError() {
		return
	}
	c, err := client.New(client.Options{BaseURL: url, Token: token, CACertFile: m.CACertFile.ValueString(),
		InsecureSkipVerify: m.InsecureSkipVerify.ValueBool(), Timeout: timeout, UserAgent: "terraform-provider-harbor-pilot/" + p.version})
	if err != nil {
		resp.Diagnostics.AddError("Invalid provider configuration", err.Error())
		return
	}
	tflog.Debug(ctx, "configured Harbor Pilot client", map[string]any{"url": url})
	resp.ResourceData = c
	resp.DataSourceData = c
	resp.EphemeralResourceData = c
}

func (p *hpProvider) Resources(context.Context) []func() resource.Resource {
	out := make([]func() resource.Resource, 0, len(Kinds)+6)
	for _, k := range Kinds {
		out = append(out, func() resource.Resource { return &objectResource{kind: k} })
	}
	return append(out,
		newPlanVersionResource,
		newGatewayCredentialResource,
		newServicePrincipalResource,
		newAPITokenResource,
		newAuthSettingsResource,
		newOIDCProviderResource,
	)
}

func (p *hpProvider) DataSources(context.Context) []func() datasource.DataSource {
	out := make([]func() datasource.DataSource, 0, len(Kinds)+2)
	for _, k := range Kinds {
		out = append(out, func() datasource.DataSource { return &objectDataSource{kind: k} })
	}
	return append(out, newQuotaAdaptersDataSource, newEventCatalogDataSource)
}

func (p *hpProvider) EphemeralResources(context.Context) []func() ephemeral.EphemeralResource {
	return []func() ephemeral.EphemeralResource{newGatewayTokenEphemeral}
}
