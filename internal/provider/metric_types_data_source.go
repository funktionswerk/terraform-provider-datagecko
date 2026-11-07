package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = &MetricTypesDataSource{}
	_ datasource.DataSourceWithConfigure = &MetricTypesDataSource{}
)

type MetricTypesDataSource struct {
	client *Client
}

func NewMetricTypesDataSource() datasource.DataSource {
	return &MetricTypesDataSource{}
}

type metricTypeModel struct {
	Key          types.String         `tfsdk:"key"`
	Presentation types.String         `tfsdk:"presentation"`
	Defaults     jsontypes.Normalized `tfsdk:"defaults"`
	Required     []types.String       `tfsdk:"required"`
	PerStream    []types.String       `tfsdk:"per_stream"`
}

type metricTypesModel struct {
	Types []metricTypeModel `tfsdk:"types"`
}

func (d *MetricTypesDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_metric_types"
}

func (d *MetricTypesDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Every metric type datagecko knows, with its defaults: look a type's defaults up instead of copying them.",
		Attributes: map[string]schema.Attribute{
			"types": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"key":          schema.StringAttribute{Computed: true, Description: "What a datagecko_metric's type names."},
						"presentation": schema.StringAttribute{Computed: true},
						"defaults": schema.StringAttribute{
							CustomType:  jsontypes.NormalizedType{},
							Computed:    true,
							Description: "Every defaulted param, as a JSON object.",
						},
						"required": schema.ListAttribute{
							ElementType: types.StringType, Computed: true,
							Description: "Params an instance must give.",
						},
						"per_stream": schema.ListAttribute{
							ElementType: types.StringType, Computed: true,
							Description: "Params set on a datagecko_stream_metric, not on the instance.",
						},
					},
				},
			},
		},
	}
}

func (d *MetricTypesDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if client := takeClient(req.ProviderData, &resp.Diagnostics); client != nil {
		d.client = client
	}
}

func (d *MetricTypesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	listed, err := d.client.ListMetricTypes(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Error listing metric types", err.Error())
		return
	}
	model := metricTypesModel{Types: make([]metricTypeModel, len(listed))}
	for i, t := range listed {
		model.Types[i] = metricTypeModel{
			Key:          types.StringValue(t.Key),
			Presentation: types.StringValue(t.Presentation),
			Defaults:     normalized(t.Defaults),
			Required:     stringValues(t.Required),
			PerStream:    stringValues(t.PerStream),
		}
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, model)...)
}

func stringValues(in []string) []types.String {
	out := make([]types.String, len(in))
	for i, s := range in {
		out[i] = types.StringValue(s)
	}
	return out
}
