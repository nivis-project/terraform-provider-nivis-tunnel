{
  description = "terraform-provider-nivis-tunnel — the nixos_activation resource";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  };

  outputs =
    { self, nixpkgs }:
    let
      # Enumerated in plain Nix on purpose; flake-utils is not used here.
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];

      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});

      version = "0.1.0-dev";

      # Pins the dependency tree. Stated once, so the provider binary and the
      # test derivation are built from the same tree — otherwise the gate would
      # be testing something other than what it ships.
      vendorHash = "sha256-HM6/k59eDfD7OpH5S3C8j6YN0Q0ina6KuKfrJVWyTB8=";
    in
    {
      packages = forAllSystems (pkgs: rec {
        terraform-provider-nivis-tunnel = pkgs.buildGoModule {
          pname = "terraform-provider-nivis-tunnel";
          inherit version;
          src = ./.;
          inherit vendorHash;
          subPackages = [ "cmd/terraform-provider-nivis-tunnel" ];
          ldflags = [
            "-s"
            "-w"
            "-X main.version=${version}"
          ];
          meta.mainProgram = "terraform-provider-nivis-tunnel";
        };
        default = terraform-provider-nivis-tunnel;
      });

      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          packages = [
            pkgs.go
            pkgs.gopls
            pkgs.go-tools
            pkgs.golangci-lint
            pkgs.jujutsu
            pkgs.opentofu
            pkgs.nixfmt-rfc-style
          ];
        };
      });

      checks = forAllSystems (pkgs: {
        build = self.packages.${pkgs.stdenv.hostPlatform.system}.default;

        # Run through buildGoModule rather than a bare `go test`: the Nix
        # sandbox has no network, so the tests must be built from the same
        # vendored tree as the binary. A hand-rolled runCommand would try to
        # fetch modules and fail.
        unit = pkgs.buildGoModule {
          pname = "terraform-provider-nivis-tunnel-tests";
          inherit version vendorHash;
          src = ./.;
          doCheck = true;
          installPhase = "touch $out";
        };

        fmt = pkgs.runCommand "nixfmt-check" { nativeBuildInputs = [ pkgs.nixfmt-rfc-style ]; } ''
          nixfmt --check ${./flake.nix} ${./nix/schema-check.nix} && touch $out
        '';

        # What an external tool actually receives over tfplugin6, as opposed to
        # what the Go code intends. Different claim, and the one that matters.
        schema = import ./nix/schema-check.nix {
          inherit pkgs;
          provider = self.packages.${pkgs.stdenv.hostPlatform.system}.default;
        };
      });

      formatter = forAllSystems (pkgs: pkgs.nixfmt-rfc-style);
    };
}
