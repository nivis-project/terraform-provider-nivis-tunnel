// Package provider implements the nixos_activation resource.
//
// The resource exists to make one property true: changing a live NixOS
// configuration must change ONLY this resource. No new image, no new snapshot,
// no replaced server. That is the image/live-config split, and it is why the
// nivis-tunnel project exists at all.
//
// What it adds over a Terraform null_resource with triggers is Read. A
// null_resource knows only what it did last time; it cannot tell you that
// someone ran nixos-rebuild on the box by hand. This resource reads the
// generation the target is actually running, so that shows up as drift in plan
// instead of being silently overwritten.
package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// Version is the provider version, overridden at build time.
var Version = "dev"

// TypeName is the provider's name in configuration. Resources are prefixed with
// it, so the resource reads as nixos_activation only because the provider is
// addressed separately.
const TypeName = "nivis-tunnel"

// New returns the provider.
func New(version string) provider.Provider {
	return &tunnelProvider{version: version}
}

type tunnelProvider struct{ version string }

func (p *tunnelProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = TypeName
	resp.Version = p.version
}

func (p *tunnelProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Activates NixOS closures on machines reached through a nivis-tunnel relay. " +
			"Connection settings are per-resource rather than provider-wide, because one " +
			"orchestrator routinely reaches targets behind different relays.",
	}
}

// Configure takes no provider-wide settings.
//
// Connection details live on each resource instead. One orchestrator routinely
// reaches targets behind different relays, and hoisting the relay to the
// provider would force an alias per relay for no gain.
func (p *tunnelProvider) Configure(_ context.Context, _ provider.ConfigureRequest, _ *provider.ConfigureResponse) {
}

func (p *tunnelProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewActivationResource,
	}
}

func (p *tunnelProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return nil
}
