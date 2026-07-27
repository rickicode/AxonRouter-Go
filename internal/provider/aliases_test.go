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

func TestRegistryHasMediaProviders(t *testing.T) {
	for _, id := range []string{"fal", "black-forest-labs", "assemblyai", "edge-tts", "cartesia", "elevenlabs", "deepgram"} {
		info, ok := Registry[id]
		if !ok {
			t.Fatalf("Registry missing entry for %q", id)
		}
		if info.DisplayName == "" {
			t.Errorf("Registry[%q].DisplayName is empty", id)
		}
	}
}

func TestResolveAlias_MediaProviders(t *testing.T) {
	cases := map[string]string{
		"blackforestlabs":   "black-forest-labs",
		"black_forest_labs": "black-forest-labs",
		"assembly-ai":       "assemblyai",
		"edgetts":           "edge-tts",
	}
	for alias, want := range cases {
		if got := ResolveAlias(alias); got != want {
			t.Errorf("ResolveAlias(%q) = %q, want %q", alias, got, want)
		}
	}
	for _, id := range []string{"fal", "black-forest-labs", "assemblyai", "edge-tts", "cartesia", "elevenlabs", "deepgram"} {
		if got := ResolveAlias(id); got != id {
			t.Errorf("ResolveAlias(%q) = %q, want %q", id, got, id)
		}
	}
}
