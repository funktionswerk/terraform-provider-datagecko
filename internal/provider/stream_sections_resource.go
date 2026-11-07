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
	_ resource.Resource                = &StreamSectionsResource{}
	_ resource.ResourceWithConfigure   = &StreamSectionsResource{}
	_ resource.ResourceWithImportState = &StreamSectionsResource{}
)

type StreamSectionsResource struct {
	client *Client
}

func NewStreamSectionsResource() resource.Resource {
	return &StreamSectionsResource{}
}

type sectionsModel struct {
	ID       types.String   `tfsdk:"id"`
	StreamID types.String   `tfsdk:"stream_id"`
	Sections []sectionModel `tfsdk:"section"`
}

type sectionModel struct {
	Title types.String `tfsdk:"title"`
	Body  types.String `tfsdk:"body"`
}

func (r *StreamSectionsResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_stream_sections"
}

func (r *StreamSectionsResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A stream's About tab: its sections, in order, replaced whole. Declare one per stream: two on one stream overwrite each other on every apply. Destroying it empties the tab.",
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
			"section": schema.ListNestedBlock{
				Description: "One section; at most 20 per stream.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"title": schema.StringAttribute{Required: true, Description: "At most 128 printable characters."},
						"body": schema.StringAttribute{
							Optional: true, Computed: true, Default: stringdefault.StaticString(""),
							Description: "Markdown, at most 20,000 characters, stored exactly as given: a heredoc's trailing newline is part of it.",
						},
					},
				},
			},
		},
	}
}

func (r *StreamSectionsResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *StreamSectionsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan sectionsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	streamID := plan.StreamID.ValueString()

	existing, err := r.client.GetSections(ctx, streamID)
	if apiErr, ok := err.(*APIError); ok && apiErr.IsNotFound() {
		resp.Diagnostics.AddError("Stream not found", fmt.Sprintf("No stream %q in this organization.", streamID))
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error reading sections", err.Error())
		return
	}
	if len(existing) > 0 {
		resp.Diagnostics.AddError("Sections already declared", fmt.Sprintf(
			"Stream %s already has %d section(s) — declared outside Terraform; import this resource with the stream id.",
			streamID, len(existing)))
		return
	}
	r.replace(ctx, plan, &resp.State, &resp.Diagnostics)
}

func (r *StreamSectionsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state sectionsModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	sections, err := r.client.GetSections(ctx, state.StreamID.ValueString())
	if apiErr, ok := err.(*APIError); ok && apiErr.IsNotFound() {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error reading sections", err.Error())
		return
	}
	state.ID = state.StreamID
	state.Sections = toSectionModels(sections)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *StreamSectionsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan sectionsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.replace(ctx, plan, &resp.State, &resp.Diagnostics)
}

func (r *StreamSectionsResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state sectionsModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	_, err := r.client.ReplaceSections(ctx, state.StreamID.ValueString(), nil)
	if apiErr, ok := err.(*APIError); ok && apiErr.IsNotFound() {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error clearing sections", err.Error())
	}
}

func (r *StreamSectionsResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("stream_id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

func (r *StreamSectionsResource) replace(ctx context.Context, plan sectionsModel, state stateSetter, diags diagnosticsAppender) {
	inputs := make([]Section, len(plan.Sections))
	for i, s := range plan.Sections {
		inputs[i] = Section{Title: s.Title.ValueString(), Body: s.Body.ValueString()}
	}
	sections, err := r.client.ReplaceSections(ctx, plan.StreamID.ValueString(), inputs)
	if err != nil {
		diags.AddError("Error replacing sections", err.Error())
		return
	}
	plan.ID = plan.StreamID
	plan.Sections = toSectionModels(sections)
	diags.Append(state.Set(ctx, plan)...)
}

func toSectionModels(sections []Section) []sectionModel {
	models := make([]sectionModel, len(sections))
	for i, s := range sections {
		models[i] = sectionModel{Title: types.StringValue(s.Title), Body: types.StringValue(s.Body)}
	}
	return models
}
