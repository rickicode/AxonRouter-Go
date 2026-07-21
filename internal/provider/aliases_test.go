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

func TestRegistryHasQwenCloud(t *testing.T) {
	info, ok := Registry["qwencloud"]
	if !ok {
		t.Fatalf("Registry missing entry for %q", "qwencloud")
	}
	if info.DisplayName == "" {
		t.Errorf("Registry[%q].DisplayName is empty", "qwencloud")
	}
}

func TestResolveAlias_QwenCloud(t *testing.T) {
	if got := ResolveAlias("qwencloud"); got != "qwencloud" {
		t.Errorf("ResolveAlias(%q) = %q, want %q", "qwencloud", got, "qwencloud")
	}
}

func TestRegistryHasAmazonQ(t *testing.T) {
	info, ok := Registry["amazon-q"]
	if !ok {
		t.Fatalf("Registry missing entry for %q", "amazon-q")
	}
	if info.DisplayName == "" {
		t.Errorf("Registry[%q].DisplayName is empty", "amazon-q")
	}
	found := false
	for _, alias := range info.Aliases {
		if alias == "aq" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Registry[%q].Aliases missing %q; got %v", "amazon-q", "aq", info.Aliases)
	}
}

func TestResolveAlias_AmazonQ(t *testing.T) {
	if got := ResolveAlias("amazon-q"); got != "amazon-q" {
		t.Errorf("ResolveAlias(%q) = %q, want %q", "amazon-q", got, "amazon-q")
	}
}

func TestResolveAlias_AqAlias(t *testing.T) {
	if got := ResolveAlias("aq"); got != "amazon-q" {
		t.Errorf("ResolveAlias(%q) = %q, want %q", "aq", got, "amazon-q")
	}
}
