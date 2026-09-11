package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
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
	CurrentSystem types.String `tfsdk:"current_system"`
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

func (r *activationResource) Create(_ context.Context, _ resource.CreateRequest, resp *resource.CreateResponse) {
	resp.Diagnostics.AddError(notImplemented, unimplementedDetail("Create"))
}

func (r *activationResource) Read(_ context.Context, _ resource.ReadRequest, resp *resource.ReadResponse) {
	resp.Diagnostics.AddError(notImplemented, unimplementedDetail("Read"))
}

func (r *activationResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(notImplemented, unimplementedDetail("Update"))
}

func (r *activationResource) Delete(_ context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	// Destroying an activation is deliberately not a rollback: the generation
	// on the machine is what it is, and removing a resource from state should
	// not reach out and change a running system. Recorded as unimplemented
	// until that decision is made deliberately rather than by default.
	resp.Diagnostics.AddError(notImplemented, unimplementedDetail("Delete"))
}

func unimplementedDetail(op string) string {
	return fmt.Sprintf(
		"%s is not implemented yet. This provider refuses rather than reporting success, "+
			"because state describing a machine nobody configured is worse than a failed apply.",
		op,
	)
}
