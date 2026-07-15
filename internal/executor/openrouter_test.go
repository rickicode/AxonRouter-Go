package executor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenRouterHeaders_SetDefaults(t *testing.T) {
	h := map[string]string{}
	openRouterHeaders(h, "openrouter", nil)

	if h["HTTP-Referer"] != "https://endpoint-proxy.local" {
		t.Errorf("HTTP-Referer = %q, want default", h["HTTP-Referer"])
	}
	if h["X-Title"] != "Endpoint Proxy" {
		t.Errorf("X-Title = %q, want default", h["X-Title"])
	}
}

func TestOpenRouterHeaders_IgnoresOtherProviders(t *testing.T) {
	h := map[string]string{}
	openRouterHeaders(h, "groq", nil)

	if h["HTTP-Referer"] != "" || h["X-Title"] != "" {
		t.Error("expected no openrouter headers for non-openrouter provider")
	}
}

func TestOpenRouterHeaders_UsesProviderSpecificData(t *testing.T) {
	h := map[string]string{}
	openRouterHeaders(h, "openrouter", map[string]string{
		"http_referer": "https://my.app",
		"x_title":      "My App",
	})

	if h["HTTP-Referer"] != "https://my.app" {
		t.Errorf("HTTP-Referer = %q, want https://my.app", h["HTTP-Referer"])
	}
	if h["X-Title"] != "My App" {
		t.Errorf("X-Title = %q, want My App", h["X-Title"])
	}
}

func TestOpenRouterExecutor_AddsAttributionHeadersToUpstream(t *testing.T) {
	var got http.Header
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"object":"chat.completion","choices":[]}`))
	}))
	defer ts.Close()

	base := NewBaseExecutor()
	exec := NewOpenRouterExecutor(base)
	_, _ = exec.Execute(context.Background(), &Request{
		Provider:        "openrouter",
		BaseURL:         ts.URL,
		APIKey:          "sk-test",
		ProviderSpecificData: map[string]string{"x_title": "Test Gateway"},
		Body:            []byte(`{"model":"openrouter/openai/gpt-4o","messages":[]}`),
	})

	if got.Get("HTTP-Referer") != "https://endpoint-proxy.local" {
		t.Errorf("HTTP-Referer = %q, want default", got.Get("HTTP-Referer"))
	}
	if got.Get("X-Title") != "Test Gateway" {
		t.Errorf("X-Title = %q, want Test Gateway", got.Get("X-Title"))
	}
}
