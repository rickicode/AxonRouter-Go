package v1

import (
	"encoding/json"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
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

// TestGetProviderModels_CodeBuddyIncludesExpectedModels verifies the codebuddy
// provider catalog exposes the seeded models with correct ownership.
func TestGetProviderModels_CodeBuddyIncludesExpectedModels(t *testing.T) {
	h := newTestHandler(t)
	models := h.getProviderModels("codebuddy")
	if len(models) == 0 {
		t.Fatal("expected codebuddy models from catalog")
	}

	want := map[string]string{
		"codebuddy/glm-5.0": "tencent",
		"codebuddy/kimi-k2.6": "tencent",
	}
	for id, owner := range want {
		found := false
		for _, m := range models {
			gotID, ok := m["id"].(string)
			if !ok || gotID != id {
				continue
			}
			found = true
			if gotOwner, _ := m["owned_by"].(string); gotOwner != owner {
				t.Errorf("model %q owned_by = %q, want %q", id, gotOwner, owner)
			}
			if kinds := kindsOf(m); !slices.Contains(kinds, "llm") {
				t.Errorf("model %q service_kinds = %v, want llm", id, kinds)
			}
		}
		if !found {
			t.Errorf("missing codebuddy model %q", id)
		}
	}
}

// TestGetProviderModels_GrokCLIIncludesExpectedModels verifies the grok-cli
// provider catalog exposes the seeded models with correct ownership.
func TestGetProviderModels_GrokCLIIncludesExpectedModels(t *testing.T) {
	h := newTestHandler(t)
	models := h.getProviderModels("grok-cli")
	if len(models) == 0 {
		t.Fatal("expected grok-cli models from catalog")
	}

	want := map[string]string{
		"grok-cli/grok-build":       "xai",
		"grok-cli/grok-4.5":         "xai",
		"grok-cli/grok-4.5-high":    "xai",
		"grok-cli/grok-4.5-medium":  "xai",
		"grok-cli/grok-4.5-low":     "xai",
	}
	for id, owner := range want {
		found := false
		for _, m := range models {
			gotID, ok := m["id"].(string)
			if !ok || gotID != id {
				continue
			}
			found = true
			if gotOwner, _ := m["owned_by"].(string); gotOwner != owner {
				t.Errorf("model %q owned_by = %q, want %q", id, gotOwner, owner)
			}
			if kinds := kindsOf(m); !slices.Contains(kinds, "llm") {
				t.Errorf("model %q service_kinds = %v, want llm", id, kinds)
			}
		}
		if !found {
			t.Errorf("missing grok-cli model %q", id)
		}
	}
}

func TestFilterAllowedModels(t *testing.T) {
	all := []gin.H{
		{"id": "openai/gpt-4o"},
		{"id": "openai/gpt-4o-mini"},
		{"id": "claude/claude-sonnet-4"},
		{"id": "smart/auto"},
		{"id": "my-combo"},
	}

	ids := func(ms []gin.H) []string {
		out := make([]string, 0, len(ms))
		for _, m := range ms {
			out = append(out, m["id"].(string))
		}
		return out
	}

	t.Run("empty allowed keeps all", func(t *testing.T) {
		got := filterAllowedModels(all, nil)
		want := []string{"openai/gpt-4o", "openai/gpt-4o-mini", "claude/claude-sonnet-4", "smart/auto", "my-combo"}
		if !slices.Equal(ids(got), want) {
			t.Errorf("got %v, want %v", ids(got), want)
		}
	})

	t.Run("filters by full id", func(t *testing.T) {
		allowed := map[string]struct{}{"openai/gpt-4o": {}}
		got := filterAllowedModels(all, allowed)
		want := []string{"openai/gpt-4o"}
		if !slices.Equal(ids(got), want) {
			t.Errorf("got %v, want %v", ids(got), want)
		}
	})

	t.Run("filters by provider prefix", func(t *testing.T) {
		allowed := map[string]struct{}{"openai": {}}
		got := filterAllowedModels(all, allowed)
		want := []string{"openai/gpt-4o", "openai/gpt-4o-mini"}
		if !slices.Equal(ids(got), want) {
			t.Errorf("got %v, want %v", ids(got), want)
		}
	})

	t.Run("filters by mix of id and prefix", func(t *testing.T) {
		allowed := map[string]struct{}{
			"claude":          {},
			"openai/gpt-4o-mini": {},
		}
		got := filterAllowedModels(all, allowed)
		want := []string{"openai/gpt-4o-mini", "claude/claude-sonnet-4"}
		if !slices.Equal(ids(got), want) {
			t.Errorf("got %v, want %v", ids(got), want)
		}
	})

	t.Run("no match returns empty", func(t *testing.T) {
		allowed := map[string]struct{}{"gemini": {}}
		got := filterAllowedModels(all, allowed)
		if len(got) != 0 {
			t.Errorf("got %v, want empty", ids(got))
		}
	})
}

func TestModels_AllowedModelsContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newTestHandler(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("allowed_models", map[string]struct{}{"smart": {}})

	h.Models(c)

	resp := w.Result()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body struct {
		Data []gin.H `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(body.Data) == 0 {
		t.Fatal("expected filtered models, got none")
	}
	for _, m := range body.Data {
		id, _ := m["id"].(string)
		if !strings.HasPrefix(id, "smart/") {
			t.Errorf("unexpected model id in filtered response: %q", id)
		}
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
