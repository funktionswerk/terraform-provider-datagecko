package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &IngestKeyResource{}
	_ resource.ResourceWithConfigure   = &IngestKeyResource{}
	_ resource.ResourceWithImportState = &IngestKeyResource{}
)

type IngestKeyResource struct {
	clientHolder
}

func NewIngestKeyResource() resource.Resource {
	return &IngestKeyResource{}
}

type ingestKeyModel struct {
	ID         types.String `tfsdk:"id"`
	Name       types.String `tfsdk:"name"`
	Secret     types.String `tfsdk:"secret"`
	Created    types.String `tfsdk:"created"`
	LastUsedAt types.String `tfsdk:"last_used_at"`
	Revoked    types.Bool   `tfsdk:"revoked"`
}

func (r *IngestKeyResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ingest_key"
}

func (r *IngestKeyResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "An ingest-role API key. Its secret is shown once, at creation, and cannot be refetched — importing an existing key leaves secret null.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "The key's prefix — how the API addresses it.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "A rename does not change the id, the secret, or what the key can do.",
			},
			"secret": schema.StringAttribute{
				Computed:      true,
				Sensitive:     true,
				Description:   "Shown once, at creation. Store it now — this state is the only place it's kept.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"created": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"last_used_at": schema.StringAttribute{
				Computed: true,
			},
			"revoked": schema.BoolAttribute{
				Computed: true,
			},
		},
	}
}

func (r *IngestKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ingestKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	key, err := r.client.CreateIngestKey(ctx, plan.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error creating ingest key", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, ingestKeyModel{
		ID:         types.StringValue(key.Prefix),
		Name:       types.StringValue(key.Name),
		Secret:     types.StringValue(key.Secret),
		Created:    types.StringValue(key.Created),
		LastUsedAt: types.StringValue(optionalString(key.LastUsedAt)),
		Revoked:    types.BoolValue(key.Revoked),
	})...)
}

func (r *IngestKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ingestKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	key, err := r.client.FindKey(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error reading ingest key", err.Error())
		return
	}
	if key == nil || key.Revoked {
		resp.State.RemoveResource(ctx)
		return
	}

	state.Name = types.StringValue(key.Name)
	state.Created = types.StringValue(key.Created)
	state.LastUsedAt = types.StringValue(optionalString(key.LastUsedAt))
	state.Revoked = types.BoolValue(key.Revoked)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *IngestKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan ingestKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var state ingestKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	key, err := r.client.RenameKey(ctx, state.ID.ValueString(), plan.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error renaming ingest key", err.Error())
		return
	}

	state.Name = types.StringValue(key.Name)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *IngestKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ingestKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteKey(ctx, state.ID.ValueString())
	if isNotFound(err) {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error revoking ingest key", err.Error())
	}
}

func (r *IngestKeyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func optionalString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
