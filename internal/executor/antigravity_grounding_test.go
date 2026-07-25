package executor

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// redirectTestTransport maps requests targeting vertexaisearch.cloud.google.com
// to the backing test server so the redirect host check can be exercised without
// real network access.
type redirectTestTransport struct {
	base   http.RoundTripper
	server *httptest.Server
}

func (t *redirectTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host == "vertexaisearch.cloud.google.com" {
		parsed, _ := url.Parse(t.server.URL)
		req.URL.Scheme = parsed.Scheme
		req.URL.Host = parsed.Host
	}
	if t.base == nil {
		t.base = http.DefaultTransport
	}
	return t.base.RoundTrip(req)
}

func newRedirectTestClient(server *httptest.Server) *http.Client {
	return &http.Client{
		Transport: &redirectTestTransport{server: server, base: http.DefaultTransport},
	}
}

func TestIsAntigravityVertexSearchRedirect(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want bool
	}{
		{"redirect", "https://vertexaisearch.cloud.google.com/grounding-api-redirect/abc123", true},
		{"redirect with path", "https://vertexaisearch.cloud.google.com/grounding-api-redirect/foo/bar", true},
		{"http scheme", "http://vertexaisearch.cloud.google.com/grounding-api-redirect/abc", false},
		{"wrong host", "https://example.com/grounding-api-redirect/abc", false},
		{"wrong path", "https://vertexaisearch.cloud.google.com/other/abc", false},
		{"plain url", "https://example.com/page", false},
		{"empty", "", false},
		{"invalid", "://bad", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isAntigravityVertexSearchRedirect(tc.url); got != tc.want {
				t.Errorf("isAntigravityVertexSearchRedirect(%q) = %v, want %v", tc.url, got, tc.want)
			}
		})
	}
}

func TestResolveAntigravityGroundingURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/grounding-api-redirect/ok" {
			w.Header().Set("Location", "https://www.example.com/final")
			w.WriteHeader(http.StatusFound)
			return
		}
		if r.URL.Path == "/grounding-api-redirect/bad-location" {
			w.Header().Set("Location", "http://insecure.example.com/final")
			w.WriteHeader(http.StatusFound)
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	client := newRedirectTestClient(server)
	ctx := context.Background()

	t.Run("resolves redirect", func(t *testing.T) {
		got := resolveAntigravityGroundingURL(ctx, client, "https://vertexaisearch.cloud.google.com/grounding-api-redirect/ok")
		if got != "https://www.example.com/final" {
			t.Errorf("got %q, want https://www.example.com/final", got)
		}
	})

	t.Run("returns original on non-redirect host", func(t *testing.T) {
		got := resolveAntigravityGroundingURL(ctx, client, "https://example.com/page")
		if got != "https://example.com/page" {
			t.Errorf("got %q, want original URL", got)
		}
	})

	t.Run("returns original on insecure target", func(t *testing.T) {
		got := resolveAntigravityGroundingURL(ctx, client, "https://vertexaisearch.cloud.google.com/grounding-api-redirect/bad-location")
		if got != "https://vertexaisearch.cloud.google.com/grounding-api-redirect/bad-location" {
			t.Errorf("got %q, want original URL", got)
		}
	})
}

func TestResolveAntigravityGroundingURLs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/grounding-api-redirect/resolve" {
			w.Header().Set("Location", "https://www.example.com/resolved")
			w.WriteHeader(http.StatusFound)
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	client := newRedirectTestClient(server)
	ctx := context.Background()

	// Pre-populate cache for a deterministic synchronous resolution.
	antigravityGroundingCache.Store("https://vertexaisearch.cloud.google.com/grounding-api-redirect/cached", "https://www.example.com/cached")
	defer antigravityGroundingCache.Delete("https://vertexaisearch.cloud.google.com/grounding-api-redirect/cached")

	payload := []byte(`{"response":{"candidates":[{"groundingMetadata":{"groundingChunks":[{"web":{"uri":"https://vertexaisearch.cloud.google.com/grounding-api-redirect/cached"}},{"web":{"uri":"https://vertexaisearch.cloud.google.com/grounding-api-redirect/resolve"}}]}}]}}`)

	out := resolveAntigravityGroundingURLs(ctx, client, payload)
	if !bytes.Contains(out, []byte(`"uri":"https://www.example.com/cached"`)) {
		t.Errorf("expected cached URL to be rewritten, got %s", string(out))
	}
	if !bytes.Contains(out, []byte(`"uri":"https://vertexaisearch.cloud.google.com/grounding-api-redirect/resolve"`)) {
		t.Errorf("expected uncached URL to remain unchanged, got %s", string(out))
	}

	// Wait for async resolution to land in the cache.
	time.Sleep(100 * time.Millisecond)
	if v, ok := antigravityGroundingCache.Load("https://vertexaisearch.cloud.google.com/grounding-api-redirect/resolve"); !ok || v.(string) != "https://www.example.com/resolved" {
		t.Errorf("expected async resolution to cache resolved URL, got %v", v)
	}
	antigravityGroundingCache.Delete("https://vertexaisearch.cloud.google.com/grounding-api-redirect/resolve")
}

func TestResolveGroundingURLsInSSELine(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/grounding-api-redirect/sse" {
			w.Header().Set("Location", "https://www.example.com/sse-final")
			w.WriteHeader(http.StatusFound)
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	// Pre-populate cache so the interceptor can rewrite synchronously.
	antigravityGroundingCache.Store("https://vertexaisearch.cloud.google.com/grounding-api-redirect/sse", "https://www.example.com/sse-final")
	defer antigravityGroundingCache.Delete("https://vertexaisearch.cloud.google.com/grounding-api-redirect/sse")

	e := NewAntigravityExecutor(NewBaseExecutor())
	ctx := context.Background()
	client := newRedirectTestClient(server)

	line := []byte(`data: {"response":{"candidates":[{"groundingMetadata":{"groundingChunks":[{"web":{"uri":"https://vertexaisearch.cloud.google.com/grounding-api-redirect/sse"}}]}}]}}` + "\n\n")
	out := e.resolveGroundingURLsInSSELine(ctx, client, line)
	if !bytes.Contains(out, []byte(`"uri":"https://www.example.com/sse-final"`)) {
		t.Errorf("expected SSE line to be rewritten, got %s", string(out))
	}
	if !strings.HasPrefix(string(out), "data: ") {
		t.Errorf("expected SSE prefix preserved, got %s", string(out))
	}
}

func TestResolveGroundingInResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/grounding-api-redirect/response" {
			w.Header().Set("Location", "https://www.example.com/response-final")
			w.WriteHeader(http.StatusFound)
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	antigravityGroundingCache.Store("https://vertexaisearch.cloud.google.com/grounding-api-redirect/response", "https://www.example.com/response-final")
	defer antigravityGroundingCache.Delete("https://vertexaisearch.cloud.google.com/grounding-api-redirect/response")

	e := NewAntigravityExecutor(NewBaseExecutor())
	ctx := context.Background()

	body := []byte(`{"candidates":[{"groundingMetadata":{"groundingChunks":[{"web":{"uri":"https://vertexaisearch.cloud.google.com/grounding-api-redirect/response"}}]}}]}`)
	out := e.resolveGroundingInResponse(ctx, body)
	if !bytes.Contains(out, []byte(`"uri":"https://www.example.com/response-final"`)) {
		t.Errorf("expected response body to be rewritten, got %s", string(out))
	}
}
