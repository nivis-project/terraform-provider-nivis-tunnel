package provider

import "testing"

func TestCurrentSystemLinkIsTheReadSource(t *testing.T) {
	// Read is the whole justification for this resource existing rather than a
	// null_resource with triggers; pin the path it depends on.
	if CurrentSystemLink != "/run/current-system" {
		t.Fatalf("CurrentSystemLink = %q, want /run/current-system", CurrentSystemLink)
	}
}
