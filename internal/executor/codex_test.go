package executor

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/logging"
)

func init() {
	logging.Init("text")
	validateURL = func(string) error { return nil }
	RegisterDefaults()
}

func TestCodexExecutor_PatchesEmptyCompletedWithOutputItemDone(t *testing.T) {
	large := strings.Repeat("word ", 20000) // ~100 KB of text, bigger than old 64 KB scanner limit
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		fmt.Fprintln(w, `data: {"type":"response.created","response":{"id":"r1","model":"gpt-5.4","created_at":1700000000}}`)
		flusher.Flush()
		itemEvent := fmt.Sprintf(`{"type":"response.output_item.done","output_index":0,"item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":%q}]}}`, large)
		fmt.Fprintln(w, "data: "+itemEvent)
		flusher.Flush()
		fmt.Fprintln(w, `data: {"type":"response.completed","response":{"id":"r1","status":"completed","output":[]}}`)
		flusher.Flush()
		fmt.Fprintln(w)
	}))
	defer ts.Close()

	base := NewBaseExecutor()
	cx := NewCodexExecutor(base)
	req := &Request{
		Provider:    "cx",
		Model:       "cx/gpt-5.4",
		BaseURL:     ts.URL,
		Body:        []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
		AccessToken: "test-token",
		StreamConfig: &StreamConfig{
			FetchTimeoutMs:           5000,
			StreamIdleTimeoutMs:      5000,
			StreamReadinessTimeoutMs: 5000,
		},
	}
	res, err := cx.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	body := string(res.Body)
	if !strings.Contains(body, large) {
		t.Fatalf("patched response is missing the large output_item.done content")
	}
	if strings.Contains(body, `"output":[]`) {
		t.Fatalf("response.completed still has an empty output array after patching")
	}
}

func TestCodexExecutor_Headers(t *testing.T) {
	var gotHeaders http.Header
	var gotBody []byte
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "data: {}")
	}))
	defer ts.Close()

	base := NewBaseExecutor()
	cx := NewCodexExecutor(base)
	req := &Request{
		Provider:    "cx",
		Model:       "cx/gpt-5.4",
		BaseURL:     ts.URL + "/codex/responses",
		Body:        []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
		AccessToken: "test-token",
		ProviderSpecificData: map[string]string{
			"workspaceId": "ws_123",
			"userAgent":   "test-agent/1.0",
		},
		StreamConfig: &StreamConfig{
			FetchTimeoutMs:           5000,
			StreamIdleTimeoutMs:      5000,
			StreamReadinessTimeoutMs: 5000,
		},
	}
	_, err := cx.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if got := gotHeaders.Get("User-Agent"); got != "test-agent/1.0" {
		t.Errorf("User-Agent=%q, want test-agent/1.0", got)
	}
	if got := gotHeaders.Get("chatgpt-account-id"); got != "ws_123" {
		t.Errorf("chatgpt-account-id=%q, want ws_123", got)
	}
	if got := gotHeaders.Get("Openai-Beta"); got != "responses=experimental" {
		t.Errorf("Openai-Beta=%q, want responses=experimental", got)
	}
	if got := gotHeaders.Get("Originator"); got != "codex_cli_rs" {
		t.Errorf("Originator=%q, want codex_cli_rs", got)
	}
	if got := gotHeaders.Get("Codex-Cli-Simplified-Flow"); got != "true" {
		t.Errorf("Codex-Cli-Simplified-Flow=%q, want true", got)
	}
	if !strings.Contains(string(gotBody), `"stream":true`) {
		t.Fatalf("expected stream=true in body, got %s", string(gotBody))
	}
}

func TestCodexExecutor_UsageLimitReached(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "100")
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprintln(w, `{"error":{"type":"usage_limit_reached","message":"Usage limit reached","resets_at":1893456000}}`)
	}))
	defer ts.Close()

	base := NewBaseExecutor()
	cx := NewCodexExecutor(base)
	req := &Request{
		Provider:    "cx",
		Model:       "cx/gpt-5.4",
		BaseURL:     ts.URL,
		Body:        []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
		AccessToken: "test-token",
		StreamConfig: &StreamConfig{
			FetchTimeoutMs:           5000,
			StreamIdleTimeoutMs:      5000,
			StreamReadinessTimeoutMs: 5000,
		},
	}
	_, err := cx.Execute(context.Background(), req)
	if err == nil {
		t.Fatal("expected error")
	}
	upErr, ok := err.(*UpstreamError)
	if !ok {
		t.Fatalf("expected UpstreamError, got %T", err)
	}
	if upErr.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status=%d, want 429", upErr.StatusCode)
	}
	if got := upErr.Headers.Get("Retry-After"); got != "100" {
		t.Fatalf("Retry-After=%q, want 100", got)
	}
	bodyStr := string(upErr.Body)
	if !strings.Contains(bodyStr, "rate_limit_error") {
		t.Fatalf("expected rate_limit_error in translated body, got %s", bodyStr)
	}
	if !strings.Contains(bodyStr, "insufficient_quota") {
		t.Fatalf("expected insufficient_quota in translated body, got %s", bodyStr)
	}
}

func TestCodexExecutor_ExtractUsage(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `data: {"type":"response.created","response":{"id":"r1","model":"gpt-5.4","created_at":1700000000}}`)
		fmt.Fprintln(w, `data: {"type":"response.output_text.delta","delta":"Hi"}`)
		fmt.Fprintln(w, `data: {"type":"response.completed","response":{"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15,"input_tokens_details":{"cached_tokens":3},"output_tokens_details":{"reasoning_tokens":2}}}}`)
		fmt.Fprintln(w)
	}))
	defer ts.Close()

	base := NewBaseExecutor()
	cx := NewCodexExecutor(base)
	req := &Request{
		Provider:    "cx",
		Model:       "cx/gpt-5.4",
		BaseURL:     ts.URL,
		Body:        []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
		AccessToken: "test-token",
		StreamConfig: &StreamConfig{
			FetchTimeoutMs:           5000,
			StreamIdleTimeoutMs:      5000,
			StreamReadinessTimeoutMs: 5000,
		},
	}
	res, err := cx.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if res.StatusCode != 200 {
		t.Fatalf("status=%d, want 200", res.StatusCode)
	}
	if res.Usage["prompt_tokens"] != 10 {
		t.Fatalf("prompt_tokens=%d, want 10", res.Usage["prompt_tokens"])
	}
	if res.Usage["completion_tokens"] != 5 {
		t.Fatalf("completion_tokens=%d, want 5", res.Usage["completion_tokens"])
	}
	if res.Usage["cached_tokens"] != 3 {
		t.Fatalf("cached_tokens=%d, want 3", res.Usage["cached_tokens"])
	}
	if res.Usage["reasoning_tokens"] != 2 {
		t.Fatalf("reasoning_tokens=%d, want 2", res.Usage["reasoning_tokens"])
	}
}

func TestCodexExecutor_DropNonstandardSSE(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		fmt.Fprintln(w, "event: codex.rate_limits")
		fmt.Fprintln(w, "data: {}")
		fmt.Fprintln(w)
		flusher.Flush()
		fmt.Fprintln(w, `data: {"type":"response.completed"}`)
		fmt.Fprintln(w)
		flusher.Flush()
	}))
	defer ts.Close()

	SetDropNonstandardCodexSSE(true)
	base := NewBaseExecutor()
	base.StreamIdleTimeout = 200 * time.Millisecond
	cx := NewCodexExecutor(base)
	req := &Request{
		Provider:    "cx",
		Model:       "cx/gpt-5.4",
		BaseURL:     ts.URL,
		Body:        []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
		AccessToken: "test-token",
		StreamConfig: &StreamConfig{
			FetchTimeoutMs:           5000,
			StreamIdleTimeoutMs:      5000,
			StreamReadinessTimeoutMs: 5000,
		},
	}
	res, err := cx.ExecuteStream(context.Background(), req)
	if err != nil {
		t.Fatalf("ExecuteStream error: %v", err)
	}
	var got []string
	for chunk := range res.Chunks {
		if chunk.Err != nil {
			t.Fatalf("unexpected chunk error: %v", chunk.Err)
		}
		if chunk.Payload != nil {
			t.Logf("chunk: %q", string(chunk.Payload))
			got = append(got, string(chunk.Payload))
		}
	}
	for _, s := range got {
		if strings.Contains(s, "codex.rate_limits") {
			t.Fatalf("non-standard codex event leaked: %s", s)
		}
	}
	found := false
	for _, s := range got {
		if strings.Contains(s, "response.completed") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected response.completed chunk, got %v", got)
	}
}
