package v1

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/tidwall/gjson"
)

// fakeCompletionsExecutor returns a chat-completions-style response so the
// completions handler can exercise its conversion path end-to-end.
type fakeCompletionsExecutor struct {
	*executor.BaseExecutor
	called bool
}

func (f *fakeCompletionsExecutor) Execute(ctx context.Context, req *executor.Request) (*executor.Response, error) {
	f.called = true

	model := gjson.GetBytes(req.Body, "model").String()
	if model == "" {
		model = "gpt-3.5-turbo-instruct"
	}
	body := []byte(`{"id":"cmpl-test","object":"chat.completion","created":1700000000,"model":"` + model + `","choices":[{"index":0,"message":{"role":"assistant","content":"hello world"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":2,"total_tokens":4}}`)
	return &executor.Response{StatusCode: http.StatusOK, Body: body}, nil
}

func (f *fakeCompletionsExecutor) ExecuteStream(ctx context.Context, req *executor.Request) (*executor.StreamResult, error) {
	return nil, nil
}

func TestCompletions_NonStreamingConvertsResponse(t *testing.T) {
	logging.Init("text")
	h := newTestHandler(t)

	fe := &fakeCompletionsExecutor{BaseExecutor: executor.NewBaseExecutor()}
	executor.GetRegistry().Register("openai", executor.FormatOpenAI, fe)
	defer executor.GetRegistry().Unregister("openai")

	seedProviderAndConnection(t, h, "openai", `["llm"]`, "openai-comp-conn", "http://unused")

	body := []byte(`{"model":"openai/gpt-3.5-turbo-instruct","prompt":"say hello","max_tokens":10}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/completions", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.Completions(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !fe.called {
		t.Fatal("expected executor to be called")
	}
	if !strings.Contains(rec.Body.String(), `"object":"text_completion"`) {
		t.Fatalf("expected completions response, got %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"text":"hello world"`) {
		t.Fatalf("expected text choice, got %s", rec.Body.String())
	}
}

func TestCompletions_ConvertsPromptToChatBody(t *testing.T) {
	logging.Init("text")
	h := newTestHandler(t)

	executor.GetRegistry().Register("openai", executor.FormatOpenAI, &fakeCompletionsExecutor{
		BaseExecutor: executor.NewBaseExecutor(),
	})
	defer executor.GetRegistry().Unregister("openai")

	seedProviderAndConnection(t, h, "openai", `["llm"]`, "openai-comp-cap", "http://unused")

	body := []byte(`{"model":"openai/gpt-3.5-turbo-instruct","prompt":"translate"}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/completions", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.Completions(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"object":"text_completion"`) {
		t.Fatalf("expected completions response, got %s", rec.Body.String())
	}
}

func TestCompletions_MissingBody(t *testing.T) {
	logging.Init("text")
	h := newTestHandler(t)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/completions", strings.NewReader(""))
	c.Request.Header.Set("Content-Type", "application/json")

	h.Completions(c)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestConvertCompletionsRequestToChatCompletions(t *testing.T) {
	in := []byte(`{"model":"openai/gpt-3.5-turbo-instruct","prompt":"hello","max_tokens":10,"temperature":0.5,"stream":true}`)
	out := convertCompletionsRequestToChatCompletions(in)

	if gjson.GetBytes(out, "model").String() != "openai/gpt-3.5-turbo-instruct" {
		t.Errorf("unexpected model: %s", gjson.GetBytes(out, "model").String())
	}
	if gjson.GetBytes(out, "messages.0.role").String() != "user" {
		t.Errorf("expected user role")
	}
	if gjson.GetBytes(out, "messages.0.content").String() != "hello" {
		t.Errorf("unexpected content: %s", gjson.GetBytes(out, "messages.0.content").String())
	}
	if gjson.GetBytes(out, "max_tokens").Int() != 10 {
		t.Errorf("unexpected max_tokens")
	}
	if !gjson.GetBytes(out, "stream").Bool() {
		t.Errorf("expected stream true")
	}
}

func TestConvertChatCompletionsResponseToCompletions(t *testing.T) {
	chat := []byte(`{"id":"chatcmpl-1","object":"chat.completion","created":1700000000,"model":"gpt-3.5-turbo-instruct","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	out := convertChatCompletionsResponseToCompletions(chat)

	if gjson.GetBytes(out, "object").String() != "text_completion" {
		t.Errorf("expected text_completion object")
	}
	if gjson.GetBytes(out, "choices.0.text").String() != "hi" {
		t.Errorf("expected text choice")
	}
}

func TestConvertChatCompletionsStreamChunkToCompletions(t *testing.T) {
	chat := []byte(`{"id":"chatcmpl-s","object":"chat.completion.chunk","created":1700000000,"model":"gpt-3.5-turbo-instruct","choices":[{"index":0,"delta":{"role":"assistant","content":"hi"},"finish_reason":null}]}`)
	out := convertChatCompletionsStreamChunkToCompletions(chat)

	if out == nil {
		t.Fatal("expected non-nil chunk")
	}
	if gjson.GetBytes(out, "object").String() != "text_completion" {
		t.Errorf("expected text_completion object")
	}
	if gjson.GetBytes(out, "choices.0.text").String() != "hi" {
		t.Errorf("expected text delta")
	}
}
