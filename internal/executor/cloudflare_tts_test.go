package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCloudflareTTS_MeloTTSPayload(t *testing.T) {
	body := []byte(`{"input":"hello world","voice":"en","model":"cf/myshell-ai/melotts"}`)
	payload, ct, accept, err := buildCloudflareTTSPayload(body, "cf/myshell-ai/melotts")
	if err != nil {
		t.Fatalf("build payload: %v", err)
	}
	if ct != "application/json" {
		t.Fatalf("content type = %q, want application/json", ct)
	}
	if accept != "audio/mpeg" {
		t.Fatalf("accept = %q, want audio/mpeg", accept)
	}
	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if got["prompt"] != "hello world" {
		t.Fatalf("prompt = %v, want hello world", got["prompt"])
	}
	if got["lang"] != "en" {
		t.Fatalf("lang = %v, want en", got["lang"])
	}
}

func TestCloudflareTTS_AuraPayload(t *testing.T) {
	body := []byte(`{"input":"hello","voice":"luna","response_format":"wav","model":"cf/deepgram/aura-2-en"}`)
	payload, ct, accept, err := buildCloudflareTTSPayload(body, "cf/deepgram/aura-2-en")
	if err != nil {
		t.Fatalf("build payload: %v", err)
	}
	if ct != "application/json" {
		t.Fatalf("content type = %q, want application/json", ct)
	}
	if accept != "audio/wav" {
		t.Fatalf("accept = %q, want audio/wav", accept)
	}
	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if got["text"] != "hello" {
		t.Fatalf("text = %v, want hello", got["text"])
	}
	if got["speaker"] != "luna" {
		t.Fatalf("speaker = %v, want luna", got["speaker"])
	}
}

func TestCloudflareTTS_ResolveRunURL(t *testing.T) {
	psd := map[string]string{"accountId": "abc123"}
	url, err := cloudflareRunURL("https://api.cloudflare.com/client/v4/accounts/{accountId}/ai/v1/chat/completions", "cf/myshell-ai/melotts", psd)
	if err != nil {
		t.Fatalf("resolve url: %v", err)
	}
	want := "https://api.cloudflare.com/client/v4/accounts/abc123/ai/run/@cf/myshell-ai/melotts"
	if url != want {
		t.Fatalf("url = %q, want %q", url, want)
	}
}

func TestCloudflareTTS_ExecuteReturnsBinary(t *testing.T) {
	var called string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = r.URL.Path
		w.Header().Set("Content-Type", "audio/mpeg")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("fake mp3 bytes"))
	}))
	defer ts.Close()

	orig := validateURL
	validateURL = func(string) error { return nil }
	defer func() { validateURL = orig }()

	exec := NewCloudflareTTSExecutor(NewBaseExecutor())
	req := &Request{
		Model:                "myshell-ai/melotts",
		Body:                 []byte(`{"input":"hello"}`),
		Provider:             "cf",
		BaseURL:              ts.URL + "/accounts/abc/ai/v1/chat/completions",
		ProviderSpecificData: map[string]string{"accountId": "abc"},
	}
	resp, err := exec.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.HasSuffix(called, "/ai/run/@cf/myshell-ai/melotts") {
		t.Fatalf("called path = %q", called)
	}
	if string(resp.Body) != "fake mp3 bytes" {
		t.Fatalf("body = %q, want fake mp3 bytes", string(resp.Body))
	}
	if ct := resp.Headers.Get("Content-Type"); ct != "audio/mpeg" {
		t.Fatalf("content-type = %q, want audio/mpeg", ct)
	}
}

func TestCloudflareTTS_ExecuteUnwrapsAudioEnvelope(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"result":"encoded audio"}`))
	}))
	defer ts.Close()

	orig := validateURL
	validateURL = func(string) error { return nil }
	defer func() { validateURL = orig }()

	exec := NewCloudflareTTSExecutor(NewBaseExecutor())
	req := &Request{
		Model:                "myshell-ai/melotts",
		Body:                 []byte(`{"input":"hello"}`),
		Provider:             "cf",
		BaseURL:              ts.URL + "/accounts/abc/ai/v1/chat/completions",
		ProviderSpecificData: map[string]string{"accountId": "abc"},
	}
	resp, err := exec.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	body, _ := io.ReadAll(bytes.NewReader(resp.Body))
	if string(body) != "encoded audio" {
		t.Fatalf("body = %q, want encoded audio", string(body))
	}
	if ct := resp.Headers.Get("Content-Type"); ct != "audio/mpeg" {
		t.Fatalf("content-type = %q, want audio/mpeg", ct)
	}
}
