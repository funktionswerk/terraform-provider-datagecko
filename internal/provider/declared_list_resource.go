package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
)

// listShape is what one of a stream's replace-whole lists — charts, sections,
// query panels — decides for itself. W is the wire type the client speaks, M
// the model the block holds in state. Everything the three share is
// listResource: one resource per stream, a create that refuses a list someone
// else declared, a replace of the whole list, a delete that empties it.
type listShape[W, M any] struct {
	name        string // plural: the type suffix (_stream_<name>) and the messages
	singular    string // the block name
	description string
	block       schema.ListNestedBlock
	get         func(*Client, context.Context, string) ([]W, error)
	replace     func(*Client, context.Context, string, []W) ([]W, error)
	toWire      func([]M) []W
	toModel     func([]W) []M // always a non-nil slice: a block list is never null
}

type listResource[W, M any] struct {
	clientHolder
	shape listShape[W, M]
}

func newListResource[W, M any](shape listShape[W, M]) resource.Resource {
	return &listResource[W, M]{shape: shape}
}

func (r *listResource[W, M]) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_stream_" + r.shape.name
}

func (r *listResource[W, M]) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: r.shape.description + " Declare one per stream: two on one stream overwrite each other on every apply.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "The stream id: the import identifier.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"stream_id": schema.StringAttribute{
				Required:      true,
				Description:   "The stream whose list this is. A change replaces the resource.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
		},
		Blocks: map[string]schema.Block{r.shape.singular: r.shape.block},
	}
}

func (r *listResource[W, M]) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	streamID, models, ok := r.planned(ctx, req.Plan, &resp.Diagnostics)
	if !ok {
		return
	}

	// A stream's list always exists, so the API has no "already exists" to
	// give: a non-empty one is someone else's declaration.
	existing, err := r.shape.get(r.client, ctx, streamID)
	if isNotFound(err) {
		resp.Diagnostics.AddError("Stream not found", fmt.Sprintf("No stream %q in this organization.", streamID))
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error reading "+r.shape.name, err.Error())
		return
	}
	if len(existing) > 0 {
		refuseExisting(&resp.Diagnostics, r.title()+" already declared", fmt.Sprintf(
			"Stream %s already has %d declared %s; the import id is the stream id", streamID, len(existing), r.shape.name))
		return
	}
	r.replace(ctx, streamID, models, &resp.State, &resp.Diagnostics)
}

func (r *listResource[W, M]) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var streamID string
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("stream_id"), &streamID)...)
	if resp.Diagnostics.HasError() {
		return
	}

	wire, err := r.shape.get(r.client, ctx, streamID)
	if isNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error reading "+r.shape.name, err.Error())
		return
	}
	r.setState(ctx, streamID, r.shape.toModel(wire), &resp.State, &resp.Diagnostics)
}

func (r *listResource[W, M]) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	streamID, models, ok := r.planned(ctx, req.Plan, &resp.Diagnostics)
	if !ok {
		return
	}
	r.replace(ctx, streamID, models, &resp.State, &resp.Diagnostics)
}

// Delete empties the list: the stream keeps whatever the API shows for an
// undeclared one (its implicit chart, an empty tab, no panels).
func (r *listResource[W, M]) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var streamID string
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("stream_id"), &streamID)...)
	if resp.Diagnostics.HasError() {
		return
	}
	_, err := r.shape.replace(r.client, ctx, streamID, nil)
	finishDelete(&resp.Diagnostics, "clearing "+r.shape.name, err)
}

func (r *listResource[W, M]) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("stream_id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

func (r *listResource[W, M]) planned(ctx context.Context, plan tfsdk.Plan, diags *diag.Diagnostics) (string, []M, bool) {
	var streamID string
	var models []M
	diags.Append(plan.GetAttribute(ctx, path.Root("stream_id"), &streamID)...)
	diags.Append(plan.GetAttribute(ctx, path.Root(r.shape.singular), &models)...)
	return streamID, models, !diags.HasError()
}

func (r *listResource[W, M]) replace(ctx context.Context, streamID string, models []M, state *tfsdk.State, diags *diag.Diagnostics) {
	wire, err := r.shape.replace(r.client, ctx, streamID, r.shape.toWire(models))
	if err != nil {
		diags.AddError("Error replacing "+r.shape.name, err.Error())
		return
	}
	r.setState(ctx, streamID, r.shape.toModel(wire), state, diags)
}

func (r *listResource[W, M]) setState(ctx context.Context, streamID string, models []M, state *tfsdk.State, diags *diag.Diagnostics) {
	diags.Append(state.SetAttribute(ctx, path.Root("id"), streamID)...)
	diags.Append(state.SetAttribute(ctx, path.Root("stream_id"), streamID)...)
	diags.Append(state.SetAttribute(ctx, path.Root(r.shape.singular), models)...)
}

func (r *listResource[W, M]) title() string {
	return strings.ToUpper(r.shape.name[:1]) + r.shape.name[1:]
}
