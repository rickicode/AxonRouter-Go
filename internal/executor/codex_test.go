package executor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/cache"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/rickicode/AxonRouter-Go/internal/telemetry"
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

func TestCodexExecutor_ExecuteStream_PatchesEmptyCompleted(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		fmt.Fprintln(w, `data: {"type":"response.created","response":{"id":"r1","model":"gpt-5.4","created_at":1700000000}}`)
		flusher.Flush()
		fmt.Fprintln(w, `data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello from stream"}]}}`)
		flusher.Flush()
		fmt.Fprintln(w, `data: {"type":"response.completed","response":{"id":"r1","status":"completed","output":[]}}`)
		flusher.Flush()
		fmt.Fprintln(w)
	}))
	defer ts.Close()

	base := NewBaseExecutor()
	base.StreamIdleTimeout = 200 * time.Millisecond
	cx := NewCodexExecutor(base)
	req := &Request{
		Provider:    "cx",
		Model:       "cx/gpt-5.4",
		BaseURL:     ts.URL,
		Body:        []byte(`{"input":[{"role":"user","content":"hi"}]}`),
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

	var completedPayload []byte
	for chunk := range res.Chunks {
		if chunk.Err != nil {
			t.Fatalf("unexpected chunk error: %v", chunk.Err)
		}
		if chunk.Payload == nil {
			continue
		}
		if strings.Contains(string(chunk.Payload), `"type":"response.completed"`) {
			completedPayload = chunk.Payload
		}
	}
	if completedPayload == nil {
		t.Fatal("response.completed chunk not found")
	}
	data, _ := parseCodexEvent(completedPayload)
	output := gjson.GetBytes(data, "response.output").Array()
	if len(output) != 1 {
		t.Fatalf("expected 1 patched output item, got %d", len(output))
	}
	if got := output[0].Get("role").String(); got != "assistant" {
		t.Fatalf("expected patched assistant role, got %s", got)
	}
	if got := output[0].Get("content.0.text").String(); got != "hello from stream" {
		t.Fatalf("expected patched output text, got %s", got)
	}
}

func TestEnsureImageGenerationTool_InjectsForImageModels(t *testing.T) {
	body := []byte(`{"model":"gpt-image-2","input":"draw a cat"}`)
	out := ensureImageGenerationTool(body, "gpt-image-2", "")
	if got := gjson.GetBytes(out, "tools.0.type").String(); got != "image_generation" {
		t.Fatalf("expected image_generation tool injected, got %q", got)
	}
	if got := gjson.GetBytes(out, "tools.0.model").String(); got != defaultCodexImageToolModel {
		t.Fatalf("expected injected model %q, got %q", defaultCodexImageToolModel, got)
	}
	if got := gjson.GetBytes(out, "tools.0.output_format").String(); got != "png" {
		t.Fatalf("expected output_format png, got %q", got)
	}
}

func TestEnsureImageGenerationTool_AppendsToExistingTools(t *testing.T) {
	body := []byte(`{"model":"gpt-image-2","tools":[{"type":"function","function":{"name":"foo"}}]}`)
	out := ensureImageGenerationTool(body, "gpt-image-2", "")
	if got := gjson.GetBytes(out, "tools.1.type").String(); got != "image_generation" {
		t.Fatalf("expected image_generation appended, got %q; body=%s", got, string(out))
	}
	if got := gjson.GetBytes(out, "tools.1.model").String(); got != defaultCodexImageToolModel {
		t.Fatalf("expected appended model %q, got %q", defaultCodexImageToolModel, got)
	}
}

func TestEnsureImageGenerationTool_SkipsWhenPresent(t *testing.T) {
	body := []byte(`{"model":"gpt-image-2","tools":[{"type":"image_generation","output_format":"webp"}]}`)
	out := ensureImageGenerationTool(body, "gpt-image-2", "")
	if gjson.GetBytes(out, "tools.#").Int() != 1 {
		t.Fatalf("expected unchanged tools, got %s", string(out))
	}
}

func TestEnsureImageGenerationTool_SkipsNonImageModels(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","input":"hello"}`)
	out := ensureImageGenerationTool(body, "gpt-5.4", "")
	if gjson.GetBytes(out, "tools").Exists() {
		t.Fatalf("expected no tools injected for non-image model, got %s", string(out))
	}
}

func TestEnsureImageGenerationTool_ConfigurableModel(t *testing.T) {
	body := []byte(`{"model":"gpt-image-2","input":"draw a cat"}`)
	out := ensureImageGenerationTool(body, "gpt-image-2", "gpt-image-1.5")
	if got := gjson.GetBytes(out, "tools.0.model").String(); got != "gpt-image-1.5" {
		t.Fatalf("expected configurable model gpt-image-1.5, got %q", got)
	}
}

func TestEnsureImageGenerationTool_EscapesModelAsJSON(t *testing.T) {
	toolModel := "custom\nmodel\"quoted\""
	body := []byte(`{"model":"gpt-image-2","input":"draw a cat"}`)
	out := ensureImageGenerationTool(body, "gpt-image-2", toolModel)
	if !json.Valid(out) {
		t.Fatalf("injected tool payload is invalid JSON: %s", out)
	}
	if got := gjson.GetBytes(out, "tools.0.model").String(); got != toolModel {
		t.Fatalf("expected model %q, got %q", toolModel, got)
	}
}

func TestCodexImageGenerationToolModel_Priority(t *testing.T) {
	t.Run("provider_specific_data", func(t *testing.T) {
		req := &Request{ProviderSpecificData: map[string]string{"imageGenerationModel": "custom-model"}}
		if got := codexImageGenerationToolModel(req); got != "custom-model" {
			t.Fatalf("expected custom-model, got %q", got)
		}
	})
	t.Run("env_override", func(t *testing.T) {
		t.Setenv("AXON_CODEX_IMAGE_GENERATION_MODEL", "env-model")
		req := &Request{}
		if got := codexImageGenerationToolModel(req); got != "env-model" {
			t.Fatalf("expected env-model, got %q", got)
		}
	})
	t.Run("default", func(t *testing.T) {
		t.Setenv("AXON_CODEX_IMAGE_GENERATION_MODEL", "")
		req := &Request{}
		if got := codexImageGenerationToolModel(req); got != defaultCodexImageToolModel {
			t.Fatalf("expected default %q, got %q", defaultCodexImageToolModel, got)
		}
	})
}

func TestCodexIdentityConfuseBodyAndExpose(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","prompt_cache_key":"user-key-123","client_metadata":{"x-codex-window-id":"win-1","x-codex-installation-id":"install-1"}}`)
	connID := "conn-abc"
	confused, state := applyCodexIdentityConfuseBody(body, connID)
	if !state.enabled {
		t.Fatal("expected identity confuse enabled")
	}
	if gjson.GetBytes(confused, "prompt_cache_key").String() == "user-key-123" {
		t.Fatal("expected prompt_cache_key to be confused")
	}
	if got := gjson.GetBytes(confused, "client_metadata.x-codex-window-id").String(); !strings.HasSuffix(got, ":0") {
		t.Fatalf("expected window id confused with :0 suffix, got %q", got)
	}

	upstreamResp := []byte(`{"prompt_cache_key":"` + gjson.GetBytes(confused, "prompt_cache_key").String() + `"}`)
	exposed := applyCodexIdentityExposeResponsePayload(upstreamResp, state)
	if !strings.Contains(string(exposed), `"user-key-123"`) {
		t.Fatalf("expected original prompt_cache_key restored in response, got %s", string(exposed))
	}
}

func TestCodexReasoningReplayCacheKeyFromBody(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","prompt_cache_key":"pk-1"}`)
	key := codexReasoningReplaySessionKey(body, nil)
	if key != "prompt-cache:pk-1" {
		t.Fatalf("expected prompt-cache:pk-1, got %q", key)
	}
}

func TestCodexReasoningReplayCacheKeyFromHeader(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4"}`)
	headers := map[string]string{"X-Codex-Window-Id": "wnd-9"}
	key := codexReasoningReplaySessionKey(body, headers)
	if key != "window:wnd-9" {
		t.Fatalf("expected window:wnd-9, got %q", key)
	}
}

func TestCodexReasoningReplayInjectAndCache(t *testing.T) {
	cache.ClearCodexReasoningReplayCache()
	model := "gpt-5.4"
	sessionKey := "prompt-cache:test"

	reasoning := []byte(`{"type":"reasoning","encrypted_content":"sig-xyz","summary":[],"content":null}`)
	call := []byte(`{"type":"function_call","call_id":"call-1","name":"fn","arguments":"{}"}`)
	if err := cache.CacheCodexReasoningReplayItems(context.Background(), model, sessionKey, [][]byte{reasoning, call}); err != nil {
		t.Fatalf("cache failed: %v", err)
	}

	body := []byte(`{"model":"gpt-5.4","input":[{"type":"message","role":"user","content":"hi"}]}`)
	body, ok := codexInjectReasoningReplay(body, sessionKey)
	if !ok {
		t.Fatal("expected replay injection")
	}
	input := gjson.GetBytes(body, "input").Array()
	if len(input) != 3 {
		t.Fatalf("expected 3 input items, got %d: %s", len(input), string(body))
	}
	if input[0].Get("type").String() != "reasoning" {
		t.Fatalf("expected first injected item to be reasoning, got %s", input[0].Get("type").String())
	}
	if input[1].Get("type").String() != "function_call" {
		t.Fatalf("expected second injected item to be function_call, got %s", input[1].Get("type").String())
	}
	if input[2].Get("role").String() != "user" {
		t.Fatalf("expected user message after injected items, got %s", input[2].Get("role").String())
	}

	// A second request with the same reasoning already present should not duplicate it.
	cache.ClearCodexReasoningReplayCache()
	if err := cache.CacheCodexReasoningReplayItems(context.Background(), model, sessionKey, [][]byte{reasoning}); err != nil {
		t.Fatalf("cache reasoning-only failed: %v", err)
	}
	body2 := []byte(`{"model":"gpt-5.4","input":[{"type":"reasoning","encrypted_content":"sig-xyz"},{"type":"message","role":"user","content":"again"}]}`)
	body2, ok2 := codexInjectReasoningReplay(body2, sessionKey)
	if ok2 {
		t.Fatal("expected no replay injection when reasoning already present")
	}
	if gjson.GetBytes(body2, "input.#").Int() != 2 {
		t.Fatalf("expected input unchanged, got %s", string(body2))
	}
}

func TestCodexCacheReasoningReplay(t *testing.T) {
	cache.ClearCodexReasoningReplayCache()
	model := "gpt-5.4"
	sessionKey := "prompt-cache:cache-test"
	response := []byte(`{"type":"response.completed","response":{"output":[{"type":"reasoning","encrypted_content":"sig-abc"},{"type":"message","role":"assistant","content":"hi"},{"type":"function_call","call_id":"c-1","name":"fn","arguments":"{}"}]}}`)
	codexCacheReasoningReplay(context.Background(), response, model, sessionKey)

	items, err := cache.GetCodexReasoningReplayItems(context.Background(), model, sessionKey)
	if err != nil {
		t.Fatalf("get cache failed: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 cached replay items, got %d", len(items))
	}
}

func TestCodexIncompleteStreamError_IsRequestScoped(t *testing.T) {
	err := newCodexIncompleteStreamError()
	rs, ok := interface{}(err).(interface{ IsRequestScoped() bool })
	if !ok {
		t.Fatal("CodexIncompleteStreamError should implement IsRequestScoped")
	}
	if !rs.IsRequestScoped() {
		t.Fatal("expected IsRequestScoped() == true")
	}
}

// TestCodexReasoningReplay_MultiTurnIdentityConfuse verifies that reasoning-replay
// cache hits work across turns when identity confusion is enabled. The replay
// session key is derived after confusion is applied, so the original continuity
// value does not affect cache lookup on the second turn.
func TestCodexReasoningReplay_MultiTurnIdentityConfuse(t *testing.T) {
	cache.ClearCodexReasoningReplayCache()
	connID := "conn-xyz"
	model := "gpt-5.4"

	// Simulate a first turn that produces reasoning and a function_call.
	turn1Req := []byte(`{"model":"gpt-5.4","prompt_cache_key":"real-user-key","input":[{"type":"message","role":"user","content":"hello"}]}`)
	turn1Body := codexRequestBody(turn1Req)
	turn1Body = ensureImageGenerationTool(turn1Body, model, "")
	turn1Body, _ = applyCodexIdentityConfuseBody(turn1Body, connID)
	turn1Key := codexReasoningReplaySessionKey(turn1Body, nil)
	if turn1Key == "prompt-cache:real-user-key" {
		t.Fatal("expected session key to use confused value, not original")
	}

	reasoning := []byte(`{"type":"reasoning","encrypted_content":"sig-turn-1","summary":[],"content":null}`)
	call := []byte(`{"type":"function_call","call_id":"call-1","name":"fn","arguments":"{}"}`)
	_ = cache.CacheCodexReasoningReplayItems(context.Background(), model, turn1Key, [][]byte{reasoning, call})

	// Second turn arrives with the same original continuity key. With identity
	// confusion applied first, cache lookup should hit using the confused key.
	turn2Req := []byte(`{"model":"gpt-5.4","prompt_cache_key":"real-user-key","input":[{"type":"message","role":"user","content":"again"}]}`)
	turn2Body := codexRequestBody(turn2Req)
	turn2Body = ensureImageGenerationTool(turn2Body, model, "")
	turn2Body, _ = applyCodexIdentityConfuseBody(turn2Body, connID)
	turn2Key := codexReasoningReplaySessionKey(turn2Body, nil)
	if turn2Key != turn1Key {
		t.Fatalf("expected same confused session key across turns; got %q != %q", turn2Key, turn1Key)
	}

	turn2Body, ok := codexInjectReasoningReplay(turn2Body, turn2Key)
	if !ok {
		t.Fatal("expected replay injection on second turn")
	}
	input := gjson.GetBytes(turn2Body, "input").Array()
	if len(input) != 3 {
		t.Fatalf("expected 3 input items (reasoning, function_call, user), got %d: %s", len(input), string(turn2Body))
	}
	if got := input[0].Get("type").String(); got != "reasoning" {
		t.Fatalf("expected injected reasoning, got %q", got)
	}
	if got := input[0].Get("encrypted_content").String(); got != "sig-turn-1" {
		t.Fatalf("expected reasoning from turn 1 cache, got %q", got)
	}
	if got := input[1].Get("type").String(); got != "function_call" {
		t.Fatalf("expected injected function_call, got %q", got)
	}
	if got := input[2].Get("role").String(); got != "user" {
		t.Fatalf("expected user message last, got role %q", got)
	}

	// The confused prompt_cache_key should be present in the outgoing body.
	confusedCacheKey := gjson.GetBytes(turn2Body, "prompt_cache_key").String()
	if confusedCacheKey == "real-user-key" || confusedCacheKey == "" {
		t.Fatalf("expected confused prompt_cache_key in outgoing body, got %q", confusedCacheKey)
	}
}

func TestCodexHeaders_BlocklistStripsDangerousHeaders(t *testing.T) {
	req := &Request{
		AccessToken: "test-token",
		Headers: map[string]string{
			"User-Agent":        "test-agent/1.0",
			"Cookie":            "session=secret",
			"Referer":           "https://example.com",
			"Authorization":     "Bearer client-token",
			"X-Forwarded-For":   "1.2.3.4",
			"X-Forwarded-Proto": "https",
			"X-Custom":          "keep-me",
		},
	}
	headers := codexHeaders(req)

	// Required/default Codex headers remain.
	if got := headers["Content-Type"]; got != "application/json" {
		t.Errorf("Content-Type=%q, want application/json", got)
	}
	if got := headers["Accept"]; got != "text/event-stream" {
		t.Errorf("Accept=%q, want text/event-stream", got)
	}
	if got := headers["Originator"]; got != "codex-tui" {
		t.Errorf("Originator=%q, want codex-tui", got)
	}
	// Authorization must come from the executor's own credential, not the client.
	if got := headers["Authorization"]; got != "Bearer test-token" {
		t.Errorf("Authorization=%q, want Bearer test-token", got)
	}

	// Blocklisted headers are never forwarded upstream.
	for _, k := range []string{"Cookie", "Referer", "X-Forwarded-For", "X-Forwarded-Proto"} {
		if _, ok := headers[k]; ok {
			t.Errorf("expected %s to be stripped", k)
		}
	}
	// Allowed custom headers are preserved.
	if got := headers["X-Custom"]; got != "keep-me" {
		t.Errorf("X-Custom=%q, want keep-me", got)
	}
}

func TestCodexHeaders_RespectsCaseInsensitiveBlocklist(t *testing.T) {
	req := &Request{
		AccessToken: "test-token",
		Headers: map[string]string{
			"cookie":           "session=secret",
			"x-forwarded-host": "evil.com",
			"authorization":    "Bearer client-token",
		},
	}
	headers := codexHeaders(req)
	for _, k := range []string{"cookie", "x-forwarded-host"} {
		if _, ok := headers[k]; ok {
			t.Errorf("expected %s to be stripped (case-insensitive)", k)
		}
	}
}

func TestCodexCompactTimeout_Default(t *testing.T) {
	t.Setenv("CODEX_RESPONSES_COMPACT_TIMEOUT_MS", "")
	if got := codexCompactTimeout(); got != defaultCodexCompactTimeout {
		t.Errorf("codexCompactTimeout()=%v, want %v", got, defaultCodexCompactTimeout)
	}
}

func TestCodexCompactTimeout_EnvironmentOverride(t *testing.T) {
	t.Setenv("CODEX_RESPONSES_COMPACT_TIMEOUT_MS", "12345")
	if got := codexCompactTimeout(); got != 12345*time.Millisecond {
		t.Errorf("codexCompactTimeout()=%v, want 12345ms", got)
	}
}

func TestCodexCompactTimeout_RejectsInvalidEnv(t *testing.T) {
	for _, v := range []string{"not-a-number", "-1", "0"} {
		t.Run(v, func(t *testing.T) {
			t.Setenv("CODEX_RESPONSES_COMPACT_TIMEOUT_MS", v)
			if got := codexCompactTimeout(); got != defaultCodexCompactTimeout {
				t.Errorf("codexCompactTimeout()=%v, want default %v", got, defaultCodexCompactTimeout)
			}
		})
	}
}

func TestResponsesCompact_Timeout(t *testing.T) {
	// Use a slow server so the compact timeout fires before the response arrives.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{}`)
	}))
	defer ts.Close()

	t.Setenv("CODEX_RESPONSES_COMPACT_TIMEOUT_MS", "50")
	base := NewBaseExecutor()
	cx := NewCodexExecutor(base)
	req := &Request{
		Provider:    "cx",
		Model:       "gpt-5.4",
		BaseURL:     ts.URL,
		Body:        []byte(`{"input":[{"role":"user","content":"hi"}]}`),
		AccessToken: "test-token",
	}

	start := time.Now()
	_, err := cx.ResponsesCompact(context.Background(), req)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %T: %v", err, err)
	}
	if elapsed > 400*time.Millisecond {
		t.Fatalf("timeout took too long: %v", elapsed)
	}
}

func TestParseCodexEvent_JSONType(t *testing.T) {
	line := []byte(`data: {"type":"response.completed","response":{"id":"r1"}}`)
	data, eventType := parseCodexEvent(line)
	if string(data) != `{"type":"response.completed","response":{"id":"r1"}}` {
		t.Fatalf("unexpected data: %s", string(data))
	}
	if eventType != "response.completed" {
		t.Fatalf("eventType=%q, want response.completed", eventType)
	}
}

func TestParseCodexEvent_SSEEventFallback(t *testing.T) {
	// Upstream emits a typed event without a JSON type field.
	line := []byte("event: response.in_progress\ndata: {}")
	data, eventType := parseCodexEvent(line)
	if string(data) != `{}` {
		t.Fatalf("unexpected data: %s", string(data))
	}
	if eventType != "response.in_progress" {
		t.Fatalf("eventType=%q, want response.in_progress", eventType)
	}
}

func TestParseCodexEvent_NoTypeNoEvent(t *testing.T) {
	line := []byte(`data: {"foo":"bar"}`)
	data, eventType := parseCodexEvent(line)
	if string(data) != `{"foo":"bar"}` {
		t.Fatalf("unexpected data: %s", string(data))
	}
	if eventType != "" {
		t.Fatalf("eventType=%q, want empty", eventType)
	}
}

func TestCodexTelemetry_CountersIncrement(t *testing.T) {
	telemetry.DefaultCodexCounters.RequestsTotal.Store(0)
	telemetry.DefaultCodexCounters.IncompleteStreamsTotal.Store(0)
	telemetry.DefaultCodexCounters.ReplayHitsTotal.Store(0)
	telemetry.DefaultCodexCounters.IdentityConfuseTotal.Store(0)

	cache.ClearCodexReasoningReplayCache()
	connID := "telemetry-conn"
	model := "gpt-5.4"

	// Seed the cache using the *confused* session key so codexInjectReasoningReplay reports a replay hit.
	confused := codexIdentityConfuseUUID(connID, "prompt-cache", "k")
	item := []byte(`{"type":"reasoning","encrypted_content":"telemetry-reasoning"}`)
	_ = cache.CacheCodexReasoningReplayItems(context.Background(), model, "prompt-cache:"+confused, [][]byte{item})

	req := []byte(`{"model":"gpt-5.4","prompt_cache_key":"k","input":[{"type":"message","role":"user","content":"hi"}]}`)
	body := codexRequestBody(req)
	body = ensureImageGenerationTool(body, model, "")
	body, _ = applyCodexIdentityConfuseBody(body, connID)
	sessionKey := codexReasoningReplaySessionKey(body, nil)
	if sessionKey == "" {
		t.Fatalf("expected non-empty session key, got %q", sessionKey)
	}
	body, injected := codexInjectReasoningReplay(body, sessionKey)
	if !injected {
		t.Fatalf("expected replay injection so telemetry can count a hit; sessionKey=%s body=%s", sessionKey, string(body))
	}

	if telemetry.DefaultCodexCounters.IdentityConfuseTotal.Load() == 0 {
		t.Error("expected IdentityConfuseTotal to increment")
	}
	if telemetry.DefaultCodexCounters.ReplayHitsTotal.Load() == 0 {
		t.Error("expected ReplayHitsTotal to increment")
	}
}
