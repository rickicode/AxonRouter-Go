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

type openRouterTestTransport struct {
	host string
}

func (rt *openRouterTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host == "openrouter.ai" {
		req.URL.Scheme = "http"
		req.URL.Host = rt.host
	}
	return http.DefaultTransport.RoundTrip(req)
}

func TestDiscoverOpenRouterModelsCached_FiltersFreeModels(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/models" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{
					"id":      "test/free-1",
					"pricing": map[string]any{"prompt": "0", "completion": "0", "image": "0"},
				},
				{
					"id":      "test/paid-1",
					"pricing": map[string]any{"prompt": "0.000001", "completion": "0.000001"},
				},
				{
					"id":      "test/free-2",
					"pricing": map[string]any{"prompt": "0", "completion": "0", "image": "0"},
				},
			},
		})
	}))
	defer ts.Close()

	u, _ := url.Parse(ts.URL)
	old := http.DefaultClient.Transport
	http.DefaultClient.Transport = &openRouterTestTransport{host: u.Host}
	defer func() { http.DefaultClient.Transport = old }()

	resetOpenRouterDiscoveryCache()
	DiscoverOpenRouterModelsCached()

	ids := GetAllModelIDs("openrouter")
	if !slices.Contains(ids, "test/free-1") {
		t.Errorf("expected free model test/free-1 in catalog, got %v", ids)
	}
	if !slices.Contains(ids, "test/free-2") {
		t.Errorf("expected free model test/free-2 in catalog, got %v", ids)
	}
	if slices.Contains(ids, "test/paid-1") {
		t.Errorf("paid model test/paid-1 should not appear, got %v", ids)
	}
}

func TestDiscoverOpenRouterModelsCached_RespectsTTL(t *testing.T) {
	var calls int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "openai/gpt-free", "pricing": map[string]any{"prompt": "0", "completion": "0"}},
			},
		})
	}))
	defer ts.Close()

	u, _ := url.Parse(ts.URL)
	old := http.DefaultClient.Transport
	http.DefaultClient.Transport = &openRouterTestTransport{host: u.Host}
	defer func() { http.DefaultClient.Transport = old }()

	resetOpenRouterDiscoveryCache()
	DiscoverOpenRouterModelsCached()
	DiscoverOpenRouterModelsCached()

	if calls != 1 {
		t.Errorf("OpenRouter API called %d times, want 1", calls)
	}
}

func TestDiscoverOpenRouterModelsCached_RefreshesAfterTTL(t *testing.T) {
	var calls int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "openai/gpt-free", "pricing": map[string]any{"prompt": "0", "completion": "0"}},
			},
		})
	}))
	defer ts.Close()

	u, _ := url.Parse(ts.URL)
	old := http.DefaultClient.Transport
	http.DefaultClient.Transport = &openRouterTestTransport{host: u.Host}
	defer func() { http.DefaultClient.Transport = old }()

	resetOpenRouterDiscoveryCache()
	openRouterDiscoveryCache.last = time.Now().Add(-openRouterDiscoveryTTL - time.Second)
	DiscoverOpenRouterModelsCached()

	if calls != 1 {
		t.Errorf("OpenRouter API called %d times, want 1", calls)
	}
}

func TestDiscoverOpenRouterModelsCached_SendsAttributionHeaders(t *testing.T) {
	var got http.Header
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "openai/gpt-free", "pricing": map[string]any{"prompt": "0", "completion": "0"}},
			},
		})
	}))
	defer ts.Close()

	u, _ := url.Parse(ts.URL)
	old := http.DefaultClient.Transport
	http.DefaultClient.Transport = &openRouterTestTransport{host: u.Host}
	defer func() { http.DefaultClient.Transport = old }()

	resetOpenRouterDiscoveryCache()
	DiscoverOpenRouterModelsCached()

	if !strings.Contains(got.Get("HTTP-Referer"), "endpoint-proxy") {
		t.Errorf("HTTP-Referer header missing or unexpected: %q", got.Get("HTTP-Referer"))
	}
	if got.Get("X-Title") == "" {
		t.Error("expected X-Title header to be present")
	}
}
