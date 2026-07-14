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
	entries := h.listModelEntries("cf", nil, staticModels("cf"))
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
