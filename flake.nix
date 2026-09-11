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
    in
    {
      packages = forAllSystems (pkgs: rec {
        terraform-provider-nivis-tunnel = pkgs.buildGoModule {
          pname = "terraform-provider-nivis-tunnel";
          inherit version;
          src = ./.;
          vendorHash = null;
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

        unit = pkgs.runCommand "go-test" { nativeBuildInputs = [ pkgs.go ]; } ''
          export HOME=$TMPDIR
          export GOFLAGS=-mod=mod
          export GOCACHE=$TMPDIR/go-cache
          cp -r ${./.} src && chmod -R +w src && cd src
          go test ./... 2>&1 | tee $out
        '';

        fmt = pkgs.runCommand "nixfmt-check" { nativeBuildInputs = [ pkgs.nixfmt-rfc-style ]; } ''
          nixfmt --check ${./flake.nix} && touch $out
        '';
      });

      formatter = forAllSystems (pkgs: pkgs.nixfmt-rfc-style);
    };
}
