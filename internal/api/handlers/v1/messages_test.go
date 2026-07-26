package v1

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/cache"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/tidwall/gjson"
	_ "github.com/rickicode/AxonRouter-Go/internal/translator"
)

func setupMessagesTest(t *testing.T) *Handler {
	t.Helper()
	logging.Init("text")
	gin.SetMode(gin.TestMode)
	h := newTestHandler(t)
	t.Cleanup(executor.RegisterDefaults)
	return h
}

func claudeMessageSSE(eventType, data string) []byte {
	return []byte("event: " + eventType + "\ndata: " + data + "\n\n")
}

func TestCountTokens_OpenAICompatible(t *testing.T) {
	logging.Init("text")
	gin.SetMode(gin.TestMode)
	executor.GetRegistry().Register("openai", executor.FormatOpenAI, executor.NewOpenAIExecutor(executor.NewBaseExecutor()))
	defer executor.GetRegistry().Unregister("openai")

	h := newTestHandler(t)
	if _, err := h.db.Exec(`INSERT OR IGNORE INTO provider_types (id, display_name, format, base_url, created_at) VALUES ('openai','OpenAI','openai','http://x',0)`); err != nil {
		t.Fatalf("seed provider_type: %v", err)
	}
	if _, err := h.db.Exec(`INSERT OR IGNORE INTO connections (id, provider_type_id, name, auth_type, status, is_active, created_at, updated_at) VALUES ('openai-conn','openai','c1','none','ready',1,0,0)`); err != nil {
		t.Fatalf("seed connection: %v", err)
	}
	h.store.SeedConnection("openai-conn", "openai", "ready", 0)
	h.elig.RecomputeAll()

	body := []byte(`{
  "model": "openai/gpt-4o",
  "messages": [
    {"role": "system", "content": "You are a helpful assistant."},
    {"role": "user", "content": "Hello world"}
  ]
}`)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.CountTokens(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200: %s", rec.Code, rec.Body.String())
	}

	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("invalid response body: %v", err)
	}

	inputTokens, ok := got["input_tokens"].(float64)
	if !ok {
		t.Fatalf("response missing input_tokens: %v", got)
	}
	if inputTokens <= 0 {
		t.Errorf("expected positive input_tokens, got %v", inputTokens)
	}
}

func TestCountTokens_UnsupportedProvider(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newTestHandler(t)

	body := []byte(`{"model":"unsupported-test-provider/fake-model","messages":[{"role":"user","content":"hi"}]}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.CountTokens(c)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", rec.Code)
	}

	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("invalid response body: %v", err)
	}
	errType := got["error"].(map[string]any)["type"].(string)
	if errType != "invalid_request_error" {
		t.Errorf("error type=%s, want invalid_request_error", errType)
	}
}

func TestMessages_NonStreamSuccess_NativeClaude(t *testing.T) {
	h := setupMessagesTest(t)

	fe := &captureExecutor{
		fakeExecutor: fakeExecutor{
			responses: []struct {
				resp *executor.Response
				err  error
			}{
				{
					resp: &executor.Response{
						StatusCode: http.StatusOK,
						Body:       []byte(`{"id":"msg_01","type":"message","role":"assistant","model":"claude-sonnet-4-20250514","content":[{"type":"text","text":"Hello from Claude"}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":3}}`),
					},
				},
			},
		},
	}
	defer setupProviderResponsesTest(t, h, "claude", string(executor.FormatClaude), fe)()

	body := []byte(`{"model":"claude/sonnet-4-20250514","messages":[{"role":"user","content":"hi"}],"max_tokens":100}`)
	rec, c := jsonRequestWithAllowedModels(t, http.MethodPost, "/v1/messages", body, nil)
	h.Messages(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"text":"Hello from Claude"`)) {
		t.Errorf("expected Claude text in response, got %s", rec.Body.String())
	}
	if rec.Header().Get("X-Cache-Status") != "MISS" {
		t.Errorf("expected cache miss, got %q", rec.Header().Get("X-Cache-Status"))
	}
	if fe.lastReq == nil {
		t.Fatal("expected upstream request to be captured")
	}
	if gjson.GetBytes(fe.lastReq.Body, "model").String() != "sonnet-4-20250514" {
		t.Errorf("upstream model not stripped, got %s", fe.lastReq.Body)
	}
	if !gjson.GetBytes(fe.lastReq.Body, "messages").Exists() {
		t.Errorf("upstream missing messages, got %s", fe.lastReq.Body)
	}
}

func TestMessages_StreamSuccess_NativeClaude(t *testing.T) {
	h := setupMessagesTest(t)

	chunks := make(chan executor.StreamChunk, 4)
	chunks <- executor.StreamChunk{Payload: claudeMessageSSE("message_start", `{"type":"message_start","message":{"id":"msg_stream","usage":{"input_tokens":5,"output_tokens":0}}}`)}
	chunks <- executor.StreamChunk{Payload: claudeMessageSSE("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text"}}`)}
	chunks <- executor.StreamChunk{Payload: claudeMessageSSE("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello from Claude stream"}}`)}
	chunks <- executor.StreamChunk{Payload: claudeMessageSSE("message_delta", `{"type":"message_delta","usage":{"output_tokens":4}}`)}
	close(chunks)

	fe := &fakeExecutor{
		streamResults: []struct {
			result *executor.StreamResult
			err    error
		}{
			{result: &executor.StreamResult{Chunks: chunks, StatusCode: http.StatusOK}},
		},
	}
	defer setupProviderResponsesTest(t, h, "claude", string(executor.FormatClaude), fe)()

	body := []byte(`{"model":"claude/sonnet-4-20250514","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	rec, c := jsonRequestWithAllowedModels(t, http.MethodPost, "/v1/messages", body, nil)
	h.Messages(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type=%q, want text/event-stream", ct)
	}
	out := rec.Body.String()
	if !strings.Contains(out, "event: message_start") {
		t.Errorf("missing message_start event, got:\n%s", out)
	}
	if !strings.Contains(out, "event: content_block_delta") {
		t.Errorf("missing content_block_delta event, got:\n%s", out)
	}
	if !strings.Contains(out, "event: message_delta") {
		t.Errorf("missing message_delta event, got:\n%s", out)
	}
	if !strings.Contains(out, "data: [DONE]") {
		t.Errorf("missing terminal [DONE], got:\n%s", out)
	}
}

func TestMessages_OpenAITranslation(t *testing.T) {
	h := setupMessagesTest(t)

	fe := &captureExecutor{
		fakeExecutor: fakeExecutor{
			responses: []struct {
				resp *executor.Response
				err  error
			}{
				{
					resp: &executor.Response{
						StatusCode: http.StatusOK,
						Body:       []byte(`{"id":"chatcmpl-123","object":"chat.completion","created":1234567890,"model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"Hello via OpenAI"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":3,"total_tokens":13}}`),
					},
				},
			},
		},
	}
	defer setupProviderResponsesTest(t, h, "openai", string(executor.FormatOpenAI), fe)()

	body := []byte(`{"model":"openai/gpt-4o","messages":[{"role":"user","content":"hi"}],"max_tokens":100}`)
	rec, c := jsonRequestWithAllowedModels(t, http.MethodPost, "/v1/messages", body, nil)
	h.Messages(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200: %s", rec.Code, rec.Body.String())
	}
	if fe.lastReq == nil {
		t.Fatal("expected upstream request to be captured")
	}
	if got := gjson.GetBytes(fe.lastReq.Body, "model").String(); got != "gpt-4o" {
		t.Errorf("upstream model=%q, want gpt-4o", got)
	}
	if !gjson.GetBytes(fe.lastReq.Body, "messages").Exists() {
		t.Fatalf("upstream missing messages, got %s", fe.lastReq.Body)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"type":"message"`)) {
		t.Errorf("expected Claude-shape response, got %s", rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"text":"Hello via OpenAI"`)) {
		t.Errorf("missing translated text, got %s", rec.Body.String())
	}
}

func TestMessages_GeminiTranslation(t *testing.T) {
	h := setupMessagesTest(t)

	fe := &captureExecutor{
		fakeExecutor: fakeExecutor{
			responses: []struct {
				resp *executor.Response
				err  error
			}{
				{
					resp: &executor.Response{
						StatusCode: http.StatusOK,
						Body: []byte(`{
							"modelVersion": "gemini-2.5-pro",
							"candidates": [{"content": {"role": "model", "parts": [{"text": "Hello from Gemini"}]}, "finishReason": "STOP"}],
							"usageMetadata": {"promptTokenCount": 5, "candidatesTokenCount": 3, "totalTokenCount": 8}
						}`),
					},
				},
			},
		},
	}
	defer setupProviderResponsesTest(t, h, "gemini", string(executor.FormatGemini), fe)()

	body := []byte(`{"model":"gemini/gemini-2.5-pro","messages":[{"role":"user","content":"hi"}],"max_tokens":100}`)
	rec, c := jsonRequestWithAllowedModels(t, http.MethodPost, "/v1/messages", body, nil)
	h.Messages(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200: %s", rec.Code, rec.Body.String())
	}
	if fe.lastReq == nil {
		t.Fatal("expected upstream request to be captured")
	}
	if !gjson.GetBytes(fe.lastReq.Body, "contents").Exists() {
		t.Fatalf("expected Gemini request to contain contents, got %s", fe.lastReq.Body)
	}
	if gjson.GetBytes(fe.lastReq.Body, "model").String() != "gemini-2.5-pro" {
		t.Errorf("upstream model not stripped, got %s", fe.lastReq.Body)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"text":"Hello from Gemini"`)) {
		t.Errorf("missing translated text, got %s", rec.Body.String())
	}
}

func TestMessages_CF_Translation(t *testing.T) {
	h := setupMessagesTest(t)

	fe := &captureExecutor{
		fakeExecutor: fakeExecutor{
			responses: []struct {
				resp *executor.Response
				err  error
			}{
				{
					resp: &executor.Response{
						StatusCode: http.StatusOK,
						Body:       []byte(`{"id":"chatcmpl-cf","object":"chat.completion","created":1234567890,"model":"cf-model","choices":[{"index":0,"message":{"role":"assistant","content":"Hello via Cloudflare"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":4,"total_tokens":9}}`),
					},
				},
			},
		},
	}
	defer setupProviderResponsesTest(t, h, "cf", string(executor.FormatOpenAI), fe)()

	body := []byte(`{"model":"cf/llama-model","messages":[{"role":"user","content":"hi"}],"max_tokens":100}`)
	rec, c := jsonRequestWithAllowedModels(t, http.MethodPost, "/v1/messages", body, nil)
	h.Messages(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200: %s", rec.Code, rec.Body.String())
	}
	if fe.lastReq == nil {
		t.Fatal("expected upstream request to be captured")
	}
	if got := gjson.GetBytes(fe.lastReq.Body, "model").String(); got != "llama-model" {
		t.Errorf("upstream model=%q, want llama-model", got)
	}
	if !gjson.GetBytes(fe.lastReq.Body, "messages").Exists() {
		t.Fatalf("upstream missing messages, got %s", fe.lastReq.Body)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"text":"Hello via Cloudflare"`)) {
		t.Errorf("missing translated text, got %s", rec.Body.String())
	}
}

func TestMessages_UpstreamErrorPassedThrough(t *testing.T) {
	h := setupMessagesTest(t)

	upBody := []byte(`{"error":{"message":"rate limited","type":"rate_limit_error","code":"rate_limit_exceeded"}}`)
	fe := &fakeExecutor{
		responses: []struct {
			resp *executor.Response
			err  error
		}{
			{nil, &executor.UpstreamError{StatusCode: http.StatusTooManyRequests, Body: upBody}},
		},
	}
	defer setupProviderResponsesTest(t, h, "openai", string(executor.FormatOpenAI), fe)()

	body := []byte(`{"model":"openai/gpt-4o","messages":[{"role":"user","content":"hi"}]}`)
	rec, c := jsonRequestWithAllowedModels(t, http.MethodPost, "/v1/messages", body, nil)
	h.Messages(c)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status=%d, want 429: %s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("invalid response body: %v", err)
	}
	inner := got["error"].(map[string]any)
	if inner["type"] != "rate_limit_error" {
		t.Errorf("error type=%v, want rate_limit_error", inner["type"])
	}
	if !strings.Contains(inner["message"].(string), "rate limited") {
		t.Errorf("error message missing upstream text: %v", inner)
	}
}

func TestMessages_MidStreamError_EmitsEventError(t *testing.T) {
	h := setupMessagesTest(t)
	h.failoverMaxAttempts = 1

	chunks := make(chan executor.StreamChunk, 2)
	chunks <- executor.StreamChunk{Payload: claudeMessageSSE("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial"}}`)}
	chunks <- executor.StreamChunk{Err: errors.New("simulated upstream failure")}
	close(chunks)

	fe := &fakeExecutor{
		streamResults: []struct {
			result *executor.StreamResult
			err    error
		}{
			{result: &executor.StreamResult{Chunks: chunks, StatusCode: http.StatusOK}},
		},
	}
	defer setupProviderResponsesTest(t, h, "claude", string(executor.FormatClaude), fe)()

	body := []byte(`{"model":"claude/sonnet-4-20250514","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	rec, c := jsonRequestWithAllowedModels(t, http.MethodPost, "/v1/messages", body, nil)
	h.Messages(c)

	out := rec.Body.String()
	if !strings.Contains(out, "event: error") {
		t.Errorf("expected SSE event: error, got:\n%s", out)
	}
	if !strings.Contains(out, `"type":"error"`) {
		t.Errorf("expected Anthropic error payload, got:\n%s", out)
	}
	if strings.Contains(out, "data: [DONE]") {
		t.Errorf("error stream should not emit [DONE], got:\n%s", out)
	}
}

func TestMessages_ExactCacheHit(t *testing.T) {
	h := setupMessagesTest(t)

	body := []byte(`{"model":"claude/sonnet-4-20250514","messages":[{"role":"user","content":"cached hi"}],"max_tokens":100}`)
	key := cache.ComputeKey(body, "claude/sonnet-4-20250514")
	h.exactCache.Set(key, cache.CacheEntry{
		Body:        []byte(`{"id":"msg_cached","type":"message","role":"assistant","content":[{"type":"text","text":"Cached"}],"usage":{"input_tokens":1,"output_tokens":1}}`),
		StatusCode:  http.StatusOK,
		ContentType: "application/json",
	})

	rec, c := jsonRequestWithAllowedModels(t, http.MethodPost, "/v1/messages", body, nil)
	h.Messages(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Cache-Status") != "HIT" {
		t.Errorf("expected cache hit, got %q", rec.Header().Get("X-Cache-Status"))
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"text":"Cached"`)) {
		t.Errorf("expected cached body, got %s", rec.Body.String())
	}
}

func TestMessages_ModelNotAllowed_ReturnsForbidden(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newTestHandler(t)

	body := []byte(`{"model":"openai/gpt-4o","messages":[{"role":"user","content":"hi"}]}`)
	allowed := map[string]struct{}{"claude": {}}
	rec, c := jsonRequestWithAllowedModels(t, http.MethodPost, "/v1/messages", body, allowed)
	h.Messages(c)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status=%d, want 403: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "model not allowed for this API key") {
		t.Errorf("expected forbidden message, got %s", rec.Body.String())
	}
}
