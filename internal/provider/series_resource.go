package provider

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	seriesNamePattern                                  = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	_                 resource.Resource                = &SeriesResource{}
	_                 resource.ResourceWithConfigure   = &SeriesResource{}
	_                 resource.ResourceWithImportState = &SeriesResource{}
)

type SeriesResource struct {
	client *Client
}

func NewSeriesResource() resource.Resource {
	return &SeriesResource{}
}

type seriesModel struct {
	ID          types.String `tfsdk:"id"`
	StreamID    types.String `tfsdk:"stream_id"`
	Name        types.String `tfsdk:"name"`
	Unit        types.String `tfsdk:"unit"`
	LastPointAt types.String `tfsdk:"last_point_at"`
}

func (r *SeriesResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_series"
}

func (r *SeriesResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "One line of points, belonging to a stream.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "The series' UUID.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"stream_id": schema.StringAttribute{
				Required:      true,
				Description:   "The stream this series belongs to. A series cannot move between streams.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Unique within the stream. Must match ^[a-z][a-z0-9_]{0,63}$.",
				Validators: []validator.String{
					stringvalidator.RegexMatches(seriesNamePattern, "must match ^[a-z][a-z0-9_]{0,63}$"),
				},
			},
			"unit": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString(""),
			},
			"last_point_at": schema.StringAttribute{
				Computed:      true,
				Description:   "RFC 3339, or empty if no point has ever landed.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *SeriesResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *SeriesResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan seriesModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	series, err := r.client.CreateSeries(ctx, plan.StreamID.ValueString(), plan.Name.ValueString(), plan.Unit.ValueString())
	if apiErr, ok := err.(*APIError); ok && apiErr.IsAlreadyExists() {
		resp.Diagnostics.AddError("Series already exists", fmt.Sprintf("A series named %q already exists on this stream. Import it instead.", plan.Name.ValueString()))
		return
	}
	if apiErr, ok := err.(*APIError); ok && apiErr.IsNotFound() {
		resp.Diagnostics.AddError("Stream not found", fmt.Sprintf("No stream %q in this organization.", plan.StreamID.ValueString()))
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error creating series", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, toSeriesModel(plan.StreamID.ValueString(), series))...)
}

func (r *SeriesResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state seriesModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	stream, err := r.client.FindStream(ctx, state.StreamID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error reading series", err.Error())
		return
	}
	if stream == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	for i := range stream.Series {
		if stream.Series[i].ID == state.ID.ValueString() {
			resp.Diagnostics.Append(resp.State.Set(ctx, toSeriesModel(stream.ID, &stream.Series[i]))...)
			return
		}
	}
	resp.State.RemoveResource(ctx)
}

func (r *SeriesResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan seriesModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := plan.Name.ValueString()
	unit := plan.Unit.ValueString()
	series, err := r.client.UpdateSeries(ctx, plan.StreamID.ValueString(), plan.ID.ValueString(), &name, &unit)
	if apiErr, ok := err.(*APIError); ok && apiErr.IsAlreadyExists() {
		resp.Diagnostics.AddError("Series already exists", fmt.Sprintf("A sibling series is already named %q.", name))
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error updating series", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, toSeriesModel(plan.StreamID.ValueString(), series))...)
}

func (r *SeriesResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state seriesModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteSeries(ctx, state.StreamID.ValueString(), state.ID.ValueString())
	if apiErr, ok := err.(*APIError); ok && apiErr.IsNotFound() {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error deleting series", err.Error())
	}
}

func (r *SeriesResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.SplitN(req.ID, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		resp.Diagnostics.AddError(
			"Unexpected Import Identifier",
			fmt.Sprintf("Expected an identifier of the form <stream_id>/<series_id>, got: %q", req.ID),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("stream_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
}

func toSeriesModel(streamID string, s *SeriesInfo) seriesModel {
	lastPointAt := ""
	if s.LastPointAt != nil {
		lastPointAt = *s.LastPointAt
	}
	return seriesModel{
		ID:          types.StringValue(s.ID),
		StreamID:    types.StringValue(streamID),
		Name:        types.StringValue(s.Name),
		Unit:        types.StringValue(s.Unit),
		LastPointAt: types.StringValue(lastPointAt),
	}
}
