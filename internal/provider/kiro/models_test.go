package kiro

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func resetLiveModelCache(t *testing.T) {
	t.Helper()
	ClearLiveModelCache()
	t.Cleanup(ClearLiveModelCache)
}

func liveServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func setLiveEndpointOverride(t *testing.T, base string) {
	t.Helper()
	orig := liveModelsEndpointBase
	liveModelsEndpointBase = base
	t.Cleanup(func() { liveModelsEndpointBase = orig })
}

func TestFetchLiveModels_NoToken_ReturnsFallback(t *testing.T) {
	resetLiveModelCache(t)
	res, err := FetchLiveModels("", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Source != "fallback" {
		t.Fatalf("expected fallback source, got %q", res.Source)
	}
	if len(res.Models) != len(BaseModels)*4 {
		t.Fatalf("expected %d fallback models, got %d", len(BaseModels)*4, len(res.Models))
	}
}

func TestFetchLiveModels_FetchesAndCachesLiveCatalog(t *testing.T) {
	resetLiveModelCache(t)
	var calls atomic.Int32
	handler := func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if !strings.Contains(r.URL.Path, "ListAvailableModels") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if origin := r.URL.Query().Get("origin"); origin != "AI_EDITOR" {
			t.Errorf("expected origin=AI_EDITOR, got %q", origin)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"models": []map[string]any{
				{"modelId": "claude-sonnet-5", "modelName": "Claude Sonnet 5", "tokenLimits": map[string]any{"maxInputTokens": 1000000}, "rateMultiplier": 2.5},
			},
		})
	}
	srv := liveServer(t, handler)
	setLiveEndpointOverride(t, srv.URL)
	orig := liveCatalogHTTPClient
	liveCatalogHTTPClient = srv.Client()
	t.Cleanup(func() { liveCatalogHTTPClient = orig })

	psd := map[string]any{"region": "us-east-1"}
	res1, err := FetchLiveModels("tok-live", psd)
	if err != nil {
		t.Fatalf("first fetch failed: %v", err)
	}
	if res1.Source != "api" {
		t.Fatalf("expected api source, got %q", res1.Source)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected 1 upstream call, got %d", calls.Load())
	}

	res2, err := FetchLiveModels("tok-live", psd)
	if err != nil {
		t.Fatalf("second fetch failed: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected cache hit, got %d calls", calls.Load())
	}
	if len(res2.Models) != len(res1.Models) {
		t.Fatalf("cached models count mismatch")
	}

	want := []string{"claude-sonnet-5", "claude-sonnet-5-thinking", "claude-sonnet-5-agentic", "claude-sonnet-5-thinking-agentic"}
	for _, id := range want {
		if _, ok := findModel(res1.Models, id); !ok {
			t.Fatalf("missing variant %q", id)
		}
	}

	base, ok := findModel(res1.Models, "claude-sonnet-5")
	if !ok {
		t.Fatal("missing base model")
	}
	if base.RateMultiplier != 2.5 {
		t.Fatalf("expected rate multiplier 2.5, got %v", base.RateMultiplier)
	}
	if base.ContextLength != 1000000 {
		t.Fatalf("expected context 1000000, got %d", base.ContextLength)
	}
	if !strings.Contains(base.DisplayName, "2.5x credit") {
		t.Fatalf("expected rate in display name, got %q", base.DisplayName)
	}
}

func TestFetchLiveModels_FallbackOnEmptyResponse(t *testing.T) {
	resetLiveModelCache(t)
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"models": []map[string]any{}})
	}
	srv := liveServer(t, handler)
	setLiveEndpointOverride(t, srv.URL)
	orig := liveCatalogHTTPClient
	liveCatalogHTTPClient = srv.Client()
	t.Cleanup(func() { liveCatalogHTTPClient = orig })

	res, err := FetchLiveModels("tok-empty", map[string]any{"region": "us-east-1"})
	if err != nil {
		t.Fatalf("fetch failed: %v", err)
	}
	if res.Source != "fallback" {
		t.Fatalf("expected fallback when live returns no models, got %q", res.Source)
	}
}

func TestFetchLiveModels_CacheExpires(t *testing.T) {
	resetLiveModelCache(t)
	var calls atomic.Int32
	handler := func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"models": []map[string]any{{"modelId": "glm-5"}},
		})
	}
	srv := liveServer(t, handler)
	setLiveEndpointOverride(t, srv.URL)
	orig := liveCatalogHTTPClient
	liveCatalogHTTPClient = srv.Client()
	t.Cleanup(func() { liveCatalogHTTPClient = orig })

	origTTL := liveModelCacheTTL
	liveModelCacheTTL = -1 * time.Second
	t.Cleanup(func() { liveModelCacheTTL = origTTL })

	_, _ = FetchLiveModels("tok-ttl", map[string]any{"region": "us-east-1"})
	_, _ = FetchLiveModels("tok-ttl", map[string]any{"region": "us-east-1"})
	if calls.Load() != 2 {
		t.Fatalf("expected cache miss after negative TTL, got %d calls", calls.Load())
	}
}

func TestFetchLiveModels_ProfileArnRetry(t *testing.T) {
	resetLiveModelCache(t)
	var calls atomic.Int32
	handler := func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		q := r.URL.Query()
		if n == 1 && q.Get("profileArn") != "" {
			t.Error("first origin-only attempt should not include profileArn")
		}
		if n == 1 {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if q.Get("profileArn") != "arn:aws:codewhisperer:us-east-1:123456789012:profile/ABC" {
			t.Errorf("expected profileArn retry, got %q", q.Get("profileArn"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"models": []map[string]any{{"modelId": "qwen3-coder-next"}},
		})
	}
	srv := liveServer(t, handler)
	setLiveEndpointOverride(t, srv.URL)
	orig := liveCatalogHTTPClient
	liveCatalogHTTPClient = srv.Client()
	t.Cleanup(func() { liveCatalogHTTPClient = orig })

	psd := map[string]any{
		"profileArn": "arn:aws:codewhisperer:us-east-1:123456789012:profile/ABC",
		"region":     "us-east-1",
	}
	res, err := FetchLiveModels("tok-retry", psd)
	if err != nil {
		t.Fatalf("fetch failed: %v", err)
	}
	if res.Source != "api" {
		t.Fatalf("expected api after retry, got %q", res.Source)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected 2 calls, got %d", calls.Load())
	}
}

func TestResolveKiroRuntimeRegion(t *testing.T) {
	cases := []struct {
		name string
		psd  map[string]string
		want string
	}{
		{"arn region wins", map[string]string{"profileArn": "arn:aws:codewhisperer:eu-central-1:123:profile/X", "region": "us-east-1"}, "eu-central-1"},
		{"known stored region", map[string]string{"region": "eu-central-1"}, "eu-central-1"},
		{"unknown stored region ignored", map[string]string{"region": "eu-north-1"}, "us-east-1"},
		{"default", map[string]string{}, "us-east-1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveKiroRuntimeRegion(tc.psd)
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildKiroModelsEndpoints(t *testing.T) {
	urls := buildKiroModelsEndpoints("eu-central-1")
	if len(urls) != 2 {
		t.Fatalf("expected 2 endpoints, got %d", len(urls))
	}
	if !strings.Contains(urls[0], "q.eu-central-1.amazonaws.com") {
		t.Fatalf("expected regional endpoint first, got %q", urls[0])
	}
	if !strings.Contains(urls[1], "q.us-east-1.amazonaws.com") {
		t.Fatalf("expected us-east-1 fallback, got %q", urls[1])
	}
}

func TestExpandLiveModels_AutoSkipsAgentic(t *testing.T) {
	data := map[string]any{
		"models": []map[string]any{{"modelId": "auto"}},
	}
	models := expandLiveModels(data)
	if len(models) != 2 {
		t.Fatalf("expected auto -> 2 variants, got %d", len(models))
	}
	for _, m := range models {
		if strings.Contains(m.ID, "agentic") {
			t.Fatalf("auto should not have agentic variant, got %q", m.ID)
		}
	}
}
