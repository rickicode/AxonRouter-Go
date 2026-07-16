package v1

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/db"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
)

type fakeTTSExecutor struct{}

func (f *fakeTTSExecutor) Execute(ctx context.Context, req *executor.Request) (*executor.Response, error) {
	return &executor.Response{StatusCode: http.StatusOK, Body: []byte("fake audio bytes"), Headers: http.Header{"Content-Type": []string{"audio/mpeg"}}}, nil
}

func (f *fakeTTSExecutor) ExecuteStream(ctx context.Context, req *executor.Request) (*executor.StreamResult, error) {
	return nil, nil
}

func TestTTS_UsageAccumulatesOnSuccess(t *testing.T) {
	logging.Init("text")
	h := newTestHandler(t)
	h.ttsExecutorFactory = func() executor.Executor { return &fakeTTSExecutor{} }
	wq := db.NewWriteQueue(h.db)
	tracker := usage.NewTracker(h.db)
	tracker.SetWriteQueue(wq)
	h.tracker = tracker
	defer func() {
		tracker.Stop()
		wq.Stop()
	}()

	seedProviderAndConnection(t, h, "openai", `["llm","audio"]`, "openai-tts-conn-usage", "http://unused")

	hash := mustHashKey(t, "sk-tts")
	if _, err := h.db.Exec(`INSERT OR IGNORE INTO api_keys (id, name, key_hash, created_at) VALUES ('key-tts', 'test', ?, 0)`, hash); err != nil {
		t.Fatalf("seed api_key: %v", err)
	}
	if _, err := h.db.Exec(`INSERT OR IGNORE INTO api_key_usage (api_key_id, total_tokens, updated_at) VALUES ('key-tts', 0, 0)`); err != nil {
		t.Fatalf("seed api_key_usage: %v", err)
	}

	body := []byte(`{"model":"openai/tts-1","input":"hello","voice":"alloy"}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("api_key_id", "key-tts")

	h.TTS(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var total int64
	if err := h.db.QueryRow(`SELECT total_tokens FROM api_key_usage WHERE api_key_id = 'key-tts'`).Scan(&total); err != nil {
		t.Fatalf("query api_key_usage: %v", err)
	}
	if total == 0 {
		t.Errorf("expected non-zero total_tokens for TTS request input, got 0")
	}
}
