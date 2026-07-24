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
	"github.com/tidwall/gjson"
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
		fmt.Fprintln(w, `data: {"type":"response.completed","response":{"id":"r1","status":"completed","output":[]}}`)
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
			"accountId": "ws_123",
			"userAgent": "test-agent/1.0",
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
	if got := gotHeaders.Get("Chatgpt-Account-Id"); got != "ws_123" {
		t.Errorf("Chatgpt-Account-Id=%q, want ws_123", got)
	}
	if got := gotHeaders.Get("Openai-Beta"); got != "" {
		t.Errorf("Openai-Beta=%q, want empty", got)
	}
	if got := gotHeaders.Get("Originator"); got != "codex-tui" {
		t.Errorf("Originator=%q, want codex-tui", got)
	}
	if got := gotHeaders.Get("Codex-Cli-Simplified-Flow"); got != "" {
		t.Errorf("Codex-Cli-Simplified-Flow=%q, want empty", got)
	}
	if got := gotHeaders.Get("Connection"); got != "Keep-Alive" {
		t.Errorf("Connection=%q, want Keep-Alive", got)
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

func TestCodexRequestBody_NormalizesResponsesRequest(t *testing.T) {
	req := []byte(`{
		"model":"cx/gpt-5.4",
		"input":[{"type":"message","role":"system","content":[{"type":"input_text","text":"sys"}]},{"type":"message","role":"user","content":"hi"}],
		"instructions":"custom instruction",
		"tools":[{"type":"web_search_preview"},{"type":"web_search_preview_2025_03_11"},{"type":"function","function":{"name":"fn"}}],
		"tool_choice":{"type":"web_search_preview"},
		"temperature":0.5,
		"max_tokens":100,
		"top_p":0.9,
		"metadata":{"key":"value"},
		"previous_response_id":"prev_1",
		"service_tier":"fast",
		"prompt_cache_key":"key123",
		"client_metadata":{"client":"test"}
	}`)

	out := codexRequestBody(req)

	if got := gjson.GetBytes(out, "input.0.role").String(); got != "developer" {
		t.Fatalf("expected system role converted to developer, got %s", got)
	}
	if got := gjson.GetBytes(out, "input.1.content").Type; got != gjson.JSON {
		t.Fatalf("expected user string content converted to array, got %v", got)
	}
	if got := gjson.GetBytes(out, "instructions").String(); got != "custom instruction" {
		t.Fatalf("expected instructions preserved, got %s", got)
	}
	for i, want := range []string{"web_search", "web_search", "function"} {
		if got := gjson.GetBytes(out, fmt.Sprintf("tools.%d.type", i)).String(); got != want {
			t.Fatalf("expected tools.%d.type=%s, got %s", i, want, got)
		}
	}
	if got := gjson.GetBytes(out, "tool_choice.type").String(); got != "web_search" {
		t.Fatalf("expected tool_choice.type web_search, got %s", got)
	}

	stripped := []string{"temperature", "max_tokens", "top_p", "metadata", "previous_response_id", "service_tier"}
	for _, field := range stripped {
		if gjson.GetBytes(out, field).Exists() {
			t.Fatalf("expected %s to be stripped", field)
		}
	}

	if got := gjson.GetBytes(out, "prompt_cache_key").String(); got != "key123" {
		t.Fatalf("expected prompt_cache_key preserved, got %s", got)
	}
	if got := gjson.GetBytes(out, "client_metadata.client").String(); got != "test" {
		t.Fatalf("expected client_metadata preserved, got %s", got)
	}
	if !gjson.GetBytes(out, "stream").Bool() {
		t.Fatal("expected stream=true")
	}
	if gjson.GetBytes(out, "store").Bool() {
		t.Fatal("expected store=false")
	}
	if !gjson.GetBytes(out, "parallel_tool_calls").Bool() {
		t.Fatal("expected parallel_tool_calls=true")
	}
	arr := gjson.GetBytes(out, "include").Array()
	if len(arr) != 1 || arr[0].String() != "reasoning.encrypted_content" {
		t.Fatalf("expected include=[reasoning.encrypted_content], got %v", arr)
	}
}

func TestCodexRequestBody_CoercesStringInput(t *testing.T) {
	req := []byte(`{"model":"cx/gpt-5.4","input":"hello"}`)
	out := codexRequestBody(req)
	if gjson.GetBytes(out, "input").Type != gjson.JSON {
		t.Fatalf("expected input to be coerced to array")
	}
	if got := gjson.GetBytes(out, "input.0.role").String(); got != "user" {
		t.Fatalf("expected role user, got %s", got)
	}
	if got := gjson.GetBytes(out, "input.0.content.0.type").String(); got != "input_text" {
		t.Fatalf("expected content part input_text, got %s", got)
	}
	if got := gjson.GetBytes(out, "input.0.content.0.text").String(); got != "hello" {
		t.Fatalf("expected text hello, got %s", got)
	}
}

func TestCodexRequestBody_DefaultInstructions(t *testing.T) {
	req := []byte(`{"model":"cx/gpt-5.4","input":"hi"}`)
	out := codexRequestBody(req)
	inst := gjson.GetBytes(out, "instructions").String()
	if !strings.Contains(inst, "You are Codex") {
		t.Fatalf("expected default Codex instructions, got %q", inst)
	}
}

func TestCodexRequestBody_ServiceTierPriorityPreserved(t *testing.T) {
	req := []byte(`{"model":"cx/gpt-5.4","input":"hi","service_tier":"priority"}`)
	out := codexRequestBody(req)
	if got := gjson.GetBytes(out, "service_tier").String(); got != "priority" {
		t.Fatalf("expected service_tier priority preserved, got %s", got)
	}
}

func TestCodexRequestBody_ReasoningPreserved(t *testing.T) {
	req := []byte(`{"model":"cx/gpt-5.4","input":"hi","reasoning":{"effort":"high","summary":"detailed"}}`)
	out := codexRequestBody(req)
	if got := gjson.GetBytes(out, "reasoning.effort").String(); got != "high" {
		t.Fatalf("expected reasoning.effort preserved, got %s", got)
	}
	if got := gjson.GetBytes(out, "reasoning.summary").String(); got != "detailed" {
		t.Fatalf("expected reasoning.summary preserved, got %s", got)
	}
}

func TestCodexRequestBody_LeavesChatCompletionsAlone(t *testing.T) {
	req := []byte(`{"messages":[{"role":"user","content":"hi"}]}`)
	out := codexRequestBody(req)
	if !gjson.GetBytes(out, "messages").Exists() {
		t.Fatal("expected chat-completions messages to be preserved")
	}
	if gjson.GetBytes(out, "input").Exists() {
		t.Fatal("expected no input field to be introduced for chat-completions body")
	}
	if !gjson.GetBytes(out, "stream").Bool() {
		t.Fatal("expected stream=true")
	}
}
