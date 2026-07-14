package v1

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/db"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
)

func TestResponses_UpstreamClientErrorPassedThrough(t *testing.T) {
	logging.Init("text")
	h := newTestHandler(t)
	wq := db.NewWriteQueue(h.db)
	tracker := usage.NewTracker(h.db)
	tracker.SetWriteQueue(wq)
	h.tracker = tracker

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
	defer executor.GetRegistry().Unregister("restest")

	if _, err := h.db.Exec(`INSERT OR IGNORE INTO provider_types (id, display_name, format, base_url, created_at) VALUES ('restest','ResTest','openai-responses','http://x',0)`); err != nil {
		t.Fatalf("seed provider_type: %v", err)
	}
	if _, err := h.db.Exec(`INSERT OR IGNORE INTO connections (id, provider_type_id, name, auth_type, status, is_active, created_at, updated_at) VALUES ('restest-conn','restest','c1','none','ready',1,0,0)`); err != nil {
		t.Fatalf("seed connection: %v", err)
	}
	h.store.SeedConnection("restest-conn", "restest", "ready", 0)
	h.elig.RecomputeAll()

	body := []byte(`{"model":"restest/model","input":"hi"}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

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
