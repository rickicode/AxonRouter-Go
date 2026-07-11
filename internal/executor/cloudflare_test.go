package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/logging"
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

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
