package models

import "testing"

func TestCatalogContainsNewProviderModels(t *testing.T) {
	want := map[string][]string{
		"tavily":        {"tavily-search", "tavily-answer"},
		"brave":         {"brave-search"},
		"exa":           {"exa-search", "exa-answer"},
		"jina":          {"jina-search", "jina-reader"},
		"google-pse":    {"google-pse"},
		"firecrawl":     {"firecrawl-search", "firecrawl-fetch"},
		"fal":           {"fal-flux-schnell"},
		"black-forest-labs": {"flux-schnell"},
		"cartesia":      {"cartesia-sonic"},
		"qwen":          {"qwen3.8-max-preview"},
		"kimi-coding":   {"kimi-k2.7-code"},
		"comfyui":       {"comfyui-default"},
	}
	for prov, ids := range want {
		for _, id := range ids {
			kinds := GetModelServiceKinds(prov, id)
			if len(kinds) == 0 {
				t.Errorf("expected service_kinds for %s/%s, got none", prov, id)
			}
		}
	}
}
