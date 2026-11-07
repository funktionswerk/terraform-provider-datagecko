package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const defaultEndpoint = "https://datagecko.ai/api/v1/"

var (
	_ provider.Provider = &DatageckoProvider{}
)

type DatageckoProvider struct {
	version string
}

type providerModel struct {
	Endpoint types.String `tfsdk:"endpoint"`
	AdminKey types.String `tfsdk:"admin_key"`
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &DatageckoProvider{version: version}
	}
}

func (p *DatageckoProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "datagecko"
	resp.Version = p.version
}

func (p *DatageckoProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages datagecko streams, series, and ingest keys for one organization.",
		Attributes: map[string]schema.Attribute{
			"endpoint": schema.StringAttribute{
				Optional:    true,
				Description: "Base URL of the datagecko API. Defaults to https://datagecko.ai/api/v1/.",
			},
			"admin_key": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "The organization's admin key. Falls back to the DATAGECKO_ADMIN_KEY environment variable.",
			},
		},
	}
}

func (p *DatageckoProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	endpoint := defaultEndpoint
	if !data.Endpoint.IsNull() && data.Endpoint.ValueString() != "" {
		endpoint = data.Endpoint.ValueString()
	}

	adminKey := os.Getenv("DATAGECKO_ADMIN_KEY")
	if !data.AdminKey.IsNull() && data.AdminKey.ValueString() != "" {
		adminKey = data.AdminKey.ValueString()
	}
	if adminKey == "" {
		resp.Diagnostics.AddError(
			"Missing admin key",
			"Set the admin_key provider attribute or the DATAGECKO_ADMIN_KEY environment variable.",
		)
		return
	}

	client := NewClient(endpoint, adminKey, nil)
	resp.DataSourceData = client
	resp.ResourceData = client
}

func (p *DatageckoProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewStreamResource,
		NewSeriesResource,
		NewIngestKeyResource,
		NewMetricResource,
		NewStreamMetricResource,
	}
}

func (p *DatageckoProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewMetricTypesDataSource,
	}
}
