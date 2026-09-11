package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	fwprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

func TestProviderIsNamedForItsConfiguration(t *testing.T) {
	var resp fwprovider.MetadataResponse
	New("test").Metadata(context.Background(), fwprovider.MetadataRequest{}, &resp)

	if resp.TypeName != TypeName {
		t.Fatalf("TypeName = %q, want %q", resp.TypeName, TypeName)
	}
	if resp.Version != "test" {
		t.Fatalf("Version = %q, want the version it was built with", resp.Version)
	}
}

func TestProviderOffersTheActivationResource(t *testing.T) {
	got := New("test").Resources(context.Background())
	if len(got) != 1 {
		t.Fatalf("Resources() returned %d resources, want exactly the activation resource", len(got))
	}

	var resp resource.MetadataResponse
	got[0]().Metadata(context.Background(), resource.MetadataRequest{}, &resp)
	if resp.TypeName != ResourceTypeName {
		t.Fatalf("resource type = %q, want %q", resp.TypeName, ResourceTypeName)
	}
}

func schemaOf(t *testing.T) resource.SchemaResponse {
	t.Helper()
	var resp resource.SchemaResponse
	NewActivationResource().Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Schema() produced diagnostics: %v", resp.Diagnostics)
	}
	return resp
}

func TestSchemaAttributes(t *testing.T) {
	attrs := schemaOf(t).Schema.Attributes

	cases := []struct {
		name     string
		required bool
		computed bool
	}{
		{"closure", true, false},
		{"stream_id", true, false},
		{"relay", true, false},
		{"key_file", true, false},
		{"profile", false, true},
		{"tunnel_command", false, true},
		{"current_system", false, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, ok := attrs[tc.name]
			if !ok {
				t.Fatalf("the schema has no %q attribute", tc.name)
			}
			if a.IsRequired() != tc.required {
				t.Fatalf("%s required = %v, want %v", tc.name, a.IsRequired(), tc.required)
			}
			if a.IsComputed() != tc.computed {
				t.Fatalf("%s computed = %v, want %v", tc.name, a.IsComputed(), tc.computed)
			}
		})
	}

	if len(attrs) != len(cases) {
		t.Fatalf("the schema has %d attributes, want %d: an undocumented one has crept in",
			len(attrs), len(cases))
	}
}

func TestEveryAttributeExplainsItself(t *testing.T) {
	// The schema is the provider's documentation. An attribute nobody can read
	// the purpose of is one an operator will set wrongly.
	for name, a := range schemaOf(t).Schema.Attributes {
		if strings.TrimSpace(a.GetDescription()) == "" {
			t.Errorf("attribute %q has no description", name)
		}
	}
}

func TestCurrentSystemIsReadNotRemembered(t *testing.T) {
	// The whole justification for this being a resource rather than a
	// provisioner. Pin both the path and the fact that the description says so.
	if CurrentSystemLink != "/run/current-system" {
		t.Fatalf("CurrentSystemLink = %q, want /run/current-system", CurrentSystemLink)
	}

	desc := schemaOf(t).Schema.Attributes["current_system"].GetDescription()
	if !strings.Contains(desc, CurrentSystemLink) {
		t.Fatalf("current_system does not say where it is read from: %q", desc)
	}
}

func TestUnimplementedDetailNamesTheOperation(t *testing.T) {
	// Nothing uses this now that every operation behaves. It is kept, and
	// tested, because it is the message for whoever adds the next one: never
	// report success while doing nothing, since state describing a machine
	// nobody configured is worse than a failed apply.
	got := unimplementedDetail("Rollback")
	if !strings.Contains(got, "Rollback") {
		t.Fatalf("the message does not name the operation: %q", got)
	}
}

func TestProxyCommandIsTheWholeIntegrationSurface(t *testing.T) {
	tg := target{
		streamID:      "i-0abc123",
		relay:         "relay.example:7843",
		keyFile:       "/root/orchestrator.key",
		tunnelCommand: "nivis-tunnel",
	}

	want := "nivis-tunnel connect i-0abc123 --relay relay.example:7843 --key /root/orchestrator.key"
	if got := tg.proxyCommand(); got != want {
		t.Fatalf("proxyCommand() = %q, want %q", got, want)
	}
}

func TestSSHArgsPinTheHostKeyOnFirstUse(t *testing.T) {
	// accept-new rather than no, and the difference is load-bearing. Under AWS
	// SSM the tunnel authenticates the target, so ssh's check is redundant and
	// elastinix disables it. This tunnel does not authenticate the target, so
	// the check is what refuses an impostor on every connection after the
	// first.
	args := strings.Join(target{tunnelCommand: "nivis-tunnel"}.sshArgs(), " ")

	if !strings.Contains(args, "StrictHostKeyChecking=accept-new") {
		t.Fatalf("ssh args do not pin the host key: %q", args)
	}
	if strings.Contains(args, "StrictHostKeyChecking=no") {
		t.Fatal("host key checking is disabled; nothing would then authenticate the target")
	}
	if !strings.Contains(args, "BatchMode=yes") {
		t.Fatal("ssh may prompt; a provider has no terminal to prompt on")
	}
}

func TestCommandErrorCarriesWhatTheTargetSaid(t *testing.T) {
	// A generic "activation failed" cannot tell an operator whether the closure
	// was wrong, the disk was full, or a unit refused to start. The target's own
	// stderr is the only thing that answers that.
	err := &commandError{
		command: "switch-to-configuration switch",
		stderr:  "error: unit sshd.service failed to start",
		err:     errors.New("exit status 1"),
	}

	got := err.Error()
	for _, want := range []string{
		"switch-to-configuration switch",
		"exit status 1",
		"unit sshd.service failed to start",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("the error omits %q: %q", want, got)
		}
	}
}

func TestCommandErrorWithoutOutputStillReads(t *testing.T) {
	err := &commandError{command: "true", err: errors.New("exit status 255")}
	if got := err.Error(); !strings.Contains(got, "exit status 255") {
		t.Fatalf("Error() = %q", got)
	}
}

func TestDefaultsAreTheConventionalPaths(t *testing.T) {
	if DefaultProfile != "/nix/var/nix/profiles/system" {
		t.Fatalf("DefaultProfile = %q", DefaultProfile)
	}
	if DefaultTunnelCommand != "nivis-tunnel" {
		t.Fatalf("DefaultTunnelCommand = %q", DefaultTunnelCommand)
	}
}

func TestResourceImplementsModifyPlan(t *testing.T) {
	// Without ModifyPlan, Read is decorative: current_system is computed, so a
	// refresh discovering a different generation would update state quietly and
	// the next plan would report no changes. The resource would know about the
	// drift and do nothing — exactly what a null_resource does, and the reason
	// this is a resource instead.
	if _, ok := NewActivationResource().(resource.ResourceWithModifyPlan); !ok {
		t.Fatal("the resource does not implement ModifyPlan, so drift would never become a planned update")
	}
}

func TestNixSSHOptsSurvivesWordSplitting(t *testing.T) {
	// Regression, and an expensive one to find: NIX_SSHOPTS is a STRING nix
	// hands to a shell, not an argument vector, so it is word-split before ssh
	// sees it. The ProxyCommand is a whole command line and contains spaces, so
	// joining the arguments naively hands ssh `-o ProxyCommand=nivis-tunnel`
	// and then `connect` as a hostname.
	//
	// The direct ssh calls pass a real argument vector and were unaffected,
	// which is what made this show up only at nix-copy-closure.
	tg := target{
		streamID:      "i-0abc123",
		relay:         "relay.example:7843",
		keyFile:       "/root/orchestrator.key",
		tunnelCommand: "nivis-tunnel",
	}

	opts := tg.sshOptsEnv()

	// Simulate the shell's word splitting, respecting single quotes.
	fields := splitRespectingQuotes(opts)

	var proxy string
	for i, f := range fields {
		if f == "-o" && i+1 < len(fields) && strings.HasPrefix(fields[i+1], "ProxyCommand=") {
			proxy = strings.TrimPrefix(fields[i+1], "ProxyCommand=")
		}
	}

	if proxy != tg.proxyCommand() {
		t.Fatalf("after word splitting the ProxyCommand is %q, want %q", proxy, tg.proxyCommand())
	}
}

// splitRespectingQuotes is a minimal stand-in for shell word splitting: enough
// to prove the quoting holds the ProxyCommand together.
func splitRespectingQuotes(s string) []string {
	var fields []string
	var cur strings.Builder
	inQuote := false

	for _, r := range s {
		switch {
		case r == '\'':
			inQuote = !inQuote
		case r == ' ' && !inQuote:
			if cur.Len() > 0 {
				fields = append(fields, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		fields = append(fields, cur.String())
	}
	return fields
}
