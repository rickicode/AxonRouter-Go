package v1

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/combo"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
)

// setupComboTestHandler builds a handler seeded with two "testprov" connections,
// an API key, and a priority combo named "test-combo" that tries conn-1 then conn-2.
func setupComboTestHandler(t *testing.T) *Handler {
	t.Helper()
	logging.Init("text")
	h := newTestHandler(t)

	seedProviderAndConnection(t, h, "testprov", `["llm"]`, "conn-1", "http://unused")
	seedProviderAndConnection(t, h, "testprov", `["llm"]`, "conn-2", "http://unused")

	hash := mustHashKey(t, "sk-test")
	if _, err := h.db.Exec(`INSERT OR IGNORE INTO api_keys (id, name, key_hash, created_at) VALUES ('key-test', 'test', ?, 0)`, hash); err != nil {
		t.Fatalf("seed api_key: %v", err)
	}

	if _, err := h.combo.CreateCombo("test-combo", "priority", 30000, 1, false, "", "", []combo.CreateStepInput{
		{ConnectionID: "conn-1", ModelID: "testprov/gpt-test", Priority: 1, Weight: 100},
		{ConnectionID: "conn-2", ModelID: "testprov/gpt-test", Priority: 2, Weight: 100},
	}); err != nil {
		t.Fatalf("create combo: %v", err)
	}
	return h
}

// captureComboCooldowns replaces the package-level transient sleep function and
// records every cooldown that is requested. The original function is restored on
// cleanup.
func captureComboCooldowns(t *testing.T) (*[]time.Duration, func()) {
	t.Helper()
	var mu sync.Mutex
	var sleeps []time.Duration
	orig := transientErrorSleep
	transientErrorSleep = func(ctx context.Context, d time.Duration) {
		mu.Lock()
		sleeps = append(sleeps, d)
		mu.Unlock()
	}
	return &sleeps, func() {
		transientErrorSleep = orig
	}
}

func comboChatBody() []byte {
	return []byte(`{"model":"test-combo","messages":[{"role":"user","content":"hello"}]}`)
}

func comboSuccessResponse() *executor.Response {
	return &executor.Response{
		StatusCode: http.StatusOK,
		Body: []byte(`{
			"id":"chatcmpl-1","object":"chat.completion","created":1,"model":"gpt-test",
			"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}
		}`),
	}
}

// runComboChatRequest sends a non-streaming chat completion request to h and
// returns the HTTP recorder.
func runComboChatRequest(t *testing.T, h *Handler) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(comboChatBody()))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("api_key_id", "key-test")
	h.ChatCompletions(c)
	return rec
}

func TestHandleComboRequest_TransientError_CooldownBeforeFailover(t *testing.T) {
	h := setupComboTestHandler(t)

	sleeps, restore := captureComboCooldowns(t)
	defer restore()

	fe := &fakeExecutor{
		responses: []struct {
			resp *executor.Response
			err  error
		}{
			{nil, &executor.UpstreamError{StatusCode: http.StatusServiceUnavailable, Body: []byte("service unavailable")}},
			{comboSuccessResponse(), nil},
		},
	}
	executor.GetRegistry().Register("testprov", executor.FormatOpenAI, fe)
	defer executor.GetRegistry().Unregister("testprov")

	rec := runComboChatRequest(t, h)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if fe.callCount != 2 {
		t.Errorf("expected 2 executor calls (fail then success), got %d", fe.callCount)
	}
	if len(*sleeps) != 1 {
		t.Fatalf("expected exactly one transient cooldown, got %d", len(*sleeps))
	}
	if got := (*sleeps)[0]; got != defaultTransientCooldown {
		t.Errorf("cooldown = %v, want %v", got, defaultTransientCooldown)
	}
}

func TestHandleComboRequest_TransientError_CooldownForEachTransient(t *testing.T) {
	h := setupComboTestHandler(t)
	// Add a third connection so the combo can fail twice and still succeed.
	seedProviderAndConnection(t, h, "testprov", `["llm"]`, "conn-3", "http://unused")
	if _, err := h.combo.CreateCombo("test-combo-3", "priority", 30000, 1, false, "", "", []combo.CreateStepInput{
		{ConnectionID: "conn-1", ModelID: "testprov/gpt-test", Priority: 1, Weight: 100},
		{ConnectionID: "conn-2", ModelID: "testprov/gpt-test", Priority: 2, Weight: 100},
		{ConnectionID: "conn-3", ModelID: "testprov/gpt-test", Priority: 3, Weight: 100},
	}); err != nil {
		t.Fatalf("create combo: %v", err)
	}

	sleeps, restore := captureComboCooldowns(t)
	defer restore()

	fe := &fakeExecutor{
		responses: []struct {
			resp *executor.Response
			err  error
		}{
			{nil, &executor.UpstreamError{StatusCode: http.StatusBadGateway, Body: []byte("bad gateway")}},
			{nil, &executor.UpstreamError{StatusCode: http.StatusGatewayTimeout, Body: []byte("gateway timeout")}},
			{comboSuccessResponse(), nil},
		},
	}
	executor.GetRegistry().Register("testprov", executor.FormatOpenAI, fe)
	defer executor.GetRegistry().Unregister("testprov")

	body := []byte(`{"model":"test-combo-3","messages":[{"role":"user","content":"hello"}]}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("api_key_id", "key-test")
	h.ChatCompletions(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if fe.callCount != 3 {
		t.Errorf("expected 3 executor calls, got %d", fe.callCount)
	}
	if len(*sleeps) != 2 {
		t.Fatalf("expected 2 transient cooldowns, got %d", len(*sleeps))
	}
}

func TestHandleComboRequest_NonTransientClientError_NoCooldown(t *testing.T) {
	h := setupComboTestHandler(t)

	sleeps, restore := captureComboCooldowns(t)
	defer restore()

	fe := &fakeExecutor{
		responses: []struct {
			resp *executor.Response
			err  error
		}{
			{nil, &executor.UpstreamError{StatusCode: http.StatusUnauthorized, Body: []byte("invalid key")}},
			{comboSuccessResponse(), nil},
		},
	}
	executor.GetRegistry().Register("testprov", executor.FormatOpenAI, fe)
	defer executor.GetRegistry().Unregister("testprov")

	rec := runComboChatRequest(t, h)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if fe.callCount != 2 {
		t.Errorf("expected 2 executor calls, got %d", fe.callCount)
	}
	if len(*sleeps) != 0 {
		t.Errorf("expected no cooldown for 401, got %d sleeps", len(*sleeps))
	}
}

func TestHandleComboRequest_500ServerError_NoTransientCooldown(t *testing.T) {
	h := setupComboTestHandler(t)

	sleeps, restore := captureComboCooldowns(t)
	defer restore()

	fe := &fakeExecutor{
		responses: []struct {
			resp *executor.Response
			err  error
		}{
			{nil, &executor.UpstreamError{StatusCode: http.StatusInternalServerError, Body: []byte("internal error")}},
			{comboSuccessResponse(), nil},
		},
	}
	executor.GetRegistry().Register("testprov", executor.FormatOpenAI, fe)
	defer executor.GetRegistry().Unregister("testprov")

	rec := runComboChatRequest(t, h)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if fe.callCount != 2 {
		t.Errorf("expected 2 executor calls, got %d", fe.callCount)
	}
	if len(*sleeps) != 0 {
		t.Errorf("expected no transient cooldown for generic 500, got %d sleeps", len(*sleeps))
	}
}

func TestUpstreamHTTPStatus(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, 0},
		{"plain error", errors.New("boom"), 0},
		{"502", &executor.UpstreamError{StatusCode: http.StatusBadGateway}, http.StatusBadGateway},
		{"503", &executor.UpstreamError{StatusCode: http.StatusServiceUnavailable}, http.StatusServiceUnavailable},
		{"504", &executor.UpstreamError{StatusCode: http.StatusGatewayTimeout}, http.StatusGatewayTimeout},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := upstreamHTTPStatus(tt.err); got != tt.want {
				t.Errorf("upstreamHTTPStatus() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestIsTransientUpstreamError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"plain error", errors.New("boom"), false},
		{"400", &executor.UpstreamError{StatusCode: http.StatusBadRequest}, false},
		{"401", &executor.UpstreamError{StatusCode: http.StatusUnauthorized}, false},
		{"429", &executor.UpstreamError{StatusCode: http.StatusTooManyRequests}, false},
		{"500", &executor.UpstreamError{StatusCode: http.StatusInternalServerError}, false},
		{"502", &executor.UpstreamError{StatusCode: http.StatusBadGateway}, true},
		{"503", &executor.UpstreamError{StatusCode: http.StatusServiceUnavailable}, true},
		{"504", &executor.UpstreamError{StatusCode: http.StatusGatewayTimeout}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTransientUpstreamError(tt.err); got != tt.want {
				t.Errorf("isTransientUpstreamError() = %v, want %v", got, tt.want)
			}
		})
	}
}
