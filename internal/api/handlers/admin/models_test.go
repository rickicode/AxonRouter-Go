package admin

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/rickicode/AxonRouter-Go/internal/models"
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

func TestListModelEntries_CodeBuddyIncludesServiceKinds(t *testing.T) {
	h := &ModelHandler{}
	entries := h.listModelEntries("codebuddy", []string{"llm"}, nil, staticModels("codebuddy"), nil)
	if len(entries) == 0 {
		t.Fatal("expected CodeBuddy model entries")
	}
	find := func(id string) map[string]any {
		for _, e := range entries {
			if gotID, ok := e["id"].(string); ok && gotID == id {
				return e
			}
		}
		return nil
	}
	llm := find("codebuddy/glm-5.0")
	if llm == nil || !slices.Contains(kindsOf(llm), "llm") {
		t.Errorf("CodeBuddy glm-5.0 entry missing service_kinds llm: %#v", llm)
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

func TestListModelEntries_QwenCloudIncludesExpectedModels(t *testing.T) {
	h := &ModelHandler{}
	entries := h.listModelEntries("qwencloud", []string{"llm"}, nil, staticModels("qwencloud"), nil)
	if len(entries) == 0 {
		t.Fatal("expected QwenCloud model entries")
	}
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		if id, ok := e["id"].(string); ok {
			ids = append(ids, id)
		}
	}
	want := []string{
		"qwencloud/qwen3.7-plus",
		"qwencloud/qwen3.7-max",
		"qwencloud/qwen3.6-plus",
		"qwencloud/qwen3.6-max",
		"qwencloud/qwen3.6-flash",
		"qwencloud/qwen3.5-omni-plus",
		"qwencloud/qwen-plus",
		"qwencloud/glm-5.2",
		"qwencloud/deepseek-v4-flash",
		"qwencloud/qwen3-coder-plus",
	}
	for _, w := range want {
		if !slices.Contains(ids, w) {
			t.Errorf("QwenCloud entries missing %q; got %v", w, ids)
		}
	}
}

func TestDefaultTestModel_ZenMuxReturnsPaidHead(t *testing.T) {
	got := defaultTestModel("zenmux")
	// Prefer the historical paid head; fallbacks are acceptable when it is not in catalog.
	wantOptions := []string{"openai/gpt-5.6-luna", "openai/gpt-5.6-terra", "openai/gpt-5.6-sol"}
	for _, want := range wantOptions {
		if got == want {
			return
		}
	}
	t.Errorf("defaultTestModel(zenmux) = %q, want one of paid GPT-5.6 heads %v", got, wantOptions)
}

func TestDefaultTestModel_ZenMuxFreeReturnsFreeModel(t *testing.T) {
	got := defaultTestModel("zenmux-free")
	want := "inclusionai/ling-3.0-flash"
	if got != want {
		t.Errorf("defaultTestModel(zenmux-free) = %q, want %q", got, want)
	}
	freeModels := []string{
		"inclusionai/ling-3.0-flash",
		"z-ai/glm-4.7-flash-free",
		"x-ai/grok-4.5-free",
		"stepfun/step-3.7-flash-free",
		"z-ai/glm-4.6v-flash-free",
		"moonshotai/kimi-k3-free",
	}
	if !slices.Contains(freeModels, got) {
		t.Errorf("defaultTestModel(zenmux-free) returned non-free model %q; expected one of %v", got, freeModels)
	}
}

func TestDefaultTestModel_OCReturnsBareFreeModel(t *testing.T) {
	got := defaultTestModel("oc")
	if got == "" {
		t.Fatal("defaultTestModel(oc) returned empty")
	}
	// OC test model must NOT have the oc/ prefix; upstream strips it itself.
	if strings.HasPrefix(got, "oc/") {
		t.Fatalf("defaultTestModel(oc) returned prefixed model %q; want bare model name", got)
	}
	// Must be a free model from the catalog (dynamically selected).
	if !strings.Contains(got, "-free") {
		t.Errorf("defaultTestModel(oc) = %q; want a -free model from catalog", got)
	}
}

func TestDefaultTestModel_OCGoReturnsBareModel(t *testing.T) {
	got := defaultTestModel("oc-go")
	if got == "" {
		t.Fatal("defaultTestModel(oc-go) returned empty")
	}
	// oc-go upstream has no -free models; must still return a bare name.
	if strings.HasPrefix(got, "oc-go/") {
		t.Fatalf("defaultTestModel(oc-go) returned prefixed model %q; want bare model name", got)
	}
}

func TestDefaultTestModel_OCZenReturnsBareFreeModel(t *testing.T) {
	got := defaultTestModel("oc-zen")
	if got == "" {
		t.Fatal("defaultTestModel(oc-zen) returned empty")
	}
	if strings.HasPrefix(got, "oc-zen/") {
		t.Fatalf("defaultTestModel(oc-zen) returned prefixed model %q; want bare model name", got)
	}
	if !strings.Contains(got, "-free") {
		t.Errorf("defaultTestModel(oc-zen) = %q; want a -free model from catalog", got)
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

// TestDefaultTestModel_OCDynamicResolvesAndLocks verifies that, for the free
// OC provider, the test model is resolved from the live upstream /v1/models
// list (not the hardcoded embedded default) and that the discovered model is
// locked into the shared catalog for reuse.
func TestDefaultTestModel_OCDynamicResolvesAndLocks(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"oc/dynamic-free-model"},{"id":"oc/some-paid-model"}]}`))
	}))
	defer ts.Close()

	// Point OC at the mock and mark it free-only, restoring afterwards.
	origBase := noAuthBaseURLs["oc"]
	noAuthBaseURLs["oc"] = ts.URL
	defer func() { noAuthBaseURLs["oc"] = origBase }()

	origFree := models.ProviderFreeOnly()
	models.SetProviderFreeOnly(map[string]bool{"oc": true})
	defer models.SetProviderFreeOnly(origFree)

	// Capture and restore the catalog entry so the test does not leak state.
	origCatalog := models.GetAllModelIDs("oc")
	defer models.ReplaceProviderModelIDs("oc", origCatalog, nil)

	got := defaultTestModel("oc")
	if got != "dynamic-free-model" {
		t.Fatalf("defaultTestModel(oc) = %q, want dynamically-resolved %q", got, "dynamic-free-model")
	}

	// The dynamically discovered free model must be locked into the catalog.
	if !slices.Contains(staticModels("oc"), "oc/dynamic-free-model") {
		t.Errorf("catalog not locked with dynamic model; got %v", staticModels("oc"))
	}
}

// TestDefaultTestModel_OCFallsBackWhenUpstreamUnavailable verifies that when
// the live upstream cannot be reached, the test model still resolves from the
// embedded/synced catalog instead of failing outright.
func TestDefaultTestModel_OCFallsBackWhenUpstreamUnavailable(t *testing.T) {
	// Use a base URL that will refuse connections.
	origBase := noAuthBaseURLs["oc"]
	noAuthBaseURLs["oc"] = "http://127.0.0.1:1/v1"
	defer func() { noAuthBaseURLs["oc"] = origBase }()

	origFree := models.ProviderFreeOnly()
	models.SetProviderFreeOnly(map[string]bool{"oc": true})
	defer models.SetProviderFreeOnly(origFree)

	got := defaultTestModel("oc")
	if got == "" {
		t.Fatal("defaultTestModel(oc) returned empty when upstream unavailable; want fallback")
	}
	if strings.HasPrefix(got, "oc/") {
		t.Fatalf("defaultTestModel(oc) returned prefixed model %q; want bare model name", got)
	}
}
