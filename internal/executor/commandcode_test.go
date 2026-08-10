package executor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rickicode/AxonRouter-Go/internal/providercfg"
)

func TestCommandCodeBaseURL_Default(t *testing.T) {
	if got := commandcodeBaseURL(""); got != commandcodeDefaultBaseURL {
		t.Errorf("commandcodeBaseURL(\"\") = %q, want %q", got, commandcodeDefaultBaseURL)
	}
}

func TestCommandCodeBaseURL_TrimsTrailingSlash(t *testing.T) {
	if got := commandcodeBaseURL("https://api.commandcode.ai/"); got != "https://api.commandcode.ai" {
		t.Errorf("got %q, want no trailing slash", got)
	}
}

func TestCommandCodeNormalize_MergesSystemMessages(t *testing.T) {
	body := []byte(`{
		"model": "commandcode/deepseek/deepseek-v4-pro",
		"messages": [
			{"role": "system", "content": "You are helpful."},
			{"role": "developer", "content": "This is a dev note."},
			{"role": "user", "content": "hello"}
		]
	}`)
	got := normalizeCommandCodeBody(body)
	var req map[string]any
	if err := json.Unmarshal(got, &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req["system"] != "You are helpful.\n\nThis is a dev note." {
		t.Errorf("system = %q, want merged system", req["system"])
	}
	msgs, ok := req["messages"].([]any)
	if !ok || len(msgs) != 1 {
		t.Fatalf("expected 1 non-system message, got %d", len(msgs))
	}
}

func TestCommandCodeNormalize_ClampsMaxTokens(t *testing.T) {
	body := []byte(`{"model":"commandcode/gpt-5.5","max_tokens":500000}`)
	got := normalizeCommandCodeBody(body)
	var req map[string]any
	if err := json.Unmarshal(got, &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req["max_tokens"] != float64(commandcodeMaxTokens) {
		t.Errorf("max_tokens = %v, want %d", req["max_tokens"], commandcodeMaxTokens)
	}
}

func TestCommandCodeNormalize_DropsNonPositiveMaxTokens(t *testing.T) {
	body := []byte(`{"model":"commandcode/gpt-5.5","max_tokens":-1}`)
	got := normalizeCommandCodeBody(body)
	var req map[string]any
	if err := json.Unmarshal(got, &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := req["max_tokens"]; ok {
		t.Errorf("max_tokens should be omitted for negative value")
	}
}

func TestCommandCodeNormalize_StripsEmptyTools(t *testing.T) {
	body := []byte(`{"model":"commandcode/gpt-5.5","tools":[]}`)
	got := normalizeCommandCodeBody(body)
	var req map[string]any
	if err := json.Unmarshal(got, &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := req["tools"]; ok {
		t.Errorf("empty tools should be omitted")
	}
}

func TestCommandCodeExecutor_Execute(t *testing.T) {
	var calledPath string
	var called bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		calledPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"cc-1","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"hi"}}]}`))
	}))
	defer ts.Close()

	base := NewBaseExecutor()
	exec := NewCommandCodeExecutor(base)
	req := &Request{
		Provider: "commandcode",
		Model:    "deepseek/deepseek-v4-pro",
		BaseURL:  ts.URL,
		APIKey:   "sk-cc-test",
		Body:     []byte(`{"model":"commandcode/deepseek/deepseek-v4-pro","messages":[{"role":"user","content":"hello"}]}`),
	}
	resp, err := exec.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if !called {
		t.Fatal("upstream was not called")
	}
	if calledPath != "/alpha/generate" {
		t.Errorf("upstream path = %q, want /alpha/generate", calledPath)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	var payload map[string]any
	if err := json.Unmarshal(resp.Body, &payload); err != nil {
		t.Fatalf("response unmarshal: %v", err)
	}
	if payload["id"] != "cc-1" {
		t.Errorf("response id = %v, want cc-1", payload["id"])
	}
}

func TestCommandCodeExecutor_ExecuteStream(t *testing.T) {
	var calledPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calledPath = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data: {\"id\":\"cc-s\",\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"hi\"}}]}\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer ts.Close()

	base := NewBaseExecutor()
	exec := NewCommandCodeExecutor(base)
	req := &Request{
		Provider: "commandcode",
		Model:    "moonshotai/Kimi-K2.6",
		BaseURL:  ts.URL,
		APIKey:   "sk-cc-test",
		Body:     []byte(`{"model":"commandcode/moonshotai/Kimi-K2.6","messages":[{"role":"user","content":"hello"}],"stream":true}`),
	}
	result, err := exec.ExecuteStream(context.Background(), req)
	if err != nil {
		t.Fatalf("execute stream failed: %v", err)
	}
	if calledPath != "/alpha/generate" {
		t.Errorf("upstream path = %q, want /alpha/generate", calledPath)
	}
	if result.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", result.StatusCode)
	}
}

func TestCommandCodeExecutor_Models(t *testing.T) {
	var calledPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calledPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"object":"list","data":[{"id":"deepseek/deepseek-v4-pro"}]}`))
	}))
	defer ts.Close()

	base := NewBaseExecutor()
	exec := NewCommandCodeExecutor(base)
	req := &Request{
		Provider: "commandcode",
		BaseURL:  ts.URL,
		APIKey:   "sk-cc-test",
	}
	resp, err := exec.Models(context.Background(), req)
	if err != nil {
		t.Fatalf("models failed: %v", err)
	}
	if calledPath != "/provider/v1/models" {
		t.Errorf("models path = %q, want /provider/v1/models", calledPath)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestCommandCodeExecutor_PropagatesAuthHeader(t *testing.T) {
	var auth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"choices":[]}`))
	}))
	defer ts.Close()

	// Reset settings manager so compatibility seed defaults apply.
	_ = providercfg.NewManager("")

	base := NewBaseExecutor()
	exec := NewCommandCodeExecutor(base)
	req := &Request{
		Provider: "commandcode",
		Model:    "gpt-5.5",
		BaseURL:  ts.URL,
		APIKey:   "sk-cc-secret",
		Body:     []byte(`{"model":"commandcode/gpt-5.5","messages":[{"role":"user","content":"x"}]}`),
	}
	_, _ = exec.Execute(context.Background(), req)
	if auth != "Bearer sk-cc-secret" {
		t.Errorf("Authorization = %q, want Bearer sk-cc-secret", auth)
	}
}
