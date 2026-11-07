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
	_ resource.Resource                = &StreamQueryPanelsResource{}
	_ resource.ResourceWithConfigure   = &StreamQueryPanelsResource{}
	_ resource.ResourceWithImportState = &StreamQueryPanelsResource{}
)

type StreamQueryPanelsResource struct {
	client *Client
}

func NewStreamQueryPanelsResource() resource.Resource {
	return &StreamQueryPanelsResource{}
}

type panelsModel struct {
	ID       types.String `tfsdk:"id"`
	StreamID types.String `tfsdk:"stream_id"`
	Panels   []panelModel `tfsdk:"panel"`
}

type panelModel struct {
	Title      types.String         `tfsdk:"title"`
	Action     types.String         `tfsdk:"action"`
	Parameters jsontypes.Normalized `tfsdk:"parameters"`
}

func (r *StreamQueryPanelsResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_stream_query_panels"
}

func (r *StreamQueryPanelsResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A stream's query panels, in order, replaced whole. A panel is a pointer to an action by name with fixed parameters, never a pre-authorization. Declare one per stream: two on one stream overwrite each other on every apply.",
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
			"panel": schema.ListNestedBlock{
				Description: "One panel; at most 8 per stream.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"title":      schema.StringAttribute{Required: true},
						"action":     schema.StringAttribute{Required: true, Description: "A declared action's name. Its existence is not checked."},
						"parameters": jsonAttribute("Fixed parameters, at most 8 KB; never dt, which datagecko fills."),
					},
				},
			},
		},
	}
}

func (r *StreamQueryPanelsResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *StreamQueryPanelsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan panelsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	streamID := plan.StreamID.ValueString()

	existing, err := r.client.GetQueryPanels(ctx, streamID)
	if apiErr, ok := err.(*APIError); ok && apiErr.IsNotFound() {
		resp.Diagnostics.AddError("Stream not found", fmt.Sprintf("No stream %q in this organization.", streamID))
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error reading query panels", err.Error())
		return
	}
	if len(existing) > 0 {
		resp.Diagnostics.AddError("Query panels already declared", fmt.Sprintf(
			"Stream %s already has %d query panel(s) — declared outside Terraform; import this resource with the stream id.",
			streamID, len(existing)))
		return
	}
	r.replace(ctx, plan, &resp.State, &resp.Diagnostics)
}

func (r *StreamQueryPanelsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state panelsModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	panels, err := r.client.GetQueryPanels(ctx, state.StreamID.ValueString())
	if apiErr, ok := err.(*APIError); ok && apiErr.IsNotFound() {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error reading query panels", err.Error())
		return
	}
	state.ID = state.StreamID
	state.Panels = toPanelModels(panels)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *StreamQueryPanelsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan panelsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.replace(ctx, plan, &resp.State, &resp.Diagnostics)
}

func (r *StreamQueryPanelsResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state panelsModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	_, err := r.client.ReplaceQueryPanels(ctx, state.StreamID.ValueString(), nil)
	if apiErr, ok := err.(*APIError); ok && apiErr.IsNotFound() {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error clearing query panels", err.Error())
	}
}

func (r *StreamQueryPanelsResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("stream_id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

func (r *StreamQueryPanelsResource) replace(ctx context.Context, plan panelsModel, state stateSetter, diags diagnosticsAppender) {
	inputs := make([]QueryPanel, len(plan.Panels))
	for i, p := range plan.Panels {
		inputs[i] = QueryPanel{Title: p.Title.ValueString(), Action: p.Action.ValueString(), Parameters: rawJSON(p.Parameters)}
	}
	panels, err := r.client.ReplaceQueryPanels(ctx, plan.StreamID.ValueString(), inputs)
	if err != nil {
		diags.AddError("Error replacing query panels", err.Error())
		return
	}
	plan.ID = plan.StreamID
	plan.Panels = toPanelModels(panels)
	diags.Append(state.Set(ctx, plan)...)
}

func toPanelModels(panels []QueryPanel) []panelModel {
	models := make([]panelModel, len(panels))
	for i, p := range panels {
		models[i] = panelModel{
			Title:      types.StringValue(p.Title),
			Action:     types.StringValue(p.Action),
			Parameters: normalized(p.Parameters),
		}
	}
	return models
}
