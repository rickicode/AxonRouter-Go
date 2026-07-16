package v1

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/db"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
)

func seedBudgetExhausted(t *testing.T, h *Handler) {
	t.Helper()
	if _, err := h.db.Exec(`INSERT OR IGNORE INTO api_keys (id, key_hash, key_value, name, rate_limit_per_min, max_tokens, is_active, created_at) VALUES ('key-budget', 'hash', 'secret', 'Budget Key', 0, 100, 1, 0)`); err != nil {
		t.Fatalf("seed api key: %v", err)
	}
	if _, err := h.db.Exec(`INSERT INTO api_key_usage (api_key_id, total_tokens, updated_at) VALUES ('key-budget', 100, 0) ON CONFLICT(api_key_id) DO UPDATE SET total_tokens = excluded.total_tokens`); err != nil {
		t.Fatalf("seed usage: %v", err)
	}
}

func seedBudgetAvailable(t *testing.T, h *Handler) {
	t.Helper()
	if _, err := h.db.Exec(`INSERT OR IGNORE INTO api_keys (id, key_hash, key_value, name, rate_limit_per_min, max_tokens, is_active, created_at) VALUES ('key-budget', 'hash', 'secret', 'Budget Key', 0, 100, 1, 0)`); err != nil {
		t.Fatalf("seed api key: %v", err)
	}
	if _, err := h.db.Exec(`INSERT INTO api_key_usage (api_key_id, total_tokens, updated_at) VALUES ('key-budget', 0, 0) ON CONFLICT(api_key_id) DO UPDATE SET total_tokens = excluded.total_tokens`); err != nil {
		t.Fatalf("seed usage: %v", err)
	}
}

func budgetContext(t *testing.T, method, path string, body []byte) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(method, path, bytes.NewReader(body))
	if body != nil {
		c.Request.Header.Set("Content-Type", "application/json")
	}
	c.Set("api_key_id", "key-budget")
	c.Set("max_tokens", int64(100))
	return c, rec
}

func TestEmbeddings_TokenBudgetExhausted(t *testing.T) {
	logging.Init("text")
	h := newTestHandler(t)
	wq := db.NewWriteQueue(h.db)
	tracker := usage.NewTracker(h.db)
	tracker.SetWriteQueue(wq)
	h.tracker = tracker
	defer func() {
		tracker.Stop()
		wq.Stop()
	}()
	seedBudgetExhausted(t, h)

	fe := &fakeEmbeddingsExecutor{BaseExecutor: executor.NewBaseExecutor()}
	executor.GetRegistry().Register("openai", executor.FormatOpenAI, fe)
	defer executor.GetRegistry().Unregister("openai")

	seedProviderAndConnection(t, h, "openai", `["llm","embedding"]`, "openai-emb-conn", "http://unused")

	body := []byte(`{"model":"openai/text-embedding-3-small","input":"hello"}`)
	c, rec := budgetContext(t, http.MethodPost, "/v1/embeddings", body)
	c.Request.Header.Set("Content-Type", "application/json")

	h.Embeddings(c)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected status 429, got %d: %s", rec.Code, rec.Body.String())
	}
	if fe.called {
		t.Fatal("expected Embeddings not to be called when token budget is exhausted")
	}
}

func TestImages_TokenBudgetExhausted(t *testing.T) {
	logging.Init("text")
	h := newTestHandler(t)
	wq := db.NewWriteQueue(h.db)
	tracker := usage.NewTracker(h.db)
	tracker.SetWriteQueue(wq)
	h.tracker = tracker
	defer func() {
		tracker.Stop()
		wq.Stop()
	}()
	seedBudgetExhausted(t, h)

	fg := &fakeImageGenerator{BaseExecutor: executor.NewBaseExecutor()}
	executor.GetRegistry().Register("openai", executor.FormatOpenAI, fg)
	defer executor.GetRegistry().Unregister("openai")

	seedProviderAndConnection(t, h, "openai", `["llm","image"]`, "openai-img-conn", "http://unused")

	body := []byte(`{"model":"openai/dall-e-3","prompt":"a cat"}`)
	c, rec := budgetContext(t, http.MethodPost, "/v1/images/generations", body)
	c.Request.Header.Set("Content-Type", "application/json")

	h.Images(c)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected status 429, got %d: %s", rec.Code, rec.Body.String())
	}
	if fg.called {
		t.Fatal("expected Images not to be called when token budget is exhausted")
	}
}

func TestTTS_TokenBudgetExhausted(t *testing.T) {
	logging.Init("text")
	h := newTestHandler(t)
	wq := db.NewWriteQueue(h.db)
	tracker := usage.NewTracker(h.db)
	tracker.SetWriteQueue(wq)
	h.tracker = tracker
	defer func() {
		tracker.Stop()
		wq.Stop()
	}()
	seedBudgetExhausted(t, h)

	seedProviderAndConnection(t, h, "openai", `["llm","audio"]`, "openai-tts-conn", "http://unused")

	body := []byte(`{"model":"openai/tts-1","input":"hello","voice":"alloy"}`)
	c, rec := budgetContext(t, http.MethodPost, "/v1/audio/speech", body)
	c.Request.Header.Set("Content-Type", "application/json")

	h.TTS(c)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected status 429, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestVideo_TokenBudgetExhausted(t *testing.T) {
	logging.Init("text")
	h := newTestHandler(t)
	wq := db.NewWriteQueue(h.db)
	tracker := usage.NewTracker(h.db)
	tracker.SetWriteQueue(wq)
	h.tracker = tracker
	defer func() {
		tracker.Stop()
		wq.Stop()
	}()
	seedBudgetExhausted(t, h)

	seedProviderAndConnection(t, h, "openai", `["llm","video"]`, "openai-video-conn", "http://unused")

	body := []byte(`{"model":"openai/gpt-4o-video","prompt":"a cat"}`)
	c, rec := budgetContext(t, http.MethodPost, "/v1/video/generations", body)
	c.Request.Header.Set("Content-Type", "application/json")

	h.Video(c)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected status 429, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestResponses_TokenBudgetExhausted(t *testing.T) {
	logging.Init("text")
	h := newTestHandler(t)
	wq := db.NewWriteQueue(h.db)
	tracker := usage.NewTracker(h.db)
	tracker.SetWriteQueue(wq)
	h.tracker = tracker
	defer func() {
		tracker.Stop()
		wq.Stop()
	}()
	seedBudgetExhausted(t, h)

	seedProviderAndConnection(t, h, "openai", `["llm"]`, "openai-resp-conn", "http://unused")

	body := []byte(`{"model":"openai/gpt-4o","input":"hello"}`)
	c, rec := budgetContext(t, http.MethodPost, "/v1/responses", body)
	c.Request.Header.Set("Content-Type", "application/json")

	h.Responses(c)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected status 429, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUnified_TokenBudgetExhausted(t *testing.T) {
	logging.Init("text")
	h := newTestHandler(t)
	seedBudgetExhausted(t, h)

	body := []byte(`{"mode":"text","model":"openai/gpt-4o","messages":[{"role":"user","content":"hi"}]}`)
	c, rec := budgetContext(t, http.MethodPost, "/v1/unified", body)
	c.Request.Header.Set("Content-Type", "application/json")

	h.Unified(c)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected status 429, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUnified_TokenBudgetRequestedMaxCompletionTokens(t *testing.T) {
	logging.Init("text")
	h := newTestHandler(t)
	seedBudgetAvailable(t, h)

	body := []byte(`{"max_completion_tokens":150}`)
	c, rec := budgetContext(t, http.MethodPost, "/v1/unified", body)

	h.Unified(c)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "request_exceeds_api_key_token_budget") {
		t.Fatalf("expected request_exceeds_api_key_token_budget, got %s", rec.Body.String())
	}
}

func TestUnified_TokenBudgetRequestedMaxOutputTokens(t *testing.T) {
	logging.Init("text")
	h := newTestHandler(t)
	seedBudgetAvailable(t, h)

	body := []byte(`{"max_output_tokens":150}`)
	c, rec := budgetContext(t, http.MethodPost, "/v1/unified", body)

	h.Unified(c)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "request_exceeds_api_key_token_budget") {
		t.Fatalf("expected request_exceeds_api_key_token_budget, got %s", rec.Body.String())
	}
}

func TestChatCompletions_TokenBudgetRequestedMaxOutputTokens(t *testing.T) {
	logging.Init("text")
	h := newTestHandler(t)
	seedBudgetAvailable(t, h)

	fe := &fakeExecutor{}
	executor.GetRegistry().Register("bgtok", executor.FormatOpenAI, fe)
	defer executor.GetRegistry().Unregister("bgtok")

	seedProviderAndConnection(t, h, "bgtok", `["llm"]`, "bgtok-conn", "http://unused")

	body := []byte(`{"model":"bgtok/model","messages":[{"role":"user","content":"hi"}],"max_output_tokens":150}`)
	c, rec := budgetContext(t, http.MethodPost, "/v1/chat/completions", body)

	h.ChatCompletions(c)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "request_exceeds_api_key_token_budget") {
		t.Fatalf("expected request_exceeds_api_key_token_budget, got %s", rec.Body.String())
	}
	if fe.callCount != 0 {
		t.Fatalf("expected upstream not to be called when pre-check rejects, got %d calls", fe.callCount)
	}
}

func TestCheckTokenBudget_AbortsContext(t *testing.T) {
	logging.Init("text")
	h := newTestHandler(t)
	seedBudgetExhausted(t, h)

	c, rec := budgetContext(t, http.MethodPost, "/v1/unified", []byte(`{}`))
	if err := h.checkTokenBudget(c, nil); err == nil {
		t.Fatal("expected checkTokenBudget to return error")
	}
	if !c.IsAborted() {
		t.Errorf("expected context to be aborted")
	}
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("expected status 429, got %d", rec.Code)
	}
}

func TestSTT_TokenBudgetExhausted(t *testing.T) {
	logging.Init("text")
	h := newTestHandler(t)
	wq := db.NewWriteQueue(h.db)
	tracker := usage.NewTracker(h.db)
	tracker.SetWriteQueue(wq)
	h.tracker = tracker
	defer func() {
		tracker.Stop()
		wq.Stop()
	}()
	seedBudgetExhausted(t, h)

	seedProviderAndConnection(t, h, "openai", `["llm","audio"]`, "openai-stt-conn", "http://unused")

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	_ = writer.WriteField("model", "openai/whisper-1")
	part, _ := writer.CreateFormFile("file", "audio.wav")
	_, _ = part.Write([]byte("fake audio"))
	writer.Close()

	c, rec := budgetContext(t, http.MethodPost, "/v1/audio/transcriptions", buf.Bytes())
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())

	h.STT(c)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected status 429, got %d: %s", rec.Code, rec.Body.String())
	}
}
