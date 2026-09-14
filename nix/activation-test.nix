# A real OpenTofu drives the provider against a real target through the tunnel.
#
# The claim this whole project exists for is a negative one: changing a live
# NixOS configuration changes ONLY this resource. No image, no snapshot, no
# replaced server. That negative cannot be asserted from a unit test, because
# there is no machine for nothing to happen to.
#
# So: three machines, a relay, a target whose firewall admits nothing, and an
# operator running tofu. What is activated is a small fake closure rather than a
# real NixOS system — building one inside a VM would test nixpkgs, which is
# already proven by every NixOS machine in existence. What is under test here is
# the provider's three steps: copy the closure, set the profile, switch.
{
  self,
  pkgs,
  tunnel,
}:
let
  orchestratorPublicKey = "Gt+ivBgkZNNSQBvcRUX6LhhlGVfUupW+IZTu0JcWQ3A=";
  orchestratorPrivateKey = "/z1GJeSYMj+cr57Rfz64DRiY2dlV7IMZWpkTxNoQNBU=";

  sshPublicKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIMPR3dZ3NnclYn3W4znSqC0dMjej/BEVxdJctSnR7C9y nivis-tunnel vm test";
  sshPrivateKey = ''
    -----BEGIN OPENSSH PRIVATE KEY-----
    b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtzc2gtZW
    QyNTUxOQAAACDD0d3WdzZ3JWJ91uM50qgtHTI3o/wRFcXSXLUp0ewvcgAAAJjzVOzr81Ts
    6wAAAAtzc2gtZWQyNTUxOQAAACDD0d3WdzZ3JWJ91uM50qgtHTI3o/wRFcXSXLUp0ewvcg
    AAAEBZdBRavNK8QtEY8pLh1kehwoGgtkve7/elHlCIIb9cBsPR3dZ3NnclYn3W4znSqC0d
    Mjej/BEVxdJctSnR7C9yAAAAFG5pdmlzLXR1bm5lbCB2bSB0ZXN0AQ==
    -----END OPENSSH PRIVATE KEY-----
  '';

  streamID = "poc-target-01";
  relayPort = 7843;

  system = pkgs.stdenv.hostPlatform.system;
in
pkgs.testers.runNixOSTest {
  name = "nivis-tunnel-activation";

  nodes = {
    relay =
      { ... }:
      {
        systemd.services.nivis-tunnel-relay = {
          wantedBy = [ "multi-user.target" ];
          after = [ "network-online.target" ];
          wants = [ "network-online.target" ];
          serviceConfig = {
            ExecStart = "${tunnel.packages.${system}.relay}/bin/relay --listen=:${toString relayPort}";
            Restart = "always";
            DynamicUser = true;
          };
        };
        networking.firewall.allowedTCPPorts = [ relayPort ];
      };

    target =
      { ... }:
      {
        imports = [ tunnel.nixosModules.default ];

        services.nivis-tunnel-agent = {
          enable = true;
          relay = "relay:${toString relayPort}";
          streamId = streamID;
          inherit orchestratorPublicKey;
        };

        services.openssh = {
          enable = true;
          settings.PasswordAuthentication = false;
          # Port lists merge rather than override; this is what actually keeps
          # the machine closed.
          openFirewall = false;
        };
        users.users.root.openssh.authorizedKeys.keys = [ sshPublicKey ];
        networking.firewall.allowedTCPPorts = [ ];

        virtualisation.writableStore = true;
        nix.settings.trusted-users = [ "root" ];

        # A test machine boots through qemu's -kernel, so it has no bootloader
        # and needs none. Leaving grub enabled makes switch-to-configuration try
        # to install one, which fails on the test disk with "will not proceed
        # with blocklists" — an artefact of the environment rather than anything
        # about activation.
        boot.loader.grub.enable = false;

        # A second, complete system closure to activate. Earlier attempts used
        # a hand-made directory containing only bin/switch-to-configuration,
        # and it kept losing fights with NixOS: /etc/static resolves through
        # /run/current-system, so pointing that at a closure without an etc
        # takes the machine's authorized_keys with it and the next connection
        # is refused. A specialisation is a real system, so nothing has to be
        # faked and nothing breaks.
        #
        # It deliberately does not touch the agent's unit. switch-to-configuration
        # restarts units whose definitions changed, and restarting the agent
        # would cut the very tunnel the activation is travelling over.
        specialisation.variant.configuration = {
          environment.etc."nivis-tunnel-generation".text = "variant\n";
        };
      };

    orchestrator =
      { nodes, ... }:
      {
        # Both generations must be VALID in this machine's Nix database, not
        # merely present on disk. NixOS test nodes share the host's store, so
        # the paths are visible either way — but nix-copy-closure asks its own
        # database, and the target's system closure is not in the
        # orchestrator's. Without this the copy fails with "path is not valid"
        # while the path plainly exists.
        virtualisation.additionalPaths = [
          nodes.target.system.build.toplevel
          nodes.target.specialisation.variant.configuration.system.build.toplevel
        ];

        environment.systemPackages = [
          self.packages.${system}.default
          tunnel.packages.${system}.nivis-tunnel
          pkgs.opentofu
          pkgs.openssh
          pkgs.jq
        ];

        virtualisation.writableStore = true;
        virtualisation.memorySize = 2048;

        environment.etc."nivis-tunnel-test/id_ed25519" = {
          text = sshPrivateKey;
          mode = "0600";
        };
        environment.etc."nivis-tunnel-test/orchestrator.key" = {
          text = orchestratorPrivateKey + "\n";
          mode = "0600";
        };
      };
  };

  testScript =
    { nodes, ... }:
    ''
      start_all()

      relay.wait_for_unit("nivis-tunnel-relay.service")
      relay.wait_for_open_port(${toString relayPort})
      target.wait_for_unit("nivis-tunnel-agent.service")
      target.wait_for_unit("sshd.service")
      orchestrator.wait_for_unit("multi-user.target")

      orchestrator.succeed("mkdir -p /root/.ssh /root/work /root/mirror")
      orchestrator.succeed(
          "install -m600 /etc/nivis-tunnel-test/id_ed25519 /root/.ssh/id_ed25519"
      )
      orchestrator.succeed(
          "install -m600 /etc/nivis-tunnel-test/orchestrator.key /root/orchestrator.key"
      )

      # Offer the provider through a filesystem mirror: no network in here, and a
      # dev override would skip the init that `tofu plan` needs.
      mirror = "/root/mirror/registry.opentofu.org/nivis-project/nivis-tunnel/0.1.0/linux_amd64"
      orchestrator.succeed(f"mkdir -p {mirror}")
      orchestrator.succeed(
          "cp ${self.packages.${system}.default}/bin/terraform-provider-nivis-tunnel "
          f"{mirror}/terraform-provider-nivis-tunnel_v0.1.0"
      )
      orchestrator.succeed(
          "cat > /root/cli.tfrc <<'EOF'\n"
          "provider_installation {\n"
          '  filesystem_mirror {\n'
          '    path    = "/root/mirror"\n'
          '    include = ["registry.opentofu.org/nivis-project/nivis-tunnel"]\n'
          "  }\n"
          '  direct { exclude = ["registry.opentofu.org/nivis-project/nivis-tunnel"] }\n'
          "}\n"
          "EOF"
      )

      # Two real system closures. The base is what the machine booted; the
      # variant is a specialisation of it, differing in one observable file.
      # Switching between complete systems is a supported NixOS operation, which
      # is why this stopped needing anything faked.
      base = "${nodes.target.system.build.toplevel}"
      variant = "${nodes.target.specialisation.variant.configuration.system.build.toplevel}"
      print(f"base system:    {base}")
      print(f"variant system: {variant}")
      assert base != variant, "the two generations are the same closure"

      def write_config(closure):
          orchestrator.succeed(
              "cat > /root/work/main.tf <<'EOF'\n"
              "terraform {\n"
              "  required_providers {\n"
              "    nixos = {\n"
              '      source  = "nivis-project/nivis-tunnel"\n'
              '      version = "0.1.0"\n'
              "    }\n"
              "  }\n"
              "}\n"
              "\n"
              'resource "nixos_activation" "target" {\n'
              f'  closure        = "{closure}"\n'
              f'  stream_id      = "${streamID}"\n'
              f'  relay          = "relay:${toString relayPort}"\n'
              '  key_file       = "/root/orchestrator.key"\n'
                "}\n"
              "EOF"
          )

      def tofu(args, check=True):
          cmd = (
              "cd /root/work && TF_CLI_CONFIG_FILE=/root/cli.tfrc "
              "TF_IN_AUTOMATION=1 HOME=/root "
              f"timeout 600 tofu {args}"
          )
          return orchestrator.succeed(cmd) if check else orchestrator.execute(cmd)

      with subtest("the target admits nothing directly"):
          # Without this, everything below could be happening over a route that
          # would not exist in the deployment this models.
          orchestrator.fail(
              "timeout 10 ssh -o StrictHostKeyChecking=no -o ConnectTimeout=5 "
              "-i /root/.ssh/id_ed25519 root@target true"
          )

      write_config(variant)
      tofu("init -no-color -input=false")

      with subtest("apply activates the variant on the target"):
          tofu("apply -no-color -input=false -auto-approve")

          # The three steps, asserted one at a time on the machine itself.
          # readlink -f, not readlink: the system profile is a symlink to a
          # generation link (system-N-link), which is itself the symlink to the
          # store. Only the full resolution names the closure.
          profile = target.succeed("readlink -f /nix/var/nix/profiles/system").strip()
          assert profile == variant, f"the profile points at {profile}, want {variant}"
          current = target.succeed("readlink /run/current-system").strip()
          assert current == variant, f"{current} is running, want {variant}"
          marker = target.succeed("cat /etc/nivis-tunnel-generation").strip()
          assert marker == "variant", f"switch-to-configuration did not take effect: {marker}"

      with subtest("the machine is still reachable after activating"):
          # A switch restarts units whose definitions changed. If it ever
          # restarted the agent, it would cut the tunnel the activation itself is
          # travelling over — so this is not a formality.
          target.succeed("systemctl is-active nivis-tunnel-agent")
          target.succeed("systemctl is-active sshd")

      with subtest("state records what is running, read back from the machine"):
          current = orchestrator.succeed(
              "cd /root/work && TF_CLI_CONFIG_FILE=/root/cli.tfrc HOME=/root tofu show -json "
              "| jq -r '.values.root_module.resources[0].values.current_system'"
          ).strip()
          assert current == variant, f"current_system = {current}, want {variant}"

      with subtest("a second plan is empty"):
          # What is running is what is configured; nothing is owed.
          rc, out = tofu("plan -no-color -input=false -detailed-exitcode", check=False)
          assert rc == 0, f"a converged resource still planned changes (exit {rc}):\n{out}"

      with subtest("changing the closure activates the other generation"):
          write_config(base)
          tofu("apply -no-color -input=false -auto-approve")

          current = target.succeed("readlink /run/current-system").strip()
          assert current == base, f"{current} is running, want {base}"
          target.fail("test -e /etc/nivis-tunnel-generation")

      with subtest("a change made by hand on the target shows as drift"):
          # The entire justification for this being a resource rather than a
          # provisioner. A null_resource with triggers knows only what it did last
          # time and would report no changes here, silently overwriting whoever
          # made the change.
          target.succeed(f"{variant}/bin/switch-to-configuration switch")
          assert target.succeed("readlink /run/current-system").strip() == variant

          rc, out = tofu("plan -no-color -input=false -detailed-exitcode", check=False)
          assert rc == 2, (
              f"a target running the wrong generation planned no changes (exit {rc}):\n{out}"
          )

      with subtest("applying the drift puts the configured generation back"):
          tofu("apply -no-color -input=false -auto-approve")
          current = target.succeed("readlink /run/current-system").strip()
          assert current == base, f"after converging, {current} is running, want {base}"

      with subtest("a failing activation surfaces the target's own error"):
          # "Activation failed" tells an operator nothing. What the machine said
          # is the only thing that does.
          write_config("/nix/store/0000000000000000000000000000000000000000-no-such-system")
          rc, out = tofu("apply -no-color -input=false -auto-approve", check=False)
          assert rc != 0, "an activation of a closure that does not exist was reported as success"
          assert "0000000000000000" in out, (
              f"the failure does not name what could not be activated:\n{out}"
          )

      with subtest("destroy leaves the machine running what it was running"):
          # Removing a resource from state is a statement about what is managed,
          # not an instruction to tear down a running system.
          write_config(base)
          tofu("apply -no-color -input=false -auto-approve")
          before = target.succeed("readlink /run/current-system").strip()

          tofu("destroy -no-color -input=false -auto-approve")

          after = target.succeed("readlink /run/current-system").strip()
          assert after == before, f"destroy changed the running system from {before} to {after}"
          target.succeed("systemctl is-active nivis-tunnel-agent")
    '';
}
