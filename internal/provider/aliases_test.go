package provider

import "testing"

func TestRegistryHasDevinAndQoder(t *testing.T) {
	for _, id := range []string{"devin", "qoder"} {
		info, ok := Registry[id]
		if !ok {
			t.Fatalf("Registry missing entry for %q", id)
		}
		if info.DisplayName == "" {
			t.Errorf("Registry[%q].DisplayName is empty", id)
		}
	}
}

func TestResolveAlias_DevinQoder(t *testing.T) {
	for _, id := range []string{"devin", "qoder"} {
		if got := ResolveAlias(id); got != id {
			t.Errorf("ResolveAlias(%q) = %q, want %q", id, got, id)
		}
	}
}