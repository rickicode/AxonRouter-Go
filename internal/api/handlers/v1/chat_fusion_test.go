package v1

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/combo"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
)

// setupFusionTestHandler builds a handler seeded with three "testprov"
// connections (two panels and one judge), an API key, and a fusion combo.
func setupFusionTestHandler(t *testing.T) *Handler {
	t.Helper()
	logging.Init("text")
	h := newTestHandler(t)
	seedProviderAndConnection(t, h, "testprov", `["llm"]`, "panel-conn-1", "http://unused")
	seedProviderAndConnection(t, h, "testprov", `["llm"]`, "panel-conn-2", "http://unused")
	seedProviderAndConnection(t, h, "testprov", `["llm"]`, "judge-conn", "http://unused")
	hash := mustHashKey(t, "sk-test")
	if _, err := h.db.Exec(`INSERT OR IGNORE INTO api_keys (id, name, key_hash, created_at) VALUES ('key-test', 'test', ?, 0)`, hash); err != nil {
		t.Fatalf("seed api_key: %v", err)
	}
	fusionConfig := `{"judge_model":"testprov/gpt-judge","min_panel":2,"straggler_grace_ms":1000,"panel_hard_timeout_ms":30000,"anonymize_sources":true}`
	if _, err := h.combo.CreateCombo("fusion-judge", "fusion", 30000, 1, false, "", fusionConfig, []combo.CreateStepInput{
		{ConnectionID: "panel-conn-1", ModelID: "testprov/gpt-panel-1", Priority: 1, Weight: 100},
		{ConnectionID: "panel-conn-2", ModelID: "testprov/gpt-panel-2", Priority: 2, Weight: 100},
	}); err != nil {
		t.Fatalf("create combo: %v", err)
	}
	return h
}

// fusionFakeExecutor is a concurrent-safe executor that dispenses responses by
// request model. Panels and the judge run in parallel during fusion, so the
// fake executor must be safe for concurrent Execute calls.
type fusionFakeExecutor struct {
	mu        sync.Mutex
	calls     []string
	responses map[string]*executor.Response
}

func (f *fusionFakeExecutor) Execute(ctx context.Context, req *executor.Request) (*executor.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, req.Model)
	resp, ok := f.responses[req.Model]
	if !ok {
		return nil, errors.New("unexpected model: " + req.Model)
	}
	return resp, nil
}

func (f *fusionFakeExecutor) ExecuteStream(ctx context.Context, req *executor.Request) (*executor.StreamResult, error) {
	return nil, errors.New("streaming not supported")
}

func fusionPanelResponse(content string) *executor.Response {
	return &executor.Response{
		StatusCode: http.StatusOK,
		Body: []byte(`{
"id":"chatcmpl-panel","object":"chat.completion","created":1,"model":"gpt-test",
"choices":[{"index":0,"message":{"role":"assistant","content":"` + content + `"},"finish_reason":"stop"}],
"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}
}`),
	}
}

func fusionJudge500Response() *executor.Response {
	return &executor.Response{
		StatusCode: http.StatusServiceUnavailable,
		Body:       []byte("service unavailable"),
	}
}

func fusionJudgeErrorBodyResponse() *executor.Response {
	return &executor.Response{
		StatusCode: http.StatusOK,
		Body:       []byte(`{"error":{"message":"rate limit exceeded","type":"rate_limit_error"}}`),
	}
}

func runFusionChatRequest(t *testing.T, h *Handler) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"fusion-judge","messages":[{"role":"user","content":"hello"}]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("api_key_id", "key-test")
	h.ChatCompletions(c)
	return rec
}

func assertFusionFallbackToPanel(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if body == "" {
		t.Fatal("expected non-empty response body")
	}
	if !strings.Contains(body, "panel-") {
		t.Errorf("expected fallback to a panel answer, got body: %s", body)
	}
	if strings.Contains(body, "service unavailable") || strings.Contains(body, "rate limit exceeded") {
		t.Errorf("expected fallback response, got raw upstream error: %s", body)
	}
}

func TestHandleFusionRequest_Judge500_FallsBackToFirstPanel(t *testing.T) {
	h := setupFusionTestHandler(t)
	fe := &fusionFakeExecutor{
		responses: map[string]*executor.Response{
			"gpt-panel-1": fusionPanelResponse("panel-1"),
			"gpt-panel-2": fusionPanelResponse("panel-2"),
			"gpt-judge":   fusionJudge500Response(),
		},
	}
	executor.GetRegistry().Register("testprov", executor.FormatOpenAI, fe)
	defer executor.GetRegistry().Unregister("testprov")

	rec := runFusionChatRequest(t, h)
	assertFusionFallbackToPanel(t, rec)
	if len(fe.calls) != 3 {
		t.Errorf("expected 3 executor calls (2 panels + judge), got %d", len(fe.calls))
	}
}

func TestHandleFusionRequest_JudgeUpstreamErrorBody_FallsBackToFirstPanel(t *testing.T) {
	h := setupFusionTestHandler(t)
	fe := &fusionFakeExecutor{
		responses: map[string]*executor.Response{
			"gpt-panel-1": fusionPanelResponse("panel-1"),
			"gpt-panel-2": fusionPanelResponse("panel-2"),
			"gpt-judge":   fusionJudgeErrorBodyResponse(),
		},
	}
	executor.GetRegistry().Register("testprov", executor.FormatOpenAI, fe)
	defer executor.GetRegistry().Unregister("testprov")

	rec := runFusionChatRequest(t, h)
	assertFusionFallbackToPanel(t, rec)
	if len(fe.calls) != 3 {
		t.Errorf("expected 3 executor calls (2 panels + judge), got %d", len(fe.calls))
	}
}
