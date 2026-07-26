package v1

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/cache"
	"github.com/rickicode/AxonRouter-Go/internal/db"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/rickicode/AxonRouter-Go/internal/providercfg"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
	"github.com/tidwall/gjson"
)

func setupResponsesTest(t *testing.T) (*Handler, func()) {
	logging.Init("text")
	h := newTestHandler(t)
	wq := db.NewWriteQueue(h.db)
	tracker := usage.NewTracker(h.db)
	tracker.SetWriteQueue(wq)
	h.tracker = tracker

	if _, err := h.db.Exec(`INSERT OR IGNORE INTO provider_types (id, display_name, format, base_url, created_at) VALUES ('restest','ResTest','openai-responses','http://x',0)`); err != nil {
		t.Fatalf("seed provider_type: %v", err)
	}
	if _, err := h.db.Exec(`INSERT OR IGNORE INTO connections (id, provider_type_id, name, auth_type, status, is_active, created_at, updated_at, provider_specific_data) VALUES ('restest-conn1','restest','c1','none','ready',1,0,0,'{"account_id":"acc-restest"}'), ('restest-conn2','restest','c2','none','ready',1,0,0,'')`); err != nil {
		t.Fatalf("seed connection: %v", err)
	}
	h.store.SeedConnection("restest-conn1", "restest", "ready", 0)
	h.store.SeedConnection("restest-conn2", "restest", "ready", 0)
	h.elig.RecomputeAll()

	return h, func() {
		executor.GetRegistry().Unregister("restest")
	}
}

func intptr(n int) *int { return &n }

func TestResponses_UpstreamClientErrorPassedThrough(t *testing.T) {
	h, cleanup := setupResponsesTest(t)
	defer cleanup()

	upBody := []byte(`{"error":{"message":"context too long","type":"invalid_request_error","code":"context_length_exceeded"}}`)
	fe := &fakeExecutor{
		responses: []struct {
			resp *executor.Response
			err  error
		}{
			{nil, &executor.UpstreamError{StatusCode: http.StatusBadRequest, Body: upBody}},
		},
	}
	executor.GetRegistry().Register("restest", executor.FormatOpenAIResponses, fe)

	body := []byte(`{"model":"restest/model","input":"hi"}`)
	rec, c := jsonRequestWithAllowedModels(t, http.MethodPost, "/v1/responses", body, nil)
	h.Responses(c)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400: %s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("invalid response body: %v", err)
	}
	if got["error"].(map[string]any)["code"] != "context_length_exceeded" {
		t.Errorf("response=%v, want upstream error code", got)
	}
	if fe.callCount != 1 {
		t.Errorf("expected 1 upstream call, got %d", fe.callCount)
	}
}

func TestResponses_NonStreamSuccess(t *testing.T) {
	h, cleanup := setupResponsesTest(t)
	defer cleanup()

	fe := &fakeExecutor{
		responses: []struct {
			resp *executor.Response
			err  error
		}{
			{
				resp: &executor.Response{
					StatusCode: http.StatusOK,
					Body:       []byte(`{"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Hello"}]}]}`),
				},
			},
		},
	}
	executor.GetRegistry().Register("restest", executor.FormatOpenAIResponses, fe)

	body := []byte(`{"model":"restest/model","input":"hi"}`)
	rec, c := jsonRequestWithAllowedModels(t, http.MethodPost, "/v1/responses", body, nil)
	h.Responses(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"text":"Hello"`)) {
		t.Fatalf("expected response body to contain Hello, got %s", rec.Body.String())
	}
	if fe.callCount != 1 {
		t.Errorf("expected 1 upstream call, got %d", fe.callCount)
	}
	if rec.Header().Get("X-Cache-Status") != "MISS" {
		t.Errorf("expected cache miss header, got %q", rec.Header().Get("X-Cache-Status"))
	}
}

func TestResponses_StreamSuccess(t *testing.T) {
	h, cleanup := setupResponsesTest(t)
	defer cleanup()
	if err := h.providerCfg.Save("restest", providercfg.ProviderSettings{
		RoutingMode:   providercfg.RoundRobin,
		HoldbackMs:    intptr(10),
		HoldbackBytes: intptr(1),
	}); err != nil {
		t.Fatalf("save holdback settings: %v", err)
	}

	chunks := make(chan executor.StreamChunk, 1)
	chunks <- executor.StreamChunk{
		Payload: []byte(`{"type":"response.completed","response":{"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Hi"}]}]}}`),
	}
	close(chunks)

	fe := &fakeExecutor{
		streamResults: []struct {
			result *executor.StreamResult
			err    error
		}{
			{result: &executor.StreamResult{Chunks: chunks, StatusCode: http.StatusOK}},
		},
	}
	executor.GetRegistry().Register("restest", executor.FormatOpenAIResponses, fe)

	body := []byte(`{"model":"restest/model","input":"hi","stream":true}`)
	rec, c := jsonRequestWithAllowedModels(t, http.MethodPost, "/v1/responses", body, nil)
	h.Responses(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("expected text/event-stream, got %q", rec.Header().Get("Content-Type"))
	}
	bodyStr := rec.Body.String()
	if !strings.Contains(bodyStr, `"text":"Hi"`) {
		t.Fatalf("expected streamed content, got %s", bodyStr)
	}
	if !strings.Contains(bodyStr, "[DONE]") {
		t.Fatalf("expected terminal [DONE], got %s", bodyStr)
	}
}

func TestResponses_FailoverAfterHoldbackError(t *testing.T) {
	h, cleanup := setupResponsesTest(t)
	defer cleanup()
	if err := h.providerCfg.Save("restest", providercfg.ProviderSettings{
		RoutingMode:   providercfg.RoundRobin,
		HoldbackMs:    intptr(50),
		HoldbackBytes: intptr(1),
	}); err != nil {
		t.Fatalf("save holdback settings: %v", err)
	}

	errCh := make(chan executor.StreamChunk, 1)
	errCh <- executor.StreamChunk{Err: errors.New("boom")}
	close(errCh)

	okCh := make(chan executor.StreamChunk, 1)
	okCh <- executor.StreamChunk{
		Payload: []byte(`{"type":"response.completed","response":{"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Recovered"}]}]}}`),
	}
	close(okCh)

	fe := &fakeExecutor{
		streamResults: []struct {
			result *executor.StreamResult
			err    error
		}{
			{result: &executor.StreamResult{Chunks: errCh, StatusCode: http.StatusOK}},
			{result: &executor.StreamResult{Chunks: okCh, StatusCode: http.StatusOK}},
		},
	}
	executor.GetRegistry().Register("restest", executor.FormatOpenAIResponses, fe)

	body := []byte(`{"model":"restest/model","input":"hi","stream":true}`)
	rec, c := jsonRequestWithAllowedModels(t, http.MethodPost, "/v1/responses", body, nil)
	h.Responses(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200: %s", rec.Code, rec.Body.String())
	}
	if fe.callCount != 2 {
		t.Fatalf("expected 2 upstream calls after failover, got %d", fe.callCount)
	}
	if !strings.Contains(rec.Body.String(), `"text":"Recovered"`) {
		t.Fatalf("expected recovered content, got %s", rec.Body.String())
	}
}

func TestResponses_ExactCacheHit(t *testing.T) {
	h, cleanup := setupResponsesTest(t)
	defer cleanup()

	executor.GetRegistry().Register("restest", executor.FormatOpenAIResponses, &fakeExecutor{})

	body := []byte(`{"model":"restest/model","input":"hi"}`)
	key := cache.ComputeKey(body, "restest/model")
	h.exactCache.Set(key, cache.CacheEntry{
		Body:        []byte(`{"cached":true}`),
		StatusCode:  http.StatusOK,
		ContentType: "application/json",
	})

	rec, c := jsonRequestWithAllowedModels(t, http.MethodPost, "/v1/responses", body, nil)
	h.Responses(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Cache-Status") != "HIT" {
		t.Errorf("expected cache hit header, got %q", rec.Header().Get("X-Cache-Status"))
	}
	if string(rec.Body.Bytes()) != `{"cached":true}` {
		t.Errorf("expected cached body, got %s", rec.Body.String())
	}
}

func TestResponsesCompact_StreamRejected(t *testing.T) {
	h, cleanup := setupResponsesTest(t)
	defer cleanup()

	body := []byte(`{"model":"cx/gpt-5","input":"hi","stream":true}`)
	rec, c := jsonRequestWithAllowedModels(t, http.MethodPost, "/v1/responses/compact", body, nil)
	h.ResponsesCompact(c)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "streaming not supported") {
		t.Errorf("expected streaming rejection, got %s", rec.Body.String())
	}
}

func TestResponsesCompact_Success(t *testing.T) {
	h, cleanup := setupResponsesTest(t)
	defer cleanup()

	fe := &fakeExecutor{
		compactResponse: &executor.Response{
			StatusCode: http.StatusOK,
			Body:       []byte(`{"response":{"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Compacted"}]}]}}`),
		},
	}
	executor.GetRegistry().Register("cx", executor.FormatOpenAIResponses, fe)
	defer executor.RegisterDefaults()

	if _, err := h.db.Exec(`INSERT OR IGNORE INTO provider_types (id, display_name, format, base_url, created_at) VALUES ('cx','Codex','openai-responses','http://codex',0)`); err != nil {
		t.Fatalf("seed provider_type: %v", err)
	}
	if _, err := h.db.Exec(`INSERT OR IGNORE INTO connections (id, provider_type_id, name, auth_type, status, is_active, created_at, updated_at) VALUES ('cx-conn1','cx','c1','none','ready',1,0,0)`); err != nil {
		t.Fatalf("seed connection: %v", err)
	}
	h.store.SeedConnection("cx-conn1", "cx", "ready", 0)
	h.elig.RecomputeAll()

	body := []byte(`{"model":"cx/gpt-5","input":"hi"}`)
	rec, c := jsonRequestWithAllowedModels(t, http.MethodPost, "/v1/responses/compact", body, nil)
	h.ResponsesCompact(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"text":"Compacted"`)) {
		t.Errorf("expected compacted response, got %s", rec.Body.String())
	}
	if fe.callCount != 1 {
		t.Errorf("expected 1 compact call, got %d", fe.callCount)
	}
}

func TestResponsesWebsocket_UpgradeAndRelay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, cleanup := setupResponsesTest(t)
	defer cleanup()

	// Upstream Codex websocket server that echoes the first text message back.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer up.Close(websocket.StatusNormalClosure, "done")
		ctx := r.Context()
		typ, data, err := up.Read(ctx)
		if err != nil {
			return
		}
		if err := up.Write(ctx, typ, data); err != nil {
			return
		}
		// Wait for the client to close.
		for {
			_, _, err := up.Read(ctx)
			if err != nil {
				return
			}
		}
	}))
	defer upstream.Close()

	if _, err := h.db.Exec(`INSERT OR IGNORE INTO provider_types (id, display_name, format, base_url, created_at) VALUES ('cx','Codex','openai-responses',?,0)`, upstream.URL); err != nil {
		t.Fatalf("seed provider_type: %v", err)
	}
	if _, err := h.db.Exec(`UPDATE provider_types SET base_url = ? WHERE id = 'cx'`, upstream.URL); err != nil {
		t.Fatalf("update provider_type base_url: %v", err)
	}
	if _, err := h.db.Exec(`INSERT OR IGNORE INTO connections (id, provider_type_id, name, auth_type, status, is_active, created_at, updated_at) VALUES ('cx-ws-conn','cx','c-ws','none','ready',1,0,0)`); err != nil {
		t.Fatalf("seed connection: %v", err)
	}
	h.store.SeedConnection("cx-ws-conn", "cx", "ready", 0)
	h.elig.RecomputeAll()

	allowed := map[string]struct{}{"cx/gpt-5": {}}
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, _ := gin.CreateTestContext(w)
		c.Request = r.WithContext(context.WithValue(r.Context(), "allowed_models", allowed))
		h.ResponsesWebsocket(c)
	}))
	defer gateway.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	clientConn, _, err := websocket.Dial(ctx, strings.Replace(gateway.URL, "http:", "ws:", 1)+"/v1/responses", nil)
	if err != nil {
		t.Fatalf("dial gateway websocket: %v", err)
	}
	defer clientConn.Close(websocket.StatusNormalClosure, "done")

	first := []byte(`{"model":"cx/gpt-5","type":"response.create"}`)
	if err := clientConn.Write(ctx, websocket.MessageText, first); err != nil {
		t.Fatalf("write first message: %v", err)
	}

	typ, echo, err := clientConn.Read(ctx)
	if err != nil {
		t.Fatalf("read echoed message: %v", err)
	}
	if typ != websocket.MessageText {
		t.Fatalf("expected text message, got %d", typ)
	}
	if string(echo) != string(first) {
		t.Errorf("echo mismatch: got %s, want %s", string(echo), string(first))
	}
}

// captureExecutor records the last upstream request it receives.
type captureExecutor struct {
	fakeExecutor
	lastReq *executor.Request
}

func (c *captureExecutor) Execute(ctx context.Context, req *executor.Request) (*executor.Response, error) {
	c.lastReq = req
	return c.fakeExecutor.Execute(ctx, req)
}

func (c *captureExecutor) ExecuteStream(ctx context.Context, req *executor.Request) (*executor.StreamResult, error) {
	c.lastReq = req
	return c.fakeExecutor.ExecuteStream(ctx, req)
}

// setupProviderResponsesTest seeds a provider_type and connection for the given
// provider ID and format, registers the supplied executor, and returns a cleanup
// function that restores the original executor.
func setupProviderResponsesTest(t *testing.T, h *Handler, provider, format string, exec executor.Executor) func() {
	t.Helper()
	if _, err := h.db.Exec(`INSERT OR IGNORE INTO provider_types (id, display_name, format, base_url, created_at) VALUES (?,?,?,'http://x',0)`, provider, provider, format); err != nil {
		t.Fatalf("seed provider_type %s: %v", provider, err)
	}
	connID := provider + "-conn1"
	if _, err := h.db.Exec(`INSERT OR IGNORE INTO connections (id, provider_type_id, name, auth_type, status, is_active, created_at, updated_at) VALUES (?,?,'c1','none','ready',1,0,0)`, connID, provider); err != nil {
		t.Fatalf("seed connection %s: %v", provider, err)
	}
	h.store.SeedConnection(connID, provider, "ready", 0)
	h.elig.RecomputeAll()

	reg := executor.GetRegistry()
	origExec, origFormat, hadOriginal := reg.Get(provider)
	reg.Register(provider, executor.ProviderFormat(format), exec)
	return func() {
		if hadOriginal {
			reg.Register(provider, origFormat, origExec)
		} else {
			reg.Unregister(provider)
		}
	}
}

func TestResponses_ClaudelPrefix_RoutesAndTranslates(t *testing.T) {
	h, cleanup := setupResponsesTest(t)
	defer cleanup()

	fe := &captureExecutor{
		fakeExecutor: fakeExecutor{
			responses: []struct {
				resp *executor.Response
				err  error
			}{
				{
					resp: &executor.Response{
						StatusCode: http.StatusOK,
						Body: []byte(strings.Join([]string{
							`data: {"type":"message_start","message":{"id":"msg_claude_smoke","usage":{"input_tokens":5,"output_tokens":0}}}`,
							`data: {"type":"content_block_start","index":0,"content_block":{"type":"text"}}`,
							`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello from Claude"}}`,
							`data: {"type":"content_block_stop","index":0}`,
							`data: {"type":"message_delta","usage":{"output_tokens":3}}`,
							`data: {"type":"message_stop"}`,
						}, "\n") + "\n"),
					},
				},
			},
		},
	}
	defer setupProviderResponsesTest(t, h, "claude", string(executor.FormatClaude), fe)()

	body := []byte(`{"model":"claude/sonnet-4-20250514","input":"hi"}`)
	rec, c := jsonRequestWithAllowedModels(t, http.MethodPost, "/v1/responses", body, nil)
	h.Responses(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200: %s", rec.Code, rec.Body.String())
	}
	if fe.lastReq == nil {
		t.Fatalf("expected upstream request to be captured")
	}
	if !gjson.GetBytes(fe.lastReq.Body, "messages").Exists() {
		t.Fatalf("expected translated Claude request to contain messages, got %s", fe.lastReq.Body)
	}
	if gjson.GetBytes(fe.lastReq.Body, "model").String() != "sonnet-4-20250514" {
		t.Fatalf("expected model name to be stripped of prefix, got %s", fe.lastReq.Body)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"text":"Hello from Claude"`)) {
		t.Fatalf("expected OpenAI Responses output, got %s", rec.Body.String())
	}
}

func TestResponses_GeminiPrefix_RoutesAndTranslates(t *testing.T) {
	h, cleanup := setupResponsesTest(t)
	defer cleanup()

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

	body := []byte(`{"model":"gemini/gemini-2.5-pro","input":"hi"}`)
	rec, c := jsonRequestWithAllowedModels(t, http.MethodPost, "/v1/responses", body, nil)
	h.Responses(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200: %s", rec.Code, rec.Body.String())
	}
	if fe.lastReq == nil {
		t.Fatalf("expected upstream request to be captured")
	}
	if !gjson.GetBytes(fe.lastReq.Body, "contents").Exists() {
		t.Fatalf("expected translated Gemini request to contain contents, got %s", fe.lastReq.Body)
	}
	if gjson.GetBytes(fe.lastReq.Body, "model").String() != "gemini-2.5-pro" {
		t.Fatalf("expected model name to be stripped of prefix, got %s", fe.lastReq.Body)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"text":"Hello from Gemini"`)) {
		t.Fatalf("expected OpenAI Responses output, got %s", rec.Body.String())
	}
}

func TestResponses_AntigravityPrefix_RoutesAndTranslates(t *testing.T) {
	h, cleanup := setupResponsesTest(t)
	defer cleanup()

	fe := &captureExecutor{
		fakeExecutor: fakeExecutor{
			responses: []struct {
				resp *executor.Response
				err  error
			}{
				{
					resp: &executor.Response{
						StatusCode: http.StatusOK,
						Body: []byte(`{"response":{"responseId":"ag-1","modelVersion":"ag-model","candidates":[{"content":{"role":"model","parts":[{"text":"Hello from Antigravity"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":3,"totalTokenCount":8}}}`),
					},
				},
			},
		},
	}
	defer setupProviderResponsesTest(t, h, "ag", string(executor.FormatAntigravity), fe)()

	body := []byte(`{"model":"ag/gemini-2.5-pro","input":"hi"}`)
	rec, c := jsonRequestWithAllowedModels(t, http.MethodPost, "/v1/responses", body, nil)
	h.Responses(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200: %s", rec.Code, rec.Body.String())
	}
	if fe.lastReq == nil {
		t.Fatalf("expected upstream request to be captured")
	}
	if !gjson.GetBytes(fe.lastReq.Body, "contents").Exists() {
		t.Fatalf("expected translated Antigravity request to contain contents, got %s", fe.lastReq.Body)
	}
	if gjson.GetBytes(fe.lastReq.Body, "model").String() != "gemini-2.5-pro" {
		t.Fatalf("expected model name to be stripped of prefix, got %s", fe.lastReq.Body)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"text":"Hello from Antigravity"`)) {
		t.Fatalf("expected OpenAI Responses output, got %s", rec.Body.String())
	}
}

func TestResponses_OpenAICompatible_RoutesAndTranslates(t *testing.T) {
	h, cleanup := setupResponsesTest(t)
	defer cleanup()

	fe := &captureExecutor{
		fakeExecutor: fakeExecutor{
			responses: []struct {
				resp *executor.Response
				err  error
			}{
				{
					resp: &executor.Response{
						StatusCode: http.StatusOK,
						Body: []byte(`{"id":"chatcmpl-abc","object":"chat.completion","created":1234567890,"model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"Hello via chat"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":3,"total_tokens":13}}`),
					},
				},
			},
		},
	}
	defer setupProviderResponsesTest(t, h, "openai", string(executor.FormatOpenAI), fe)()

	body := []byte(`{"model":"openai/gpt-4o","input":"Hi there","temperature":0.7}`)
	rec, c := jsonRequestWithAllowedModels(t, http.MethodPost, "/v1/responses", body, nil)
	h.Responses(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200: %s", rec.Code, rec.Body.String())
	}
	if fe.lastReq == nil {
		t.Fatalf("expected upstream request to be captured")
	}
	if got := gjson.GetBytes(fe.lastReq.Body, "model").String(); got != "gpt-4o" {
		t.Errorf("upstream model=%q, want gpt-4o", got)
	}
	if !gjson.GetBytes(fe.lastReq.Body, "messages").Exists() {
		t.Fatalf("upstream body missing messages, got %s", fe.lastReq.Body)
	}
	if gjson.GetBytes(fe.lastReq.Body, "input").Exists() {
		t.Errorf("upstream body still contained Responses API 'input' field")
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"object":"response"`)) {
		t.Fatalf("client response not Responses API shape, got %s", rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"text":"Hello via chat"`)) {
		t.Fatalf("client response missing translated text, got %s", rec.Body.String())
	}
}

func TestResponses_CustomOpenAI_RoutesAndTranslates(t *testing.T) {
	h, cleanup := setupResponsesTest(t)
	defer cleanup()

	fe := &captureExecutor{
		fakeExecutor: fakeExecutor{
			responses: []struct {
				resp *executor.Response
				err  error
			}{
				{
					resp: &executor.Response{
						StatusCode: http.StatusOK,
						Body: []byte(`{"id":"chatcmpl-custom","object":"chat.completion","created":1234567890,"model":"custom-model","choices":[{"index":0,"message":{"role":"assistant","content":"Hello from custom OpenAI"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":4,"total_tokens":9}}`),
					},
				},
			},
		},
	}
	defer setupProviderResponsesTest(t, h, "restestopenai", string(executor.FormatOpenAI), fe)()

	body := []byte(`{"model":"restestopenai/custom-model","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"Hi"}]}]}`)
	rec, c := jsonRequestWithAllowedModels(t, http.MethodPost, "/v1/responses", body, nil)
	h.Responses(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200: %s", rec.Code, rec.Body.String())
	}
	if fe.lastReq == nil {
		t.Fatalf("expected upstream request to be captured")
	}
	if got := gjson.GetBytes(fe.lastReq.Body, "model").String(); got != "custom-model" {
		t.Errorf("upstream model=%q, want custom-model", got)
	}
	if !gjson.GetBytes(fe.lastReq.Body, "messages").Exists() {
		t.Fatalf("upstream body missing messages, got %s", fe.lastReq.Body)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"text":"Hello from custom OpenAI"`)) {
		t.Fatalf("client response missing translated text, got %s", rec.Body.String())
	}
}
