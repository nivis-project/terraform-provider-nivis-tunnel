# AGENTS.md — terraform-provider-nivis-tunnel

## Overview

This is the OpenTofu/Terraform provider half of the **nivis-tunnel** project. It
carries one resource, `nixos_activation`, and that resource exists to make one
property true:

> Changing a live NixOS configuration must change **only this resource**.
> No new image. No new snapshot. No replaced server.

That is the image/live-config split, and it is the reason the whole project
exists. Without it, nivis can only rebuild a whole machine: a rotated secret
replaces the server, and any identity the machine generated at first boot dies
with it.

The transport lives in the companion repo
[`nivis-tunnel`](https://github.com/nivis-project/nivis-tunnel), which also
holds **all planning artifacts** — this repo has no `.beans` directory of its
own. Read the roadmap there.

### Why a provider and not a `null_resource`

`null_resource` with `triggers` knows only what it did last time. This resource
implements **Read**:

```
readlink /run/current-system   →  the generation actually running
```

so a manual `nixos-rebuild` on the target surfaces as drift in `plan` instead of
being silently overwritten. That is the entire justification for the resource
existing, and it is the one thing a shelled-out provisioner cannot do.

### Shape

```
closure   = drv liveSystem     # a nivis __build leaf, realised before apply
                               # → "the store path changed" IS the diff, free
target    = server.refAttr ".."# a ref → ordering after the server, free

Read      readlink /run/current-system
Apply     nix copy  →  nix-env --profile /nix/var/nix/profiles/system --set <path>
                    →  <path>/bin/switch-to-configuration switch
```

Transport is ssh, reached through the `ProxyCommand` the companion repo
provides. That seam is deliberate: `deploy-rs`, `nix-copy-closure` and plain ssh
all work over the tunnel unchanged.

nivis resolves providers by **filesystem path**, so no registry publication is
needed for the PoC. The repo is named for the registry convention only so that
publishing later requires no rename.

## Commands

```bash
# OpenSpec — planning lives in the shared `nivis` store, NOT in ./openspec
openspec context             # show which root/store resolves here
openspec doctor              # store registration health
/opsx:propose "<idea>"       # new proposal (Claude Code)

# Beans live in the companion repo
cd ../nivis-tunnel && beans roadmap

# Nix — the gate
nix flake check              # build + unit tests + formatting; must stay green
nix develop                  # dev shell: go, gopls, golangci-lint, jj, opentofu
nix build

# Go
go test ./...

# Version control: jj, colocated with git
jj st && jj describe -m "<subject>" && jj git push
```

## OpenSpec lives in a store

This repo declares `store: nivis` in `openspec/config.yaml`, so there is **no
local `openspec/specs` or `openspec/changes`** — they live in
`/home/pim/gh.nivis-project/ospecs`. `openspec init` creates those directories
by default; if `openspec doctor` warns that the store declaration is ignored,
one of them came back — delete it.

Every proposal names the bean id in `nivis-tunnel` that it implements. On
archive, update that bean with `openspec-link:` and set its status.

## Version control: jj

`jj` is colocated with git. Commit **after every OpenSpec change archival** —
one archived change, one commit. Commits are authored by **Pim Snel** alone:
never add `Co-authored-by`, `Generated with`, or any similar trailer.

Nix reads the git index, not the jj working copy. If `nix flake check` says a
file "is not tracked by Git", run `git add -A` first.

## Nix conventions

- **Do not use `flake-utils`.** Supported systems are enumerated in plain Nix
  and mapped with `nixpkgs.lib.genAttrs`.
- `nix flake check` is the gate and must be green before archiving a change.
- `vendorHash = null` holds only while `go.mod` has no external dependencies.
  Adding one makes `nix build` print the expected hash — put it in.

## Testing

Unit tests run in `nix flake check`. Every task that changes behaviour names the
test that proves it.

End-to-end tests live in `test/e2e` behind the `e2e` build tag, so they never
run in the sandboxed gate. The acceptance test for this repo is rung 3 of the
project's test ladder:

```
nivis apply → bootstrap image → server → activation
change liveSystem, apply again
  → ONLY the activation resource changes
  → no new snapshot, no replaced server
```

A test that asserts `switch-to-configuration` ran is not enough. The test must
assert what *did not* happen: that the image and server resources were
untouched. That negative is the claim.
