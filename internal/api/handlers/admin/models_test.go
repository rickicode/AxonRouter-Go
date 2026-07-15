package admin

import (
	"slices"
	"strings"
	"testing"
)

func TestDefaultTestModel_CloudflareStripsProviderPrefix(t *testing.T) {
	got := defaultTestModel("cf")
	if got == "" {
		t.Fatal("defaultTestModel(cf) returned empty")
	}
	if strings.HasPrefix(got, "cf/") {
		t.Fatalf("defaultTestModel(cf) returned full gateway ID %q; want model name without cf/ prefix", got)
	}
}

func TestListModelEntries_CFIncludesServiceKinds(t *testing.T) {
	h := &ModelHandler{}
	entries := h.listModelEntries("cf", nil, nil, staticModels("cf"), nil)
	if len(entries) == 0 {
		t.Fatal("expected CF model entries")
	}
	find := func(id string) map[string]any {
		for _, e := range entries {
			if gotID, ok := e["id"].(string); ok && gotID == id {
				return e
			}
		}
		return nil
	}
	if llm := find("cf/meta/llama-3.2-1b-instruct"); llm == nil || !slices.Contains(kindsOf(llm), "llm") {
		t.Errorf("CF LLM entry missing service_kinds llm: %#v", llm)
	}
	if emb := find("cf/baai/bge-base-en-v1.5"); emb == nil || !slices.Contains(kindsOf(emb), "embedding") {
		t.Errorf("CF embedding entry missing service_kinds embedding: %#v", emb)
	}
	if img := find("cf/black-forest-labs/flux-1-schnell"); img == nil || !slices.Contains(kindsOf(img), "image") {
		t.Errorf("CF image entry missing service_kinds image: %#v", img)
	}
}

func TestListModelEntries_FallsBackToSingleProviderServiceKind(t *testing.T) {
	h := &ModelHandler{}
	// Single-kind providers should inherit so their models don't land in "Other".
	entries := h.listModelEntries("claude", []string{"llm"}, nil, staticModels("claude"), nil)
	if len(entries) == 0 {
		t.Fatal("expected claude model entries")
	}
	for _, e := range entries {
		if !slices.Contains(kindsOf(e), "llm") {
			t.Errorf("expected entry %v to inherit provider service_kinds [llm], got %v", e["id"], e["service_kinds"])
		}
	}
}

func TestListModelEntries_MultiKindProviderDoesNotFallback(t *testing.T) {
	h := &ModelHandler{}
	// Multi-kind providers (like cf) should not blanket-tag unknown models with every kind.
	// Use an unknown provider with a static model and multi-kind service kinds.
	entries := h.listModelEntries("unknown-provider", []string{"llm", "embedding", "image"}, nil, []string{"fake-model"}, nil)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if _, ok := entries[0]["service_kinds"]; ok {
		t.Errorf("expected no service_kinds fallback for multi-kind provider, got %v", entries[0]["service_kinds"])
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
