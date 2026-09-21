package provider

import (
	"context"
	"fmt"
	"regexp"

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
	streamNamePattern                                  = regexp.MustCompile(`^[a-z][a-z0-9_.]{0,127}$`)
	_                 resource.Resource                = &StreamResource{}
	_                 resource.ResourceWithConfigure   = &StreamResource{}
	_                 resource.ResourceWithImportState = &StreamResource{}
)

type StreamResource struct {
	client *Client
}

func NewStreamResource() resource.Resource {
	return &StreamResource{}
}

type streamModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	FirstSeen   types.String `tfsdk:"first_seen"`
}

func (r *StreamResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_stream"
}

func (r *StreamResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A named grouping of related series.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "The stream's UUID.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Unique within the organization. Must match ^[a-z][a-z0-9_.]{0,127}$.",
				Validators: []validator.String{
					stringvalidator.RegexMatches(streamNamePattern, "must match ^[a-z][a-z0-9_.]{0,127}$"),
				},
			},
			"description": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString(""),
			},
			"first_seen": schema.StringAttribute{
				Computed:      true,
				Description:   "RFC 3339 timestamp of when this stream was created.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *StreamResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *StreamResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan streamModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	stream, err := r.client.CreateStream(ctx, plan.Name.ValueString(), plan.Description.ValueString())
	if apiErr, ok := err.(*APIError); ok && apiErr.IsAlreadyExists() {
		resp.Diagnostics.AddError(
			"Stream already exists",
			fmt.Sprintf("A stream named %q already exists in this organization. Import it instead: terraform import <this resource's address> <id>.", plan.Name.ValueString()),
		)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error creating stream", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, toStreamModel(stream))...)
}

func (r *StreamResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state streamModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	stream, err := r.client.FindStream(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error reading stream", err.Error())
		return
	}
	if stream == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, toStreamModel(stream))...)
}

func (r *StreamResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan streamModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := plan.Name.ValueString()
	description := plan.Description.ValueString()
	stream, err := r.client.UpdateStream(ctx, plan.ID.ValueString(), &name, &description)
	if apiErr, ok := err.(*APIError); ok && apiErr.IsAlreadyExists() {
		resp.Diagnostics.AddError("Stream already exists", fmt.Sprintf("A stream named %q already exists in this organization.", name))
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error updating stream", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, toStreamModel(stream))...)
}

func (r *StreamResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state streamModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteStream(ctx, state.ID.ValueString())
	if apiErr, ok := err.(*APIError); ok && apiErr.IsNotFound() {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error deleting stream", err.Error())
	}
}

func (r *StreamResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func toStreamModel(s *Stream) streamModel {
	return streamModel{
		ID:          types.StringValue(s.ID),
		Name:        types.StringValue(s.Name),
		Description: types.StringValue(s.Description),
		FirstSeen:   types.StringValue(s.FirstSeen),
	}
}
