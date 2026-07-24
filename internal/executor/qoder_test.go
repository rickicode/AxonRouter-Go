package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/tidwall/gjson"
)

// fakeQoderCli creates a temporary directory containing a fake qodercli
// executable. Callers must clean it up.
func fakeQoderCli(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	name := "qodercli"
	if runtime.GOOS == "windows" {
		name += ".bat"
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake qodercli: %v", err)
	}
	return dir
}

func TestQoderExecutor_ExecuteStream_ViaCLI(t *testing.T) {
	dir := fakeQoderCli(t, `#!/usr/bin/env bash
set -e
for arg in "$@"; do
  case "$arg" in
    --model) model="$2"; shift 2 ;;
    --print) ;;
  esac
done
echo '{"result":"hello from qoder","is_error":false}'
`)
	t.Setenv("CLI_QODER_BIN", filepath.Join(dir, "qodercli"))

	base := NewBaseExecutor()
	openai := NewOpenAIExecutor(base)
	qoder := NewQoderExecutor(base, openai)

	body, _ := json.Marshal(map[string]any{
		"model":    "qoder/qoder-rome",
		"messages": []map[string]any{{"role": "user", "content": "hi"}},
		"stream":   true,
	})

	result, err := qoder.ExecuteStream(context.Background(), &Request{
		Model:       "qoder/qoder-rome",
		AccessToken: "pt-fake-token",
		Body:        body,
	})
	if err != nil {
		t.Fatalf("ExecuteStream error: %v", err)
	}
	if result.StatusCode != 200 {
		t.Fatalf("expected status 200, got %d", result.StatusCode)
	}

	var parts []string
	done := time.After(3 * time.Second)
collect:
	for {
		select {
		case chunk, ok := <-result.Chunks:
			if !ok {
				break collect
			}
			if len(chunk.Payload) == 0 {
				continue
			}
			if strings.TrimSpace(string(chunk.Payload)) == "data: [DONE]" {
				break collect
			}
			data := strings.TrimPrefix(strings.TrimSpace(string(chunk.Payload)), "data: ")
			content := gjson.Get(data, "choices.0.delta.content").String()
			if content != "" {
				parts = append(parts, content)
			}
		case <-done:
			t.Fatal("stream timed out")
		}
	}

	got := strings.Join(parts, "")
	want := "hello from qoder"
	if !strings.Contains(strings.TrimSpace(got), want) {
		t.Fatalf("expected stream content to contain %q, got %q", want, got)
	}
}

func TestQoderExecutor_Execute_ViaCLI(t *testing.T) {
	dir := fakeQoderCli(t, `#!/usr/bin/env bash
echo '{"result":"42","is_error":false}'
`)
	t.Setenv("CLI_QODER_BIN", filepath.Join(dir, "qodercli"))

	base := NewBaseExecutor()
	openai := NewOpenAIExecutor(base)
	qoder := NewQoderExecutor(base, openai)

	body, _ := json.Marshal(map[string]any{
		"model":    "qoder/deepseek-r1",
		"messages": []map[string]any{{"role": "user", "content": "answer"}},
	})

	resp, err := qoder.Execute(context.Background(), &Request{
		Model:   "qoder/deepseek-r1",
		APIKey:  "pt-secret",
		BaseURL: "https://example.com",
		Body:    body,
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var data map[string]any
	if err := json.Unmarshal(resp.Body, &data); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if data["object"] != "chat.completion" {
		t.Fatalf("expected object chat.completion, got %v", data["object"])
	}
	choices, _ := data["choices"].([]any)
	if len(choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(choices))
	}
	msg := choices[0].(map[string]any)["message"].(map[string]any)
	if msg["content"] != "42" {
		t.Fatalf("expected content 42, got %v", msg["content"])
	}
}

func TestQoderExecutor_Execute_CLIReportsError(t *testing.T) {
	dir := fakeQoderCli(t, `#!/usr/bin/env bash
echo '{"result":"bad credentials","is_error":true}'
`)
	t.Setenv("CLI_QODER_BIN", filepath.Join(dir, "qodercli"))

	base := NewBaseExecutor()
	openai := NewOpenAIExecutor(base)
	qoder := NewQoderExecutor(base, openai)

	body, _ := json.Marshal(map[string]any{
		"model":    "qoder/qwen3-coder",
		"messages": []map[string]any{{"role": "user", "content": "x"}},
	})

	_, err := qoder.Execute(context.Background(), &Request{
		Model:       "qoder/qwen3-coder",
		AccessToken: "pt-wrong",
		Body:        body,
	})
	if err == nil {
		t.Fatal("expected error for CLI error response, got nil")
	}
	if !strings.Contains(err.Error(), "bad credentials") {
		t.Fatalf("expected error to mention 'bad credentials', got %v", err)
	}
}

func TestQoderExecutor_Execute_ViaHTTP(t *testing.T) {
	var receivedModel string
	var authHeader string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		receivedModel = gjson.GetBytes(body, "model").String()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"id":"chatcmpl-test","object":"chat.completion","created":1,"model":"coder-model","choices":[{"index":0,"message":{"role":"assistant","content":"from http"},"finish_reason":"stop"}]}`)
	}))
	defer ts.Close()

	base := NewBaseExecutor()
	openai := NewOpenAIExecutor(base)
	qoder := NewQoderExecutor(base, openai)

	body, _ := json.Marshal(map[string]any{
		"model":    "qoder/qwen3.5-plus",
		"messages": []map[string]any{{"role": "user", "content": "hi"}},
	})

	resp, err := qoder.Execute(context.Background(), &Request{
		Model:       "qoder/qwen3.5-plus",
		APIKey:      "sk-not-pat",
		BaseURL:     ts.URL + "/v1/chat/completions",
		Body:        body,
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if receivedModel != "coder-model" {
		t.Fatalf("expected upstream model coder-model, got %q", receivedModel)
	}
	if !strings.Contains(authHeader, "sk-not-pat") {
		t.Fatalf("expected Authorization header to contain API key, got %q", authHeader)
	}
}

func TestQoderExecutor_ExecuteStream_ViaHTTP(t *testing.T) {
	var receivedModel string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		receivedModel = gjson.GetBytes(body, "model").String()
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"delta\":{\"content\":\"h\"}}]}")
		fmt.Fprintln(w, "data: [DONE]")
	}))
	defer ts.Close()

	base := NewBaseExecutor()
	openai := NewOpenAIExecutor(base)
	qoder := NewQoderExecutor(base, openai)

	body, _ := json.Marshal(map[string]any{
		"model":    "qoder/vision-model",
		"messages": []map[string]any{{"role": "user", "content": "look"}},
		"stream":   true,
	})

	_, err := qoder.ExecuteStream(context.Background(), &Request{
		Model:          "qoder/vision-model",
		AccessToken:    "dashscope-token",
		BaseURL:        ts.URL + "/v1/chat/completions",
		Body:           body,
	})
	if err != nil {
		t.Fatalf("ExecuteStream error: %v", err)
	}
	if receivedModel != "qwen3-vl-plus" {
		t.Fatalf("expected upstream model qwen3-vl-plus, got %q", receivedModel)
	}
}

func TestQoderMapModelToLevel(t *testing.T) {
	cases := []struct {
		model string
		want  string
	}{
		{"qoder/deepseek-r1", "ultimate"},
		{"qoder/glm-5.2", "gm51model"},
		{"qoder/minimax-text", "mmodel"},
		{"qoder/qwen3-max", "performance"},
		{"qoder/kimi-k2.7", "kmodel"},
		{"qoder/qwen3-coder", "qmodel"},
		{"qoder/unknown", "auto"},
		{"qoder/qoder-rome", "qmodel"},
	}
	for _, c := range cases {
		got := mapQoderModelToLevel(ExtractModel(c.model))
		if got != c.want {
			t.Errorf("mapQoderModelToLevel(%q) = %q, want %q", c.model, got, c.want)
		}
	}
}

func TestEffectiveQoderToken(t *testing.T) {
	t.Setenv("QODER_PERSONAL_ACCESS_TOKEN", "")
	if got := effectiveQoderToken(&Request{APIKey: "pt-a"}); got != "pt-a" {
		t.Errorf("expected APIKey priority, got %q", got)
	}
	if got := effectiveQoderToken(&Request{AccessToken: "pt-b"}); got != "pt-b" {
		t.Errorf("expected AccessToken priority, got %q", got)
	}
	t.Setenv("QODER_PERSONAL_ACCESS_TOKEN", "pt-env")
	if got := effectiveQoderToken(&Request{}); got != "pt-env" {
		t.Errorf("expected env fallback, got %q", got)
	}
}

func TestIsQoderPAT(t *testing.T) {
	if !isQoderPAT("pt-abc") {
		t.Error("expected pt-abc to be PAT")
	}
	if isQoderPAT("sk-abc") {
		t.Error("expected sk-abc not to be PAT")
	}
}
func TestEffectiveQoderToken_ProviderSpecificAPIKeyPriority(t *testing.T) {
	t.Setenv("QODER_PERSONAL_ACCESS_TOKEN", "")
	req := &Request{
		AccessToken:          "at-http",
		APIKey:               "ak-http",
		ProviderSpecificData: map[string]string{"api_key": "ak-oauth"},
	}
	if got := effectiveQoderToken(req); got != "ak-oauth" {
		t.Errorf("expected ProviderSpecific api_key priority, got %q", got)
	}
}

func TestEffectiveQoderToken_AccessTokenBeforeAPIKeyForNonPAT(t *testing.T) {
	t.Setenv("QODER_PERSONAL_ACCESS_TOKEN", "")
	req := &Request{
		AccessToken: "at-http",
		APIKey:      "ak-http",
	}
	if got := effectiveQoderToken(req); got != "at-http" {
		t.Errorf("expected AccessToken before APIKey, got %q", got)
	}
}

func TestEffectiveQoderToken_PATOverridesProviderSpecific(t *testing.T) {
	t.Setenv("QODER_PERSONAL_ACCESS_TOKEN", "")
	req := &Request{
		APIKey:               "pt-abc",
		AccessToken:          "at-http",
		ProviderSpecificData: map[string]string{"api_key": "ak-oauth"},
	}
	if got := effectiveQoderToken(req); got != "pt-abc" {
		t.Errorf("expected PAT to override ProviderSpecific, got %q", got)
	}
}

func TestEffectiveQoderToken_EnvFallback(t *testing.T) {
	t.Setenv("QODER_PERSONAL_ACCESS_TOKEN", "pt-env")
	req := &Request{}
	if got := effectiveQoderToken(req); got != "pt-env" {
		t.Errorf("expected env fallback, got %q", got)
	}
}
