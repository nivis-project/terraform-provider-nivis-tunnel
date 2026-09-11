package provider

import (
	"context"
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

func TestUnimplementedOperationsRefuseRatherThanPretend(t *testing.T) {
	// A provider that reports success while doing nothing writes state
	// describing a machine nobody configured, and the next plan believes it.
	// Failing is strictly better.
	r := NewActivationResource()
	ctx := context.Background()

	t.Run("create", func(t *testing.T) {
		var resp resource.CreateResponse
		r.Create(ctx, resource.CreateRequest{}, &resp)
		assertRefused(t, resp.Diagnostics.HasError(), resp.Diagnostics.Errors()[0].Detail(), "Create")
	})
	t.Run("read", func(t *testing.T) {
		var resp resource.ReadResponse
		r.Read(ctx, resource.ReadRequest{}, &resp)
		assertRefused(t, resp.Diagnostics.HasError(), resp.Diagnostics.Errors()[0].Detail(), "Read")
	})
	t.Run("update", func(t *testing.T) {
		var resp resource.UpdateResponse
		r.Update(ctx, resource.UpdateRequest{}, &resp)
		assertRefused(t, resp.Diagnostics.HasError(), resp.Diagnostics.Errors()[0].Detail(), "Update")
	})
	t.Run("delete", func(t *testing.T) {
		var resp resource.DeleteResponse
		r.Delete(ctx, resource.DeleteRequest{}, &resp)
		assertRefused(t, resp.Diagnostics.HasError(), resp.Diagnostics.Errors()[0].Detail(), "Delete")
	})
}

func assertRefused(t *testing.T, hasError bool, detail, op string) {
	t.Helper()
	if !hasError {
		t.Fatalf("%s reported success while doing nothing", op)
	}
	if !strings.Contains(detail, op) {
		t.Fatalf("the diagnostic does not name the operation %q: %q", op, detail)
	}
}
