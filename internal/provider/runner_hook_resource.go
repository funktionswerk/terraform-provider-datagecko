package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &RunnerHookResource{}
	_ resource.ResourceWithConfigure   = &RunnerHookResource{}
	_ resource.ResourceWithImportState = &RunnerHookResource{}
)

type RunnerHookResource struct {
	client *Client
}

func NewRunnerHookResource() resource.Resource {
	return &RunnerHookResource{}
}

type runnerHookModel struct {
	ID             types.String `tfsdk:"id"`
	KeyPrefix      types.String `tfsdk:"key_prefix"`
	URL            types.String `tfsdk:"url"`
	Secret         types.String `tfsdk:"secret"`
	LastRingAt     types.String `tfsdk:"last_ring_at"`
	LastRingStatus types.String `tfsdk:"last_ring_status"`
}

func (r *RunnerHookResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_runner_hook"
}

func (r *RunnerHookResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	keep := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	resp.Schema = schema.Schema{
		Description: "The hook a runner is rung at, on one ingest key. One key, one hook.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "The key prefix: the import identifier.",
				PlanModifiers: keep,
			},
			"key_prefix": schema.StringAttribute{
				Required:      true,
				Description:   "The ingest key the runner holds.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"url": schema.StringAttribute{
				Required:    true,
				Description: "https on port 443, at most 500 characters. It is logged: never put a secret in it. A change issues a new secret.",
			},
			"secret": schema.StringAttribute{
				Computed:      true,
				Sensitive:     true,
				Description:   "Signs every ring. Shown once, on create and on every url change; never read back, so an imported hook has none until its url changes.",
				PlanModifiers: []planmodifier.String{secretRotatesWithURL{}},
			},
			"last_ring_at":     schema.StringAttribute{Computed: true, PlanModifiers: keep, Description: "RFC 3339, or empty if never rung."},
			"last_ring_status": schema.StringAttribute{Computed: true, PlanModifiers: keep},
		},
	}
}

// secretRotatesWithURL keeps the stored secret in the plan while url is
// unchanged and leaves it unknown when url changes: a PUT always issues a new one.
type secretRotatesWithURL struct{}

func (secretRotatesWithURL) Description(ctx context.Context) string {
	return "The secret is kept until the url changes."
}

func (m secretRotatesWithURL) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (secretRotatesWithURL) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if req.State.Raw.IsNull() {
		return // create: unknown until the API shows it
	}
	var planURL, stateURL types.String
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("url"), &planURL)...)
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("url"), &stateURL)...)
	if planURL.Equal(stateURL) {
		resp.PlanValue = req.StateValue
	}
}

func (r *RunnerHookResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *RunnerHookResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan runnerHookModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	prefix := plan.KeyPrefix.ValueString()

	set, err := r.client.CreateHook(ctx, prefix, plan.URL.ValueString())
	if apiErr, ok := err.(*APIError); ok && apiErr.IsAlreadyExists() {
		resp.Diagnostics.AddError("Hook already set", fmt.Sprintf(
			"Key %s already has a hook — declared outside Terraform; import it with the key prefix (the secret cannot be recovered).", prefix))
		return
	}
	if apiErr, ok := err.(*APIError); ok && apiErr.IsNotFound() {
		resp.Diagnostics.AddError("Ingest key not found", fmt.Sprintf("No live ingest key %q in this organization.", prefix))
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error setting hook", err.Error())
		return
	}
	// A hook set this moment has never been rung.
	resp.Diagnostics.Append(resp.State.Set(ctx, runnerHookModel{
		ID: types.StringValue(prefix), KeyPrefix: types.StringValue(prefix), URL: types.StringValue(set.URL),
		Secret: types.StringValue(set.Secret), LastRingAt: types.StringValue(""), LastRingStatus: types.StringValue(""),
	})...)
}

func (r *RunnerHookResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state runnerHookModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	runner, err := r.client.FindRunner(ctx, state.KeyPrefix.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error reading hook", err.Error())
		return
	}
	if runner == nil || runner.Hook == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	state.ID = state.KeyPrefix
	state.URL = types.StringValue(runner.Hook.URL)
	state.LastRingAt = types.StringValue(optionalString(runner.Hook.LastRingAt))
	state.LastRingStatus = types.StringValue(runner.Hook.LastRingStatus)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *RunnerHookResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state runnerHookModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	rotated, err := r.client.ReplaceHook(ctx, plan.KeyPrefix.ValueString(), plan.URL.ValueString())
	if apiErr, ok := err.(*APIError); ok && apiErr.IsNotFound() {
		resp.Diagnostics.AddError("Hook is gone", "The hook was cleared outside Terraform; the next plan will set it again.")
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error replacing hook", err.Error())
		return
	}
	state.URL = types.StringValue(rotated.URL)
	state.Secret = types.StringValue(rotated.Secret)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *RunnerHookResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state runnerHookModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteHook(ctx, state.KeyPrefix.ValueString())
	if apiErr, ok := err.(*APIError); ok && apiErr.IsNotFound() {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error clearing hook", err.Error())
	}
}

func (r *RunnerHookResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("key_prefix"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
