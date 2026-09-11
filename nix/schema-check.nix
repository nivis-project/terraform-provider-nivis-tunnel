# A real OpenTofu starts the built provider and reads its schema.
#
# The unit tests assert the schema from inside the process, which proves what
# the Go code intends. This proves what an external tool actually receives over
# tfplugin6 — a different claim, and the one that matters, since nivis and
# OpenTofu are the only consumers this provider will ever have.
#
# The provider is offered through a filesystem mirror rather than a dev
# override, because `tofu providers schema` needs an initialised lock file and
# dev overrides deliberately bypass init. A mirror needs no network, which is
# what lets this run inside the Nix sandbox.
{ pkgs, provider }:
pkgs.runCommand "activation-provider-schema-check"
  {
    nativeBuildInputs = [
      pkgs.opentofu
      pkgs.jq
    ];
  }
  ''
    export HOME=$TMPDIR
    mirror="$HOME/mirror/registry.opentofu.org/nivis-project/nivis-tunnel/0.1.0/linux_amd64"
    mkdir -p work "$mirror"
    cp ${provider}/bin/terraform-provider-nivis-tunnel \
      "$mirror/terraform-provider-nivis-tunnel_v0.1.0"

    cat > "$HOME/cli.tfrc" <<EOF
    provider_installation {
      filesystem_mirror {
        path    = "$HOME/mirror"
        include = ["registry.opentofu.org/nivis-project/nivis-tunnel"]
      }
      direct { exclude = ["registry.opentofu.org/nivis-project/nivis-tunnel"] }
    }
    EOF
    export TF_CLI_CONFIG_FILE="$HOME/cli.tfrc"

    cd work
    cat > main.tf <<'EOF'
    terraform {
      required_providers {
        # The local name MUST be the resource type's prefix: OpenTofu resolves
        # the provider for `resource "nixos_activation"` by looking up "nixos"
        # here. Declaring it as anything else sends it to the public registry
        # looking for hashicorp/nixos. Same shape as google-beta, whose
        # resources are google_*.
        nixos = {
          source  = "nivis-project/nivis-tunnel"
          version = "0.1.0"
        }
      }
    }

    resource "nixos_activation" "target" {
      closure   = "/nix/store/0000000000000000000000000000000000000000-system"
      stream_id = "poc-target-01"
      relay     = "relay.example:7843"
      key_file  = "/root/orchestrator.key"
    }
    EOF

    tofu init -no-color -input=false > init.log 2>&1 || { cat init.log; exit 1; }
    tofu providers schema -json > schema.json

    echo "--- the resource is offered over tfplugin6 ---"
    jq -e '.provider_schemas
             | to_entries[0].value.resource_schemas["nixos_activation"]' schema.json > resource.json

    for attr in closure stream_id relay key_file current_system; do
      jq -e --arg a "$attr" '.block.attributes[$a]' resource.json > /dev/null \
        || { echo "the schema is missing attribute $attr"; exit 1; }
      # The schema is this provider's documentation. An attribute nobody can
      # read the purpose of is one an operator will set wrongly.
      jq -e --arg a "$attr" '.block.attributes[$a].description
                             | type == "string" and length > 0' resource.json > /dev/null \
        || { echo "attribute $attr has no description"; exit 1; }
      echo "  $attr: ok"
    done

    echo "--- required attributes are required ---"
    for attr in closure stream_id relay key_file; do
      jq -e --arg a "$attr" '.block.attributes[$a].required == true' resource.json > /dev/null \
        || { echo "$attr is not marked required"; exit 1; }
    done
    jq -e '.block.attributes.current_system.computed == true' resource.json > /dev/null \
      || { echo "current_system is not computed"; exit 1; }

    echo "--- a missing required attribute is refused by name ---"
    # The operator-facing half: a configuration that forgets something must say
    # which something, rather than failing somewhere downstream.
    cat > main.tf <<'EOF'
    terraform {
      required_providers {
        # The local name MUST be the resource type's prefix: OpenTofu resolves
        # the provider for `resource "nixos_activation"` by looking up "nixos"
        # here. Declaring it as anything else sends it to the public registry
        # looking for hashicorp/nixos. Same shape as google-beta, whose
        # resources are google_*.
        nixos = {
          source  = "nivis-project/nivis-tunnel"
          version = "0.1.0"
        }
      }
    }

    resource "nixos_activation" "target" {
      stream_id = "poc-target-01"
      relay     = "relay.example:7843"
      key_file  = "/root/orchestrator.key"
    }
    EOF

    if tofu validate -no-color > validate.log 2>&1; then
      echo "a configuration missing 'closure' was accepted"; cat validate.log; exit 1
    fi
    grep -q "closure" validate.log \
      || { echo "the refusal does not name the missing attribute"; cat validate.log; exit 1; }

    echo "all schema assertions passed"
    mkdir -p $out
    cp schema.json resource.json $out/
  ''
