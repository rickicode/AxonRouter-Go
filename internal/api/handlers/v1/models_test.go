package v1

import (
	"slices"
	"testing"
)

func TestGetProviderModels_CFIncludesServiceKinds(t *testing.T) {
	h := newTestHandler(t)
	models := h.getProviderModels("cf")
	if len(models) == 0 {
		t.Fatal("expected CF models from catalog")
	}

	find := func(id string) map[string]any {
		for _, m := range models {
			if gotID, ok := m["id"].(string); ok && gotID == id {
				return m
			}
		}
		return nil
	}

	llm := find("cf/meta/llama-3.2-1b-instruct")
	if llm == nil {
		t.Fatal("missing CF LLM model")
	}
	if kinds := kindsOf(llm); !slices.Contains(kinds, "llm") {
		t.Errorf("CF LLM service_kinds = %v, want llm", kinds)
	}

	emb := find("cf/baai/bge-base-en-v1.5")
	if emb == nil {
		t.Fatal("missing CF embedding model")
	}
	if kinds := kindsOf(emb); len(kinds) != 1 || kinds[0] != "embedding" {
		t.Errorf("CF embedding service_kinds = %v, want [embedding]", kinds)
	}

	img := find("cf/black-forest-labs/flux-1-schnell")
	if img == nil {
		t.Fatal("missing CF image model")
	}
	if kinds := kindsOf(img); len(kinds) != 1 || kinds[0] != "image" {
		t.Errorf("CF image service_kinds = %v, want [image]", kinds)
	}
}

func kindsOf(m map[string]any) []string {
	if v, ok := m["service_kinds"].([]string); ok {
		return v
	}
	if v, ok := m["service_kinds"].([]any); ok {
		out := make([]string, 0, len(v))
		for _, s := range v {
			if str, ok := s.(string); ok {
				out = append(out, str)
			}
		}
		return out
	}
	return nil
}
