package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &ActionResource{}
	_ resource.ResourceWithConfigure   = &ActionResource{}
	_ resource.ResourceWithImportState = &ActionResource{}
)

type ActionResource struct {
	client *Client
}

func NewActionResource() resource.Resource {
	return &ActionResource{}
}

type actionModel struct {
	ID               types.String         `tfsdk:"id"`
	Name             types.String         `tfsdk:"name"`
	Description      types.String         `tfsdk:"description"`
	ParametersSchema jsontypes.Normalized `tfsdk:"parameters_schema"`
	ExpiresAfter     types.Int64          `tfsdk:"expires_after"`
	ResultMaxBytes   types.Int64          `tfsdk:"result_max_bytes"`
	RequestRole      types.String         `tfsdk:"request_role"`
}

func (r *ActionResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_action"
}

func (r *ActionResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	keep := []planmodifier.Int64{int64planmodifier.UseStateForUnknown()}
	resp.Schema = schema.Schema{
		Description: "A declared query action: what a runner answers and a query panel calls. Bounds are the API's, refused by name, never adjusted.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "The action's UUID.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:      true,
				Description:   "The action's key and import identifier. A rename is a new action; the old one is deleted, which is refused while it has a run in flight.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"description": schema.StringAttribute{
				Optional: true, Computed: true, Default: stringdefault.StaticString(""),
				Description: "At most 500 characters.",
			},
			"parameters_schema": jsonAttribute("A JSON Schema (draft 2020-12) for a run's parameters, stored exactly as given and closed when a run is checked; references may only point inside it."),
			"expires_after": schema.Int64Attribute{
				Optional: true, Computed: true, PlanModifiers: keep,
				Description: "Seconds a run may wait for a runner; at most 3600. The API's default when never set.",
			},
			"result_max_bytes": schema.Int64Attribute{
				Optional: true, Computed: true, PlanModifiers: keep,
				Description: "The largest result a runner may report; at most 4 MiB. The API's default when never set.",
			},
			"request_role": schema.StringAttribute{
				Optional: true, Computed: true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				Description:   "The least membership role that may request a run from the SPA. The API's default when never set.",
			},
		},
	}
}

func (r *ActionResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *ActionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan actionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	action, err := r.client.CreateAction(ctx, plan.Name.ValueString(), inputOf(plan))
	if apiErr, ok := err.(*APIError); ok && apiErr.IsAlreadyExists() {
		resp.Diagnostics.AddError("Action already exists", fmt.Sprintf("An action named %q is declared outside Terraform; import it.", plan.Name.ValueString()))
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error creating action", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, toActionModel(action))...)
}

func (r *ActionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state actionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	action, err := r.client.FindAction(ctx, state.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error reading action", err.Error())
		return
	}
	if action == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, toActionModel(action))...)
}

func (r *ActionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan actionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	action, err := r.client.ReplaceAction(ctx, plan.Name.ValueString(), inputOf(plan))
	if apiErr, ok := err.(*APIError); ok && apiErr.IsNotFound() {
		resp.Diagnostics.AddError("Action is gone", "The action was deleted outside Terraform; the next plan will create it again.")
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error replacing action", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, toActionModel(action))...)
}

func (r *ActionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state actionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteAction(ctx, state.Name.ValueString())
	if apiErr, ok := err.(*APIError); ok && apiErr.IsNotFound() {
		return
	}
	if apiErr, ok := err.(*APIError); ok && apiErr.IsAlreadyExists() {
		resp.Diagnostics.AddError("Action has runs in flight", apiErr.Message)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error deleting action", err.Error())
	}
}

func (r *ActionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}

// inputOf is what a create or a replace sends: description and schema always,
// each server-defaulted field whenever the plan knows it — so a replace, which
// is whole, never resets a value Terraform holds.
func inputOf(plan actionModel) ActionInput {
	description := plan.Description.ValueString()
	in := ActionInput{Description: &description, ParametersSchema: rawJSON(plan.ParametersSchema)}
	if known(plan.ExpiresAfter.IsNull(), plan.ExpiresAfter.IsUnknown()) {
		in.ExpiresAfter = plan.ExpiresAfter.ValueInt64Pointer()
	}
	if known(plan.ResultMaxBytes.IsNull(), plan.ResultMaxBytes.IsUnknown()) {
		in.ResultMaxBytes = plan.ResultMaxBytes.ValueInt64Pointer()
	}
	if known(plan.RequestRole.IsNull(), plan.RequestRole.IsUnknown()) {
		in.RequestRole = plan.RequestRole.ValueStringPointer()
	}
	return in
}

func known(isNull, isUnknown bool) bool { return !isNull && !isUnknown }

func toActionModel(a *Action) actionModel {
	return actionModel{
		ID:               types.StringValue(a.ID),
		Name:             types.StringValue(a.Name),
		Description:      types.StringValue(a.Description),
		ParametersSchema: normalized(a.ParametersSchema),
		ExpiresAfter:     types.Int64Value(a.ExpiresAfter),
		ResultMaxBytes:   types.Int64Value(a.ResultMaxBytes),
		RequestRole:      types.StringValue(a.RequestRole),
	}
}
