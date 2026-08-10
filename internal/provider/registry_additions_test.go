package provider

import "testing"

func TestRegistryHasNewProviders(t *testing.T) {
	want := []string{
		// search
		"brave", "tavily", "exa", "jina", "google-pse", "firecrawl",
		// media
		"fal", "black-forest-labs", "assemblyai", "cartesia", "edge-tts",
		// regional
		"qwen", "alicode", "kimi-coding", "iflow", "volcengine-ark", "hunyuan",
		// developer/niche
		"nanobanana", "topaz", "puter", "comfyui",
	}
	for _, id := range want {
		info, ok := Registry[id]
		if !ok {
			t.Fatalf("Registry missing entry for %q", id)
		}
		if info.DisplayName == "" {
			t.Errorf("Registry[%q].DisplayName is empty", id)
		}
		if got := ResolveAlias(id); got != id {
			t.Errorf("ResolveAlias(%q) = %q, want %q", id, got, id)
		}
	}
}

func TestResolveAlias_GooglePSE(t *testing.T) {
	if got := ResolveAlias("google-pse-search"); got != "google-pse" {
		t.Errorf("ResolveAlias(google-pse-search) = %q, want google-pse", got)
	}
}
