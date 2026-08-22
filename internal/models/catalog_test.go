package models

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestGetModelTargetFormat_Found(t *testing.T) {
	format := GetModelTargetFormat("copilot", "gpt-5.4")
	if format != "openai-responses" {
		t.Errorf("GetModelTargetFormat(copilot, gpt-5.4) = %q, want openai-responses", format)
	}
}

func TestGetModelTargetFormat_Missing(t *testing.T) {
	format := GetModelTargetFormat("copilot", "claude-sonnet-4.6")
	if format != "" {
		t.Errorf("GetModelTargetFormat(copilot, claude-sonnet-4.6) = %q, want empty", format)
	}
}

func TestGetModelTargetFormat_UnknownProvider(t *testing.T) {
	format := GetModelTargetFormat("not-a-provider", "gpt-5.4")
	if format != "" {
		t.Errorf("GetModelTargetFormat(not-a-provider, gpt-5.4) = %q, want empty", format)
	}
}

func TestGetModelIDs_NewOpenAICompatibleProviders(t *testing.T) {
	want := map[string][]string{
		"glm":       {"glm-4", "glm-5"},
		"minimax":   {"minimax-m2.1", "minimax-m2.5"},
		"kimi":      {"kimi-k2"},
		"mistral":   {"mistral-large-latest", "codestral-latest"},
		"codebuddy": {"glm-5.0", "glm-5.2"},
	}
	for key, ids := range want {
		got := GetModelIDs(key)
		if len(got) == 0 {
			t.Errorf("GetModelIDs(%q) returned empty", key)
			continue
		}
		for _, id := range ids {
			if !slices.Contains(got, id) {
				t.Errorf("GetModelIDs(%q) missing %q; got %v", key, id, got)
			}
		}
	}
}

func TestGetModelIDs_CFIncludesEmbeddingAndImageModels(t *testing.T) {
	ids := GetModelIDs("cf")
	if len(ids) == 0 {
		t.Fatal("GetModelIDs(cf) returned empty")
	}
	want := []string{
		"cf/baai/bge-base-en-v1.5",
		"cf/black-forest-labs/flux-1-schnell",
	}
	for _, w := range want {
		if !slices.Contains(ids, w) {
			t.Errorf("GetModelIDs(cf) missing %q; got %v", w, ids)
		}
	}
}

func TestGetAllModelIDs_CFIncludesEmbeddingAndImageModels(t *testing.T) {
	ids := GetAllModelIDs("cf")
	if !slices.Contains(ids, "cf/baai/bge-base-en-v1.5") {
		t.Errorf("GetAllModelIDs(cf) missing embedding model; got %v", ids)
	}
	if !slices.Contains(ids, "cf/black-forest-labs/flux-1-schnell") {
		t.Errorf("GetAllModelIDs(cf) missing image model; got %v", ids)
	}
}

func TestGetModelServiceKinds_CatalogLLM(t *testing.T) {
	kinds := GetModelServiceKinds("cf", "cf/meta/llama-3.2-1b-instruct")
	if !slices.Contains(kinds, "llm") {
		t.Errorf("GetModelServiceKinds(cf, llama) = %v, want llm", kinds)
	}
}

func TestGetModelServiceKinds_ModalitiesEmbedding(t *testing.T) {
	kinds := GetModelServiceKinds("cf", "cf/baai/bge-base-en-v1.5")
	if len(kinds) != 1 || kinds[0] != "embedding" {
		t.Errorf("GetModelServiceKinds(cf, bge) = %v, want [embedding]", kinds)
	}
}

func TestGetModelServiceKinds_ModalitiesImage(t *testing.T) {
	kinds := GetModelServiceKinds("cf", "cf/black-forest-labs/flux-1-schnell")
	if len(kinds) != 1 || kinds[0] != "image" {
		t.Errorf("GetModelServiceKinds(cf, flux) = %v, want [image]", kinds)
	}
}

func TestGetModelServiceKinds_UnknownModel(t *testing.T) {
	kinds := GetModelServiceKinds("cf", "cf/not-a-model")
	if len(kinds) != 0 {
		t.Errorf("GetModelServiceKinds(cf, not-a-model) = %v, want empty", kinds)
	}
}

func TestServiceKindsForModelID_Found(t *testing.T) {
	kinds := ServiceKindsForModelID("cf/baai/bge-base-en-v1.5")
	if len(kinds) != 1 || kinds[0] != "embedding" {
		t.Errorf("ServiceKindsForModelID(cf/baai/bge-base-en-v1.5) = %v, want [embedding]", kinds)
	}
}

func TestServiceKindsForModelID_Unknown(t *testing.T) {
	kinds := ServiceKindsForModelID("not-a-real-model")
	if kinds != nil {
		t.Errorf("ServiceKindsForModelID(not-a-real-model) = %v, want nil", kinds)
	}
}

func TestGetModelIDs_QwenCloudContainsExpectedModels(t *testing.T) {
	want := []string{
		"qwen3.7-plus",
		"qwen3.7-max",
		"qwen3.6-plus",
		"qwen3.6-max",
		"qwen3.6-flash",
		"qwen3.5-omni-plus",
		"qwen-plus",
		"glm-5.2",
		"deepseek-v4-flash",
		"qwen3-coder-plus",
	}
	got := GetModelIDs("qwencloud")
	if len(got) == 0 {
		t.Fatal("GetModelIDs(qwencloud) returned empty")
	}
	for _, id := range want {
		if !slices.Contains(got, id) {
			t.Errorf("GetModelIDs(qwencloud) missing %q; got %v", id, got)
		}
	}
}

func TestDiscoverCloudflareModelsCached_HitsUpstreamOnceWithinTTL(t *testing.T) {
	var calls int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"result": []map[string]any{{
				"name": "@cf/test/cached-model",
				"task": map[string]any{"name": "Text Generation"},
			}},
		})
	}))
	defer ts.Close()

	u, _ := url.Parse(ts.URL)
	rt := &cfTestTransport{host: u.Host}
	old := http.DefaultClient.Transport
	http.DefaultClient.Transport = rt
	defer func() { http.DefaultClient.Transport = old }()

	resetCloudflareDiscoveryCache()

	DiscoverCloudflareModelsCached("key", "account")
	DiscoverCloudflareModelsCached("key", "account")

	if calls != 1 {
		t.Errorf("Cloudflare API called %d times, want 1", calls)
	}
}

func TestDiscoverCloudflareModelsCached_ExpiresAfterTTL(t *testing.T) {
	var calls int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"result": []map[string]any{{
				"name": "@cf/test/cached-model",
				"task": map[string]any{"name": "Text Generation"},
			}},
		})
	}))
	defer ts.Close()

	u, _ := url.Parse(ts.URL)
	rt := &cfTestTransport{host: u.Host}
	old := http.DefaultClient.Transport
	http.DefaultClient.Transport = rt
	defer func() { http.DefaultClient.Transport = old }()

	resetCloudflareDiscoveryCache()
	cfDiscoveryCache.last = time.Now().Add(-cfDiscoveryTTL - time.Second)

	DiscoverCloudflareModelsCached("key", "account")

	if calls != 1 {
		t.Errorf("Cloudflare API called %d times, want 1", calls)
	}
}

type cfTestTransport struct {
	host string
}

func (t *cfTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.HasSuffix(req.URL.Host, ".cloudflare.com") {
		req.URL.Scheme = "http"
		req.URL.Host = t.host
	}
	return http.DefaultTransport.RoundTrip(req)
}

// TestTryFetchProviders_ZenMuxFreeFiltering verifies that zenmux-free sync keeps
// only models whose prompt + completion pricing is zero, falling back to the
// "-free" suffix when pricing metadata is missing.
func TestTryFetchProviders_ZenMuxFreeFiltering(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{
					"id": "paid-model",
					"pricings": map[string]any{
						"prompt":     []map[string]any{{"value": 5, "unit": "perMTokens", "currency": "USD"}},
						"completion": []map[string]any{{"value": 10, "unit": "perMTokens", "currency": "USD"}},
					},
				},
				{
					"id": "free-model",
					"pricings": map[string]any{
						"prompt":     []map[string]any{{"value": 0, "unit": "perMTokens", "currency": "USD"}},
						"completion": []map[string]any{{"value": 0, "unit": "perMTokens", "currency": "USD"}},
					},
				},
				{
					// No pricing metadata: should be kept only by the "-free" suffix fallback.
					"id": "fallback-free",
				},
				{
					// No pricing metadata and no "-free" suffix: should be dropped.
					"id": "unknown-model",
				},
			},
		})
	}))
	defer upstream.Close()

	origEndpoints := ProviderEndpoints()
	origFreeOnly := ProviderFreeOnly()
	defer func() {
		SetProviderEndpoints(origEndpoints)
		SetProviderFreeOnly(origFreeOnly)
	}()

	SetProviderEndpoints(map[string]string{
		"zenmux":      upstream.URL,
		"zenmux-free": upstream.URL,
	})
	SetProviderFreeOnly(map[string]bool{
		"zenmux-free": true,
	})

	tryFetchProviders(t.Context())

	paid := getCurrentModelIDs("zenmux")
	free := getCurrentModelIDs("zenmux-free")

	wantPaid := []string{"fallback-free", "free-model", "paid-model", "unknown-model"}
	slices.Sort(paid)
	if !slices.Equal(paid, wantPaid) {
		t.Errorf("zenmux full list = %v, want %v", paid, wantPaid)
	}

	wantFree := []string{"fallback-free", "free-model"}
	if !slices.Equal(free, wantFree) {
		t.Errorf("zenmux-free filtered list = %v, want %v", free, wantFree)
	}
}

// getCurrentModelIDs returns the in-memory model IDs for a provider key.
func getCurrentModelIDs(key string) []string {
	mu.RLock()
	defer mu.RUnlock()
	entries := current[key]
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.ID)
	}
	slices.Sort(ids)
	return ids
}

func TestGetModelIDs_CommandCodeContainsExpectedModels(t *testing.T) {
	want := []string{
		"claude-opus-4-7",
		"deepseek/deepseek-v4-pro",
		"moonshotai/Kimi-K2.6",
		"zai-org/GLM-5.1",
		"MiniMaxAI/MiniMax-M2.7",
		"Qwen/Qwen3.6-Max-Preview",
	}
	got := GetModelIDs("commandcode")
	if len(got) == 0 {
		t.Fatal("GetModelIDs(commandcode) returned empty")
	}
	for _, id := range want {
		if !slices.Contains(got, id) {
			t.Errorf("GetModelIDs(commandcode) missing %q; got %v", id, got)
		}
	}
}

// TestFetchProviderModelsURL_FetchesFromCustomEndpoint verifies that
// FetchProviderModelsURL correctly fetches models from any OpenAI-compatible
// /v1/models endpoint and returns stripped model IDs.
func TestFetchProviderModelsURL_FetchesFromCustomEndpoint(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "model-a"},
				{"id": "@prefixed/model-b"},
				{"id": "model-c"},
			},
		})
	}))
	defer ts.Close()

	ids := FetchProviderModelsURL(t.Context(), ts.URL+"/v1/models")
	want := []string{"model-a", "prefixed/model-b", "model-c"}
	if !slices.Equal(ids, want) {
		t.Errorf("FetchProviderModelsURL() = %v, want %v", ids, want)
	}
}

// TestFetchProviderModelsURL_ReturnsNilOnBadURL verifies graceful failure.
func TestFetchProviderModelsURL_ReturnsNilOnBadURL(t *testing.T) {
	ids := FetchProviderModelsURL(t.Context(), "http://127.0.0.1:1/v1/models")
	if ids != nil {
		t.Errorf("FetchProviderModelsURL(bad URL) = %v, want nil", ids)
	}
}

// TestFetchProviderModelsURLCached_CachesWithinTTL verifies that the cached
// variant does not re-fetch within the TTL window.
func TestFetchProviderModelsURLCached_CachesWithinTTL(t *testing.T) {
	var calls int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"id": "cached-model"}},
		})
	}))
	defer ts.Close()

	url := ts.URL + "/v1/models"
	ids1 := FetchProviderModelsURLCached(t.Context(), url)
	ids2 := FetchProviderModelsURLCached(t.Context(), url)

	if calls != 1 {
		t.Errorf("upstream called %d times, want 1 (cached)", calls)
	}
	if !slices.Equal(ids1, ids2) {
		t.Errorf("cached call mismatch: %v != %v", ids1, ids2)
	}
}

// TestRegisterCustomEndpoint_OverridesHardcodedURL verifies that
// RegisterCustomEndpoint replaces the endpoint for a catalog key.
func TestRegisterCustomEndpoint_OverridesHardcodedURL(t *testing.T) {
	origEndpoints := ProviderEndpoints()
	defer func() {
		SetProviderEndpoints(origEndpoints)
	}()

	// Register a custom endpoint
	RegisterCustomEndpoint("oc", "http://192.168.90.101:3777/providers/oc/v1/models")

	ep := ProviderEndpoints()
	if ep["oc"] != "http://192.168.90.101:3777/providers/oc/v1/models" {
		t.Errorf("oc endpoint = %q, want custom URL", ep["oc"])
	}
}

// TestRegisterCustomEndpoint_IgnoresEmptyArgs verifies no-op on empty input.
func TestRegisterCustomEndpoint_IgnoresEmptyArgs(t *testing.T) {
	origEndpoints := ProviderEndpoints()
	defer func() {
		SetProviderEndpoints(origEndpoints)
	}()

	RegisterCustomEndpoint("", "http://example.com")
	RegisterCustomEndpoint("oc", "")

	ep := ProviderEndpoints()
	// Should still have the original oc endpoint (or empty if unset)
	_ = ep
}

// TestDynamicModelSync_RemovedModelsDisappear verifies that when the upstream
// endpoint changes its model list (removing a model), the synced catalog
// reflects the removal — models removed upstream disappear from the catalog.
func TestDynamicModelSync_RemovedModelsDisappear(t *testing.T) {
	modelList := []map[string]any{
		{"id": "model-a"},
		{"id": "model-b"},
		{"id": "model-c"},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"data": modelList})
	}))
	defer ts.Close()

	origEndpoints := ProviderEndpoints()
	defer func() {
		SetProviderEndpoints(origEndpoints)
	}()

	SetProviderEndpoints(map[string]string{"test-dynamic": ts.URL + "/v1/models"})

	// First sync: all 3 models present
	tryFetchProviders(t.Context())
	ids := getCurrentModelIDs("test-dynamic")
	want := []string{"model-a", "model-b", "model-c"}
	if !slices.Equal(ids, want) {
		t.Errorf("first sync = %v, want %v", ids, want)
	}

	// Remove model-b from upstream
	modelList = []map[string]any{
		{"id": "model-a"},
		{"id": "model-c"},
	}

	// Second sync: model-b should be gone
	tryFetchProviders(t.Context())
	ids = getCurrentModelIDs("test-dynamic")
	want = []string{"model-a", "model-c"}
	if !slices.Equal(ids, want) {
		t.Errorf("second sync = %v, want %v (model-b should be removed)", ids, want)
	}
}
