# terraform-provider-nivis-tunnel

The OpenTofu/Terraform provider half of
[nivis-tunnel](https://github.com/nivis-project/nivis-tunnel). It carries one
resource, `nixos_activation`, which exists to make one property true:

> Changing a live NixOS configuration must change **only this resource**.
> No new image. No new snapshot. No replaced server.

That is the image/live-config split.

## Why not a `null_resource`

`null_resource` with `triggers` knows only what it did last time. This resource
implements **Read**:

```
readlink /run/current-system   →  the generation actually running
```

so a manual `nixos-rebuild` on the target shows up as drift in `plan` instead of
being silently overwritten.

## Shape

```hcl
resource "nixos_activation" "host" {
  target  = <reachable through the nivis-tunnel ProxyCommand>
  closure = <a nivis __build leaf, realised before apply>
}
```

The closure arrives as a store path nivis has already realised, so the diff is
free: *the store path changed* **is** the change. Apply is three steps —
`nix copy`, `nix-env --profile /nix/var/nix/profiles/system --set`, then
`switch-to-configuration switch`.

nivis resolves providers by filesystem path, so no registry publication is
needed. The repo is named for the registry convention so publishing later needs
no rename.

## Getting started

```sh
nix develop          # go, gopls, golangci-lint, jj, opentofu
nix flake check      # the gate: build, unit tests, formatting
```

Planning and the backlog live in the companion repo:

```sh
cd ../nivis-tunnel && beans roadmap
```

## Licence

See [LICENSE](LICENSE).
