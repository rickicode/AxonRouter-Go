package v1

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestBuildFusionJudgeBody_PreservesConversationHistory(t *testing.T) {
	originalReq := []byte(`{
		"model": "combo-fusion",
		"temperature": 0.7,
		"messages": [
			{"role": "system", "content": "You are a helpful assistant."},
			{"role": "user", "content": "What is the capital of France?"},
			{"role": "assistant", "content": "Paris."},
			{"role": "user", "content": "And Germany?"}
		]
	}`)

	panels := []fusionPanel{
		{modelID: "openai/gpt-4o", content: "Berlin is the capital of Germany."},
		{modelID: "claude/claude-sonnet-4", content: "The capital of Germany is Berlin."},
	}

	got := buildFusionJudgeBody(originalReq, panels, false)

	var out map[string]any
	if err := json.Unmarshal(got, &out); err != nil {
		t.Fatalf("judge body is not valid JSON: %v", err)
	}

	if out["model"] != "combo-fusion" {
		t.Errorf("model field changed: got %v, want combo-fusion", out["model"])
	}
	if out["temperature"] != 0.7 {
		t.Errorf("temperature field changed: got %v, want 0.7", out["temperature"])
	}

	messages, ok := out["messages"].([]any)
	if !ok {
		t.Fatalf("messages is not an array")
	}
	if len(messages) != 5 {
		t.Fatalf("expected 5 messages (4 original + 1 judge turn), got %d", len(messages))
	}

	// Verify original messages are preserved in order.
	expectOriginal := [4]map[string]string{
		{"role": "system", "content": "You are a helpful assistant."},
		{"role": "user", "content": "What is the capital of France?"},
		{"role": "assistant", "content": "Paris."},
		{"role": "user", "content": "And Germany?"},
	}
	for i, want := range expectOriginal {
		msg, ok := messages[i].(map[string]any)
		if !ok {
			t.Fatalf("message %d is not an object", i)
		}
		if msg["role"] != want["role"] {
			t.Errorf("message %d role: got %v, want %v", i, msg["role"], want["role"])
		}
		if msg["content"] != want["content"] {
			t.Errorf("message %d content: got %v, want %v", i, msg["content"], want["content"])
		}
	}

	// Verify judge directive is appended as a new user turn.
	judgeMsg, ok := messages[4].(map[string]any)
	if !ok {
		t.Fatalf("judge message is not an object")
	}
	if judgeMsg["role"] != "user" {
		t.Errorf("judge turn role: got %v, want user", judgeMsg["role"])
	}

	judgeContent, ok := judgeMsg["content"].(string)
	if !ok {
		t.Fatalf("judge content is not a string")
	}
	for _, want := range []string{
		"You are a synthesis assistant",
		"User question: And Germany?",
		"Source openai/gpt-4o",
		"Source claude/claude-sonnet-4",
		"Berlin is the capital of Germany.",
		"Synthesize the best answer.",
	} {
		if !strings.Contains(judgeContent, want) {
			t.Errorf("judge content missing %q\ncontent:\n%s", want, judgeContent)
		}
	}
}

func TestBuildFusionJudgeBody_AnonymizeSources(t *testing.T) {
	originalReq := []byte(`{"model":"combo","messages":[{"role":"user","content":"hello"}]}`)
	panels := []fusionPanel{
		{modelID: "openai/gpt-4o", content: "hi there"},
		{modelID: "claude/claude-sonnet-4", content: "greetings"},
	}

	got := buildFusionJudgeBody(originalReq, panels, true)

	var out map[string]any
	if err := json.Unmarshal(got, &out); err != nil {
		t.Fatalf("judge body is not valid JSON: %v", err)
	}

	messages := out["messages"].([]any)
	judgeContent := messages[1].(map[string]any)["content"].(string)

	if strings.Contains(judgeContent, "openai/gpt-4o") || strings.Contains(judgeContent, "claude/claude-sonnet-4") {
		t.Errorf("judge content should not contain model IDs when anonymized:\n%s", judgeContent)
	}
	if !strings.Contains(judgeContent, "Source 1") || !strings.Contains(judgeContent, "Source 2") {
		t.Errorf("judge content should use anonymous labels:\n%s", judgeContent)
	}
}

func TestExtractAssistantContent(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		expected string
	}{
		{
			name:     "openai string content",
			body:     `{"choices":[{"message":{"role":"assistant","content":"hello openai"}}]}`,
			expected: "hello openai",
		},
		{
			name:     "openai content array",
			body:     `{"choices":[{"message":{"content":[{"type":"text","text":"hello "},{"type":"text","text":"world"}]}}]}`,
			expected: "hello world",
		},
		{
			name:     "claude messages",
			body:     `{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"hello claude"},{"type":"thinking","thinking":"hidden"}]}`,
			expected: "hello claude",
		},
		{
			name:     "gemini generateContent",
			body:     `{"candidates":[{"content":{"parts":[{"text":"hello gemini"}]}}]}`,
			expected: "hello gemini",
		},
		{
			name:     "openai responses output_text",
			body:     `{"id":"resp_1","model":"gpt-test","output":[{"type":"reasoning","summary":[{"type":"summary_text","text":"step 1"}]},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello responses"}]}]}`,
			expected: "hello responses",
		},
		{
			name:     "fallback output_text",
			body:     `{"output_text":"plain output text"}`,
			expected: "plain output text",
		},
		{
			name:     "fallback text",
			body:     `{"text":"plain text"}`,
			expected: "plain text",
		},
		{
			name:     "empty choices",
			body:     `{"choices":[]}`,
			expected: "",
		},
		{
			name:     "invalid json",
			body:     `{not json`,
			expected: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractAssistantContent([]byte(tc.body))
			if got != tc.expected {
				t.Errorf("extractAssistantContent() = %q, want %q", got, tc.expected)
			}
		})
	}
}
