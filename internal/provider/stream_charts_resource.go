package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &StreamChartsResource{}
	_ resource.ResourceWithConfigure   = &StreamChartsResource{}
	_ resource.ResourceWithImportState = &StreamChartsResource{}
)

type StreamChartsResource struct {
	client *Client
}

func NewStreamChartsResource() resource.Resource {
	return &StreamChartsResource{}
}

type chartsModel struct {
	ID       types.String `tfsdk:"id"`
	StreamID types.String `tfsdk:"stream_id"`
	Charts   []chartModel `tfsdk:"chart"`
}

type chartModel struct {
	Title   types.String      `tfsdk:"title"`
	Series  []chartEntryModel `tfsdk:"series"`
	Markers []markerModel     `tfsdk:"marker"`
}

type chartEntryModel struct {
	ID          types.String  `tfsdk:"id"`
	Color       types.String  `tfsdk:"color"`
	Width       types.Float64 `tfsdk:"width"`
	Dash        types.String  `tfsdk:"dash"`
	Fill        types.Bool    `tfsdk:"fill"`
	PointRadius types.Float64 `tfsdk:"point_radius"`
	PointStyle  types.String  `tfsdk:"point_style"`
}

type markerModel struct {
	ID    types.String `tfsdk:"id"`
	Color types.String `tfsdk:"color"`
}

func (r *StreamChartsResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_stream_charts"
}

func (r *StreamChartsResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A stream's declared charts, in order, replaced whole; the first is the hero. Declare one per stream: two on one stream overwrite each other on every apply. Destroying it returns the stream to its implicit chart. Style bounds and vocabularies are the API's; a style field left out is drawn with its default and reads back unset.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "The stream id: the import identifier.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"stream_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
		},
		Blocks: map[string]schema.Block{
			"chart": schema.ListNestedBlock{
				Description: "One chart; at most 20 per stream.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"title": schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("")},
					},
					Blocks: map[string]schema.Block{
						"series": schema.ListNestedBlock{
							Description: "A measure series drawn on the chart, in order.",
							NestedObject: schema.NestedBlockObject{
								Attributes: map[string]schema.Attribute{
									"id":           schema.StringAttribute{Required: true, Description: "A series of this stream."},
									"color":        schema.StringAttribute{Optional: true, Description: "#rrggbb."},
									"width":        schema.Float64Attribute{Optional: true, Description: "0-8."},
									"dash":         schema.StringAttribute{Optional: true, Description: "solid, dashed or dotted."},
									"fill":         schema.BoolAttribute{Optional: true},
									"point_radius": schema.Float64Attribute{Optional: true, Description: "0-8."},
									"point_style":  schema.StringAttribute{Optional: true, Description: "circle, rect, triangle, cross or star."},
								},
							},
						},
						"marker": schema.ListNestedBlock{
							Description: "A marker series shown on the chart.",
							NestedObject: schema.NestedBlockObject{
								Attributes: map[string]schema.Attribute{
									"id":    schema.StringAttribute{Required: true},
									"color": schema.StringAttribute{Optional: true, Description: "#rrggbb."},
								},
							},
						},
					},
				},
			},
		},
	}
}

func (r *StreamChartsResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *StreamChartsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan chartsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	streamID := plan.StreamID.ValueString()

	// A stream's list always exists, so the API has no "already exists" to
	// give: a non-empty one is someone else's declaration.
	existing, err := r.client.GetCharts(ctx, streamID)
	if apiErr, ok := err.(*APIError); ok && apiErr.IsNotFound() {
		resp.Diagnostics.AddError("Stream not found", fmt.Sprintf("No stream %q in this organization.", streamID))
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error reading charts", err.Error())
		return
	}
	if len(existing) > 0 {
		resp.Diagnostics.AddError("Charts already declared", fmt.Sprintf(
			"Stream %s already has %d declared chart(s) — declared outside Terraform; import this resource with the stream id.",
			streamID, len(existing)))
		return
	}
	r.replace(ctx, plan, &resp.State, &resp.Diagnostics)
}

func (r *StreamChartsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state chartsModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	charts, err := r.client.GetCharts(ctx, state.StreamID.ValueString())
	if apiErr, ok := err.(*APIError); ok && apiErr.IsNotFound() {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error reading charts", err.Error())
		return
	}
	state.ID = state.StreamID
	state.Charts = toChartModels(charts)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *StreamChartsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan chartsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.replace(ctx, plan, &resp.State, &resp.Diagnostics)
}

func (r *StreamChartsResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state chartsModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	_, err := r.client.ReplaceCharts(ctx, state.StreamID.ValueString(), nil)
	if apiErr, ok := err.(*APIError); ok && apiErr.IsNotFound() {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error clearing charts", err.Error())
	}
}

func (r *StreamChartsResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("stream_id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

func (r *StreamChartsResource) replace(ctx context.Context, plan chartsModel, state stateSetter, diags diagnosticsAppender) {
	charts, err := r.client.ReplaceCharts(ctx, plan.StreamID.ValueString(), toChartInputs(plan.Charts))
	if err != nil {
		diags.AddError("Error replacing charts", err.Error())
		return
	}
	plan.ID = plan.StreamID
	plan.Charts = toChartModels(charts)
	diags.Append(state.Set(ctx, plan)...)
}

func toChartInputs(models []chartModel) []Chart {
	charts := make([]Chart, len(models))
	for i, m := range models {
		chart := Chart{Title: m.Title.ValueString(), Series: []ChartEntry{}, Markers: []ChartEntry{}}
		for _, e := range m.Series {
			chart.Series = append(chart.Series, ChartEntry{
				ID:          e.ID.ValueString(),
				Color:       e.Color.ValueStringPointer(),
				Width:       e.Width.ValueFloat64Pointer(),
				Dash:        e.Dash.ValueStringPointer(),
				Fill:        e.Fill.ValueBoolPointer(),
				PointRadius: e.PointRadius.ValueFloat64Pointer(),
				PointStyle:  e.PointStyle.ValueStringPointer(),
			})
		}
		for _, m := range m.Markers {
			chart.Markers = append(chart.Markers, ChartEntry{ID: m.ID.ValueString(), Color: m.Color.ValueStringPointer()})
		}
		charts[i] = chart
	}
	return charts
}

func toChartModels(charts []Chart) []chartModel {
	models := make([]chartModel, len(charts))
	for i, c := range charts {
		model := chartModel{Title: types.StringValue(c.Title), Series: []chartEntryModel{}, Markers: []markerModel{}}
		for _, e := range c.Series {
			model.Series = append(model.Series, chartEntryModel{
				ID:          types.StringValue(e.ID),
				Color:       types.StringPointerValue(e.Color),
				Width:       types.Float64PointerValue(e.Width),
				Dash:        types.StringPointerValue(e.Dash),
				Fill:        types.BoolPointerValue(e.Fill),
				PointRadius: types.Float64PointerValue(e.PointRadius),
				PointStyle:  types.StringPointerValue(e.PointStyle),
			})
		}
		for _, m := range c.Markers {
			model.Markers = append(model.Markers, markerModel{ID: types.StringValue(m.ID), Color: types.StringPointerValue(m.Color)})
		}
		models[i] = model
	}
	return models
}
