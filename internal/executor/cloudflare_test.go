package executor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/tidwall/gjson"
)

func init() {
	logging.Init("text")
	// Tests use httptest which binds to localhost; override validation.
	validateURL = func(string) error { return nil }
}

func TestCloudflareExecutor_SanitizesAndRoutesCorrectly(t *testing.T) {
	var calledPath string
	var receivedBody []byte

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calledPath = r.URL.Path
		receivedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "data: {}")
	}))
	defer ts.Close()

	base := NewBaseExecutor()
	cf := NewCloudflareExecutor(NewOpenAIExecutor(base))

	req := &Request{
		Model:   "@cf/meta/llama-3.2-1b-instruct",
		BaseURL: ts.URL + "/v1/chat/completions",
		Body: mustJSON(map[string]any{
			"model": "@cf/meta/llama-3.2-1b-instruct",
			"messages": []any{
				map[string]any{
					"role": "user",
					"content": []any{
						map[string]any{"type": "text", "text": "hello"},
						map[string]any{"type": "thinking", "thinking": "..."},
					},
				},
			},
			"max_tokens": -1,
		}),
		Stream: true,
		StreamConfig: &StreamConfig{
			FetchTimeoutMs:       5000,
			StreamIdleTimeoutMs:  5000,
			StreamReadinessTimeoutMs: 5000,
		},
	}

	res, err := cf.ExecuteStream(context.Background(), req)
	if err != nil {
		t.Fatalf("ExecuteStream error: %v", err)
	}

	// Drain chunks
	var chunks int
	for chunk := range res.Chunks {
		if chunk.Err != nil {
			t.Fatalf("unexpected chunk error: %v", chunk.Err)
		}
		chunks++
	}
	if chunks == 0 {
		t.Fatal("expected at least one chunk")
	}

	if calledPath != "/v1/chat/completions" {
		t.Fatalf("expected path /v1/chat/completions, got %s", calledPath)
	}

	var got map[string]any
	if err := json.Unmarshal(receivedBody, &got); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}

	// max_tokens should be capped to positive value
	if got["max_tokens"] == nil || got["max_tokens"] == -1 {
		t.Fatalf("max_tokens should be capped, got %v", got["max_tokens"])
	}

	// thinking block should be stripped; single remaining text block is collapsed to string
	msgs, _ := got["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	msg := msgs[0].(map[string]any)
	contentStr, ok := msg["content"].(string)
	if !ok || contentStr != "hello" {
		t.Fatalf("expected content string 'hello', got %v", msg["content"])
	}
}

func TestCloudflareExecutor_RoutesEmbeddingsAwayFromChatURL(t *testing.T) {
	var calledPath string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calledPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"data":[]}`)
	}))
	defer ts.Close()

	base := NewBaseExecutor()
	cf := NewCloudflareExecutor(NewOpenAIExecutor(base))

	req := &Request{
		Model:   "@cf/meta/llama-3.2-1b-instruct",
		BaseURL: ts.URL + "/v1/chat/completions",
		Body:    mustJSON(map[string]any{"model": "@cf/meta/llama-3.2-1b-instruct", "input": "hi"}),
	}

	_, err := cf.Embeddings(context.Background(), req)
	if err != nil {
		t.Fatalf("Embeddings error: %v", err)
	}

	if calledPath != "/v1/embeddings" {
		t.Fatalf("expected path /v1/embeddings, got %s", calledPath)
	}
}

func TestCloudflareExecutor_RoutesImagesAwayFromChatURL(t *testing.T) {
	var calledPath string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calledPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"created":1,"data":[]}`)
	}))
	defer ts.Close()

	base := NewBaseExecutor()
	cf := NewCloudflareExecutor(NewOpenAIExecutor(base))

	req := &Request{
		Model:   "@cf/black-forest-labs/flux-1-schnell",
		BaseURL: ts.URL + "/v1/chat/completions",
		Body:    mustJSON(map[string]any{"model": "@cf/black-forest-labs/flux-1-schnell", "prompt": "a cat"}),
	}

	_, err := cf.Images(context.Background(), req)
	if err != nil {
		t.Fatalf("Images error: %v", err)
	}

	if calledPath != "/v1/images/generations" {
		t.Fatalf("expected path /v1/images/generations, got %s", calledPath)
	}
}

func TestCloudflareExecutor_SanitizesReasoningEffort(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantKey  bool
		wantVal  string
	}{
		{"invalid minimal", "minimal", false, ""},
		{"invalid auto", "auto", false, ""},
		{"valid low", "low", true, "low"},
		{"valid max", "max", true, "max"},
		{"valid none", "none", true, "none"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var receivedBody []byte
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				receivedBody, _ = io.ReadAll(r.Body)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				fmt.Fprintln(w, `{"choices":[{"message":{"content":"ok"}}]}`)
			}))
			defer ts.Close()

			base := NewBaseExecutor()
			cf := NewCloudflareExecutor(NewOpenAIExecutor(base))
			req := &Request{
				Model:   "@cf/meta/llama-3.2-1b-instruct",
				BaseURL: ts.URL + "/v1/chat/completions",
				Body: mustJSON(map[string]any{
					"model":             "@cf/meta/llama-3.2-1b-instruct",
					"messages":          []any{map[string]any{"role": "user", "content": "hi"}},
					"reasoning_effort":  tt.input,
				}),
			}

			_, err := cf.Execute(context.Background(), req)
			if err != nil {
				t.Fatalf("Execute error: %v", err)
			}

			var got map[string]any
			if err := json.Unmarshal(receivedBody, &got); err != nil {
				t.Fatalf("unmarshal body: %v", err)
			}

			_, exists := got["reasoning_effort"]
			if exists != tt.wantKey {
				t.Fatalf("reasoning_effort existence=%v, want=%v; body=%s", exists, tt.wantKey, string(receivedBody))
			}
			if tt.wantKey {
				if got["reasoning_effort"] != tt.wantVal {
					t.Fatalf("reasoning_effort=%v, want=%v", got["reasoning_effort"], tt.wantVal)
				}
			}
		})
	}
}

func TestDoStreamRequest_IdleTimeout(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		fmt.Fprintln(w, "data: {}")
		flusher.Flush()
		// Then hang forever
		<-r.Context().Done()
	}))
	defer ts.Close()

	base := NewBaseExecutor()
	base.StreamIdleTimeout = 200 * time.Millisecond
	base.StreamReadinessTimeout = 500 * time.Millisecond

	res, err := base.DoStreamRequestWithConfig(context.Background(), "GET", ts.URL, nil, nil, &StreamConfig{
		StreamIdleTimeoutMs: 200,
	})
	if err != nil {
		t.Fatalf("DoStreamRequest error: %v", err)
	}

	var gotIdleErr bool
	for chunk := range res.Chunks {
		if chunk.Err != nil {
			if strings.Contains(chunk.Err.Error(), "stream idle timeout") {
				gotIdleErr = true
				break
			}
			t.Fatalf("unexpected chunk error: %v", chunk.Err)
		}
	}
	if !gotIdleErr {
		t.Fatal("expected stream idle timeout error")
	}
}

func TestCfInjectReasoningControl(t *testing.T) {
	tests := []struct {
		name         string
		body         []byte
		wantThinking any
	}{
		{
			name:         "default reasoning model defaults thinking to false",
			body:         []byte(`{"model":"@cf/moonshotai/kimi-k2.7","messages":[{"role":"user","content":"hi"}]}`),
			wantThinking: false,
		},
		{
			name:         "reasoning_effort high enables thinking",
			body:         []byte(`{"model":"@cf/moonshotai/kimi-k2.7","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"high"}`),
			wantThinking: true,
		},
		{
			name:         "reasoning_effort none disables thinking",
			body:         []byte(`{"model":"@cf/moonshotai/kimi-k2.7","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"none"}`),
			wantThinking: false,
		},
		{
			name:         "explicit chat_template_kwargs.thinking true is preserved",
			body:         []byte(`{"model":"@cf/moonshotai/kimi-k2.7","chat_template_kwargs":{"thinking":true,"depth":3}}`),
			wantThinking: true,
		},
		{
			name:         "non-reasoning CF model is unchanged",
			body:         []byte(`{"model":"@cf/meta/llama-3.2-1b-instruct","messages":[{"role":"user","content":"hi"}]}`),
			wantThinking: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cfInjectReasoningControl(tt.body)
			th := gjson.GetBytes(got, "chat_template_kwargs.thinking")
			switch want := tt.wantThinking.(type) {
			case nil:
				if th.Exists() {
					t.Fatalf("expected no chat_template_kwargs.thinking, got %v", th.Value())
				}
			case bool:
				if !th.Exists() || th.Bool() != want {
					t.Fatalf("chat_template_kwargs.thinking=%v, want %v", th.Value(), want)
				}
			}

			// Existing chat_template_kwargs keys should never be stripped.
			if depth := gjson.GetBytes(got, "chat_template_kwargs.depth"); depth.Exists() && depth.Int() != 3 {
				t.Fatalf("chat_template_kwargs.depth=%v, want 3", depth.Int())
			}
		})
	}
}

func TestCloudflareStreamNormalize_RewritesReasoningDelta(t *testing.T) {
	in := make(chan StreamChunk, 1)
	in <- StreamChunk{Payload: []byte(`data: {"id":"1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"reasoning":"think"},"finish_reason":null}]}`)}
	close(in)

	out := collectStreamChunks(normalizeCloudflareStream(in))
	if len(out) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(out))
	}
	if !strings.Contains(out[0], `"reasoning_content":"think"`) {
		t.Fatalf("expected reasoning_content, got %s", out[0])
	}
	if strings.Contains(out[0], `"reasoning":"think"`) {
		t.Fatalf("reasoning field should be removed, got %s", out[0])
	}
}

func TestCloudflareStreamNormalize_AggregatesReasoningBeforeContent(t *testing.T) {
	in := make(chan StreamChunk, 3)
	in <- StreamChunk{Payload: []byte(`data: {"id":"1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"reasoning":"step1"},"finish_reason":null}]}`)}
	in <- StreamChunk{Payload: []byte(`data: {"id":"1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"reasoning":"step2"},"finish_reason":null}]}`)}
	in <- StreamChunk{Payload: []byte(`data: {"id":"1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":null}]}`)}
	close(in)

	out := collectStreamChunks(normalizeCloudflareStream(in))
	if len(out) != 2 {
		t.Fatalf("expected 2 chunks, got %d: %v", len(out), out)
	}
	if !strings.Contains(out[0], `"reasoning_content":"step1step2"`) {
		t.Fatalf("expected aggregated reasoning_content, got %s", out[0])
	}
	if !strings.Contains(out[1], `"content":"hello"`) {
		t.Fatalf("expected content chunk, got %s", out[1])
	}
}

func TestCloudflareStreamNormalize_NoReasoningPassesUnchanged(t *testing.T) {
	payload := `data: {"id":"1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":null}]}`
	in := make(chan StreamChunk, 1)
	in <- StreamChunk{Payload: []byte(payload)}
	close(in)

	out := collectStreamChunks(normalizeCloudflareStream(in))
	if len(out) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(out))
	}
	if out[0] != payload {
		t.Fatalf("expected unchanged payload, got %s", out[0])
	}
}

func TestCloudflareStreamNormalize_EmptyReasoningPassesUnchanged(t *testing.T) {
	payload := `data: {"id":"1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"reasoning":"","content":"hello"},"finish_reason":null}]}`
	in := make(chan StreamChunk, 1)
	in <- StreamChunk{Payload: []byte(payload)}
	close(in)

	out := collectStreamChunks(normalizeCloudflareStream(in))
	if len(out) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(out))
	}
	if out[0] != payload {
		t.Fatalf("expected unchanged payload, got %s", out[0])
	}
}

func TestCloudflareStreamNormalize_PreservesDoneAndErrors(t *testing.T) {
	in := make(chan StreamChunk, 3)
	in <- StreamChunk{Payload: []byte("data: [DONE]")}
	in <- StreamChunk{Err: errors.New("boom")}
	in <- StreamChunk{Payload: []byte{}}
	close(in)

	out := collectStreamChunks(normalizeCloudflareStream(in))
	if len(out) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(out))
	}
	if out[0] != "data: [DONE]" {
		t.Fatalf("expected DONE unchanged, got %s", out[0])
	}
	if out[1] != "boom" {
		t.Fatalf("expected error chunk, got %s", out[1])
	}
	if len(out[2]) != 0 {
		t.Fatalf("expected empty payload, got %s", out[2])
	}
}

func collectStreamChunks(ch <-chan StreamChunk) []string {
	var out []string
	for c := range ch {
		if c.Err != nil {
			out = append(out, c.Err.Error())
			continue
		}
		out = append(out, string(c.Payload))
	}
	return out
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
