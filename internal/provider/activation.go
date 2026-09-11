package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// CurrentSystemLink is the symlink Read follows on the target to discover which
// generation is live.
const CurrentSystemLink = "/run/current-system"

// ResourceTypeName is the full type name in configuration.
const ResourceTypeName = "nixos_activation"

// notImplemented is the summary every unimplemented operation reports.
//
// It refuses rather than pretending. A provider that appears to work while
// doing nothing is worse than one that fails: it writes state describing a
// machine nobody configured, and the next plan believes it.
const notImplemented = "Operation not implemented"

// ActivationModel is the resource's state.
type ActivationModel struct {
	Closure       types.String `tfsdk:"closure"`
	StreamID      types.String `tfsdk:"stream_id"`
	Relay         types.String `tfsdk:"relay"`
	KeyFile       types.String `tfsdk:"key_file"`
	Profile       types.String `tfsdk:"profile"`
	TunnelCommand types.String `tfsdk:"tunnel_command"`
	CurrentSystem types.String `tfsdk:"current_system"`
}

// target builds the connection description from the model.
func (m ActivationModel) target() target {
	return target{
		streamID:      m.StreamID.ValueString(),
		relay:         m.Relay.ValueString(),
		keyFile:       m.KeyFile.ValueString(),
		tunnelCommand: m.TunnelCommand.ValueString(),
	}
}

// NewActivationResource returns the nixos_activation resource.
func NewActivationResource() resource.Resource { return &activationResource{} }

type activationResource struct{}

func (r *activationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	// The framework prefixes with the provider type name; the configured type
	// is what a user writes, so build it explicitly rather than by convention.
	resp.TypeName = ResourceTypeName
	_ = req
}

func (r *activationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Activates a NixOS closure on a target reached through a nivis-tunnel relay. " +
			"Changing the closure changes only this resource: no image is rebuilt, no snapshot " +
			"is taken and no server is replaced. That is the whole point.",
		Attributes: map[string]schema.Attribute{
			"closure": schema.StringAttribute{
				Required: true,
				Description: "Nix store path of the system closure to activate. " +
					"This is the diff. The path arrives already realised, so a changed store path " +
					"IS the change — there is no trigger, no hash and no timestamp to keep in step.",
			},
			"stream_id": schema.StringAttribute{
				Required: true,
				Description: "Rendezvous id the target announces, normally its cloud instance id. " +
					"An identifier and never a credential: anyone may claim one, and all authority " +
					"comes from the handshake.",
				PlanModifiers: []planmodifier.String{
					// A different target is a different resource, not an
					// update to this one.
					stringplanmodifier.RequiresReplace(),
				},
			},
			"relay": schema.StringAttribute{
				Required:    true,
				Description: "Address of the rendezvous relay, host:port.",
			},
			"key_file": schema.StringAttribute{
				Required: true,
				Description: "Path to the orchestrator private key. The only secret in the system; " +
					"the target holds the public half, which is what lets a boot image carry key " +
					"material while carrying no secret.",
			},
			"profile": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString(DefaultProfile),
				Description: "Nix profile the closure becomes a generation of. Generations are what " +
					"make a rollback possible at all, and deploy-rs's several-profiles-per-node " +
					"model is where this goes next. Defaults to " + DefaultProfile + ".",
			},
			"tunnel_command": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString(DefaultTunnelCommand),
				Description: "The nivis-tunnel client used as ssh's ProxyCommand. Name it explicitly " +
					"rather than relying on PATH when the provider runs somewhere the operator " +
					"does not control.",
			},
			"current_system": schema.StringAttribute{
				Computed: true,
				Description: "The generation the target is actually running, read from " +
					CurrentSystemLink + ". This is what distinguishes this resource from a " +
					"provisioner: it is read from the machine rather than remembered from the " +
					"last apply, so a manual nixos-rebuild on the target shows up as drift.",
			},
		},
	}
}

// ModifyPlan turns a difference between what is running and what is configured
// into a planned update.
//
// Without this, Read would be decorative. `current_system` is computed, so a
// refresh that discovers a different generation running would quietly update
// state and the next plan would report no changes — the resource would know
// about the drift and do nothing about it, which is precisely the behaviour a
// null_resource has and the reason this is a resource instead.
//
// Comparing the running generation against the configured closure is the whole
// convergence rule: if they differ, for any reason, an apply is owed.
func (r *activationResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	// Creating or destroying: nothing to compare against.
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}

	var state, plan ActivationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if state.CurrentSystem.ValueString() == plan.Closure.ValueString() {
		// What is running is what is configured. Nothing owed.
		return
	}

	// Marking it unknown is what makes the plan non-empty, so an operator sees
	// that the machine is not running what the configuration says.
	plan.CurrentSystem = types.StringUnknown()
	resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
}

func (r *activationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ActivationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.apply(ctx, plan, &resp.Diagnostics, &resp.State)
}

func (r *activationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan ActivationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.apply(ctx, plan, &resp.Diagnostics, &resp.State)
}

// apply is Create and Update, which are the same operation. There is no
// first-time work: activating a closure on a machine that already has one is
// the normal case, and treating the first apply as special would mean two code
// paths where the second is the one that runs forever.
func (r *activationResource) apply(ctx context.Context, plan ActivationModel, diags *diag.Diagnostics, state *tfsdk.State) {
	t := plan.target()

	if err := t.Activate(ctx, plan.Closure.ValueString(), plan.Profile.ValueString()); err != nil {
		diags.AddError("Activation failed", err.Error())
		return
	}

	// Read back rather than assuming. What is running is a fact about the
	// machine, and asserting it from the plan would make state a record of
	// intent dressed up as observation.
	current, err := t.CurrentSystem(ctx)
	if err != nil {
		diags.AddError("Activated, but could not read the result back",
			"The closure was activated. Reading "+CurrentSystemLink+" afterwards failed, "+
				"so state does not record what is running:\n"+err.Error())
		return
	}
	plan.CurrentSystem = types.StringValue(current)

	diags.Append(state.Set(ctx, &plan)...)
}

func (r *activationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var model ActivationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	current, err := model.target().CurrentSystem(ctx)
	if err != nil {
		// Not RemoveResource. An unreachable machine is a machine that might be
		// rebooting; reporting the resource as gone would provoke a recreation
		// and activate a closure on something that may already have it.
		resp.Diagnostics.AddError("Could not read the target's current generation",
			"The resource is left in state unchanged; a target that cannot be reached is not "+
				"the same as one that no longer exists.\n"+err.Error())
		return
	}

	model.CurrentSystem = types.StringValue(current)
	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

func (r *activationResource) Delete(_ context.Context, _ resource.DeleteRequest, _ *resource.DeleteResponse) {
	// Nothing. Removing a resource from state is a statement about what is
	// managed, not an instruction to tear down a running system — and there is
	// nothing sensible to tear down to. Rolling a machine back to a previous
	// generation is a deliberate act that belongs behind an explicit mechanism,
	// not a side effect of `terraform destroy`.
}

// A rule for whoever adds the next operation, kept here rather than in the
// spec because it is about code that does not exist yet:
//
//	An operation that is not implemented must return an error diagnostic
//	naming itself. Never report success while doing nothing. State describing
//	a machine nobody configured is worse than a failed apply, because the next
//	plan believes it.
//
// unimplementedDetail is the message to use.
func unimplementedDetail(op string) string {
	return fmt.Sprintf(
		"%s is not implemented. This provider refuses rather than reporting success, "+
			"because state describing a machine nobody configured is worse than a failed apply.",
		op,
	)
}
