package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &MetricResource{}
	_ resource.ResourceWithConfigure   = &MetricResource{}
	_ resource.ResourceWithImportState = &MetricResource{}
)

type MetricResource struct {
	client *Client
}

func NewMetricResource() resource.Resource {
	return &MetricResource{}
}

type metricModel struct {
	ID        types.String         `tfsdk:"id"`
	Type      types.String         `tfsdk:"type"`
	Name      types.String         `tfsdk:"name"`
	Params    jsontypes.Normalized `tfsdk:"params"`
	Effective jsontypes.Normalized `tfsdk:"effective"`
}

func (r *MetricResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_metric"
}

func (r *MetricResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "An instance of a metric type: a named, id-addressed use of it in the organization. Connect it to a stream with datagecko_stream_metric when the type has per-stream params.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "The metric's UUID.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"type": schema.StringAttribute{
				Required:      true,
				Description:   "The metric type's key (see the datagecko_metric_types data source). A change replaces the metric.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Unique in the organization, case-insensitively; at most 64 characters.",
			},
			"params":    jsonAttribute("The instance's params, exactly as given; they replace the previous ones whole."),
			"effective": computedJSONAttribute("The type's defaults with params laid over them."),
		},
	}
}

func (r *MetricResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", fmt.Sprintf("Expected *Client, got: %T", req.ProviderData))
		return
	}
	r.client = client
}

func (r *MetricResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan metricModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	metric, err := r.client.CreateMetric(ctx, plan.Type.ValueString(), plan.Name.ValueString(), rawJSON(plan.Params))
	if apiErr, ok := err.(*APIError); ok && apiErr.IsAlreadyExists() {
		resp.Diagnostics.AddError("Metric already exists", fmt.Sprintf("A metric named %q is declared outside Terraform; import it.", plan.Name.ValueString()))
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error creating metric", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, toMetricModel(metric))...)
}

func (r *MetricResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state metricModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	metric, err := r.client.GetMetric(ctx, state.ID.ValueString())
	if apiErr, ok := err.(*APIError); ok && apiErr.IsNotFound() {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error reading metric", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, toMetricModel(metric))...)
}

func (r *MetricResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan metricModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	metric, err := r.client.UpdateMetric(ctx, plan.ID.ValueString(), plan.Name.ValueString(), rawJSON(plan.Params))
	if apiErr, ok := err.(*APIError); ok && apiErr.IsAlreadyExists() {
		resp.Diagnostics.AddError("Metric already exists", fmt.Sprintf("A metric named %q is declared outside Terraform; import it.", plan.Name.ValueString()))
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error updating metric", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, toMetricModel(metric))...)
}

func (r *MetricResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state metricModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteMetric(ctx, state.ID.ValueString())
	if apiErr, ok := err.(*APIError); ok && apiErr.IsNotFound() {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error deleting metric", err.Error())
	}
}

func (r *MetricResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func toMetricModel(m *Metric) metricModel {
	return metricModel{
		ID:        types.StringValue(m.ID),
		Type:      types.StringValue(m.Type),
		Name:      types.StringValue(m.Name),
		Params:    normalized(m.Params),
		Effective: normalized(m.Effective),
	}
}
