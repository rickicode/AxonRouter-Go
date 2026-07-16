package v1

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/db"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
)

type fakeSTTExecutor struct {
	body []byte
}

func (f *fakeSTTExecutor) Execute(ctx context.Context, req *executor.Request) (*executor.Response, error) {
	body := f.body
	if body == nil {
		body = []byte(`{"text":"hello","usage":{"prompt_tokens":7,"total_tokens":7}}`)
	}
	return &executor.Response{StatusCode: http.StatusOK, Body: body, Headers: http.Header{"Content-Type": []string{"application/json"}}}, nil
}

func (f *fakeSTTExecutor) ExecuteStream(ctx context.Context, req *executor.Request) (*executor.StreamResult, error) {
	return nil, nil
}

func TestSTT_UsageAccumulatorArgs(t *testing.T) {
	logging.Init("text")
	h := newTestHandler(t)
	h.sttExecutorFactory = func() executor.Executor { return &fakeSTTExecutor{} }
	wq := db.NewWriteQueue(h.db)
	tracker := usage.NewTracker(h.db)
	tracker.SetWriteQueue(wq)
	h.tracker = tracker
	defer func() {
		tracker.Stop()
		wq.Stop()
	}()

	seedProviderAndConnection(t, h, "openai", `["llm","audio"]`, "openai-stt-conn-usage", "http://unused")

	hash := mustHashKey(t, "sk-stt")
	if _, err := h.db.Exec(`INSERT OR IGNORE INTO api_keys (id, name, key_hash, created_at) VALUES ('key-stt', 'test', ?, 0)`, hash); err != nil {
		t.Fatalf("seed api_key: %v", err)
	}
	if _, err := h.db.Exec(`INSERT OR IGNORE INTO api_key_usage (api_key_id, total_tokens, updated_at) VALUES ('key-stt', 0, 0)`); err != nil {
		t.Fatalf("seed api_key_usage: %v", err)
	}

	var gotReqBody []byte
	var gotRespBody []byte
	var gotEstimateOutput bool
	h.usageAccumulator = func(apiKeyID string, reqBody, respBody []byte, estimateOutput bool) {
		if apiKeyID == "key-stt" {
			gotReqBody = reqBody
			gotRespBody = respBody
			gotEstimateOutput = estimateOutput
		}
	}

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	_ = writer.WriteField("model", "openai/whisper-1")
	part, _ := writer.CreateFormFile("file", "audio.wav")
	// Large fake audio payload — if it were passed as reqBody it would inflate tokens.
	_, _ = part.Write(bytes.Repeat([]byte("fake audio "), 1000))
	writer.Close()

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", &buf)
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	c.Set("api_key_id", "key-stt")

	h.STT(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if gotReqBody != nil {
		t.Errorf("expected reqBody=nil so audio bytes are not tokenized, got %d bytes", len(gotReqBody))
	}
	if gotEstimateOutput {
		t.Errorf("expected estimateOutput=false for STT, got true")
	}
	if len(gotRespBody) == 0 {
		t.Errorf("expected respBody to be the upstream JSON response")
	}
}

func TestSTT_UsageAccumulatesOnSuccess(t *testing.T) {
	logging.Init("text")
	h := newTestHandler(t)
	h.sttExecutorFactory = func() executor.Executor { return &fakeSTTExecutor{} }
	wq := db.NewWriteQueue(h.db)
	tracker := usage.NewTracker(h.db)
	tracker.SetWriteQueue(wq)
	h.tracker = tracker
	defer func() {
		tracker.Stop()
		wq.Stop()
	}()

	seedProviderAndConnection(t, h, "openai", `["llm","audio"]`, "openai-stt-conn-usage2", "http://unused")

	hash := mustHashKey(t, "sk-stt2")
	if _, err := h.db.Exec(`INSERT OR IGNORE INTO api_keys (id, name, key_hash, created_at) VALUES ('key-stt2', 'test', ?, 0)`, hash); err != nil {
		t.Fatalf("seed api_key: %v", err)
	}
	if _, err := h.db.Exec(`INSERT OR IGNORE INTO api_key_usage (api_key_id, total_tokens, updated_at) VALUES ('key-stt2', 0, 0)`); err != nil {
		t.Fatalf("seed api_key_usage: %v", err)
	}

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	_ = writer.WriteField("model", "openai/whisper-1")
	part, _ := writer.CreateFormFile("file", "audio.wav")
	_, _ = part.Write(bytes.Repeat([]byte("fake audio "), 1000))
	writer.Close()

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", &buf)
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	c.Set("api_key_id", "key-stt2")

	h.STT(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var total int64
	if err := h.db.QueryRow(`SELECT total_tokens FROM api_key_usage WHERE api_key_id = 'key-stt2'`).Scan(&total); err != nil {
		t.Fatalf("query api_key_usage: %v", err)
	}
	if total != 7 {
		t.Errorf("total_tokens = %d, want 7", total)
	}
}
