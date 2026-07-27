package executor

import "testing"

func TestRegistryContainsNewProviders(t *testing.T) {
	want := []string{
		"brave", "tavily", "exa", "jina", "google-pse", "firecrawl",
		"fal", "black-forest-labs", "assemblyai", "cartesia", "edge-tts",
		"qwen", "alicode", "kimi-coding", "iflow", "volcengine-ark", "hunyuan",
		"nanobanana", "topaz", "puter", "comfyui",
	}
	for _, id := range want {
		if _, _, ok := GetRegistry().Get(id); !ok {
			t.Errorf("registry missing provider %q", id)
		}
	}
}
