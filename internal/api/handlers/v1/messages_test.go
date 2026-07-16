package v1

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	_ "github.com/rickicode/AxonRouter-Go/internal/translator"
)

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

	body := []byte(`{"model":"kiro/fake-model","messages":[{"role":"user","content":"hi"}]}`)
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
