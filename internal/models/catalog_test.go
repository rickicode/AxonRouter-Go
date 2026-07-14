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

func TestGetModelIDs_NewOpenAICompatibleProviders(t *testing.T) {
	want := map[string][]string{
		"glm":     {"glm-4", "glm-5"},
		"minimax": {"minimax-m2.1", "minimax-m2.5"},
		"kimi":    {"kimi-k2"},
		"mistral": {"mistral-large-latest", "codestral-latest"},
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
