// Package provider implements the nixos_activation resource.
//
// The resource exists to make one property true: changing a live NixOS
// configuration must change ONLY this resource. No new image, no new snapshot,
// no replaced server. That is the image/live-config split, and it is the
// reason the whole nivis-tunnel project exists.
//
// What it adds over a Terraform null_resource is Read. A null_resource with
// triggers knows only what it did last time; this resource reads the target's
// current system generation, so a manual nixos-rebuild on the box shows up as
// drift in `plan` instead of being silently overwritten.
package provider

// CurrentSystemLink is the symlink Read follows on the target to discover which
// generation is live.
const CurrentSystemLink = "/run/current-system"

// Version is the provider version, overridden at build time.
var Version = "dev"
