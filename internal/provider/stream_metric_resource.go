package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &StreamMetricResource{}
	_ resource.ResourceWithConfigure   = &StreamMetricResource{}
	_ resource.ResourceWithImportState = &StreamMetricResource{}
)

type StreamMetricResource struct {
	client *Client
}

func NewStreamMetricResource() resource.Resource {
	return &StreamMetricResource{}
}

type streamMetricModel struct {
	ID        types.String         `tfsdk:"id"`
	StreamID  types.String         `tfsdk:"stream_id"`
	MetricID  types.String         `tfsdk:"metric_id"`
	Params    jsontypes.Normalized `tfsdk:"params"`
	Effective jsontypes.Normalized `tfsdk:"effective"`
}

func (r *StreamMetricResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_stream_metric"
}

func (r *StreamMetricResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "A metric's row on one stream: connects the metric to the stream and carries its per-stream params (coverage's markers and target, as series ids).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "<stream_id>/<metric_id>: the import identifier. The pair is the row's identity.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"stream_id": schema.StringAttribute{Required: true, PlanModifiers: replace},
			"metric_id": schema.StringAttribute{Required: true, PlanModifiers: replace},
			"params":    jsonAttribute("The row's params, exactly as given; replaced whole on change."),
			"effective": computedJSONAttribute("The type's defaults, then the metric's params, then this row's."),
		},
	}
}

func (r *StreamMetricResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *StreamMetricResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan streamMetricModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	streamID, metricID := plan.StreamID.ValueString(), plan.MetricID.ValueString()

	row, err := r.client.CreateStreamMetric(ctx, streamID, metricID, rawJSON(plan.Params))
	if apiErr, ok := err.(*APIError); ok && apiErr.IsAlreadyExists() {
		resp.Diagnostics.AddError("Stream metric already exists", fmt.Sprintf(
			"Metric %s is already set on stream %s — declared outside Terraform; import it as %s/%s.",
			metricID, streamID, streamID, metricID))
		return
	}
	if apiErr, ok := err.(*APIError); ok && apiErr.IsNotFound() {
		resp.Diagnostics.AddError("Stream or metric not found", apiErr.Message)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error creating stream metric", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, toStreamMetricModel(row))...)
}

func (r *StreamMetricResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state streamMetricModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	row, err := r.client.GetStreamMetric(ctx, state.StreamID.ValueString(), state.MetricID.ValueString())
	if apiErr, ok := err.(*APIError); ok && apiErr.IsNotFound() {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error reading stream metric", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, toStreamMetricModel(row))...)
}

func (r *StreamMetricResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan streamMetricModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	row, err := r.client.ReplaceStreamMetric(ctx, plan.StreamID.ValueString(), plan.MetricID.ValueString(), rawJSON(plan.Params))
	if apiErr, ok := err.(*APIError); ok && apiErr.IsNotFound() {
		resp.Diagnostics.AddError("Stream metric is gone", "The row was removed outside Terraform; the next plan will create it again.")
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error updating stream metric", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, toStreamMetricModel(row))...)
}

func (r *StreamMetricResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state streamMetricModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteStreamMetric(ctx, state.StreamID.ValueString(), state.MetricID.ValueString())
	if apiErr, ok := err.(*APIError); ok && apiErr.IsNotFound() {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error deleting stream metric", err.Error())
	}
}

func (r *StreamMetricResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.SplitN(req.ID, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		resp.Diagnostics.AddError(
			"Unexpected Import Identifier",
			fmt.Sprintf("Expected an identifier of the form <stream_id>/<metric_id>, got: %q", req.ID),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("stream_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("metric_id"), parts[1])...)
}

func toStreamMetricModel(row *StreamMetric) streamMetricModel {
	return streamMetricModel{
		ID:        types.StringValue(row.Stream + "/" + row.Metric),
		StreamID:  types.StringValue(row.Stream),
		MetricID:  types.StringValue(row.Metric),
		Params:    normalized(row.Params),
		Effective: normalized(row.Effective),
	}
}
