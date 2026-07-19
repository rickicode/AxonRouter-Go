package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rickicode/AxonRouter-Go/internal/cache"
	"github.com/tidwall/gjson"
)

func TestGrokCLI_CacheReasoningReplayHelpers(t *testing.T) {
	ctx := context.Background()
	model := "grok-4.5"
	key := "conv-1:deadbeef"

	// Store and retrieve.
	items := []map[string]any{
		{"type": "reasoning", "encrypted_content": "enc-1"},
		{"type": "message", "role": "assistant", "content": "hi"},
	}
	if err := cache.CacheGrokCLIReasoningReplayItems(ctx, model, key, items); err != nil {
		t.Fatalf("cache store error: %v", err)
	}
	got, err := cache.GetGrokCLIReasoningReplayItems(ctx, model, key)
	if err != nil {
		t.Fatalf("cache get error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 cached items, got %d", len(got))
	}

	// No anchor -> purge.
	noAnchor := []map[string]any{
		{"type": "message", "role": "assistant", "content": "bye"},
	}
	if err := cache.CacheGrokCLIReasoningReplayItems(ctx, model, key, noAnchor); err != nil {
		t.Fatalf("cache store error: %v", err)
	}
	body := []byte(`{"output":[{"type":"message","role":"assistant","content":"bye"}]}`)
	grokcliCacheReasoningReplay(ctx, model, key, body)
	got2, _ := cache.GetGrokCLIReasoningReplayItems(ctx, model, key)
	if len(got2) != 0 {
		t.Fatalf("expected cache purge without reasoning anchor, got %d items", len(got2))
	}
}

func TestGrokCLI_InjectReasoningReplay(t *testing.T) {
	ctx := context.Background()
	model := "grok-4.5"
	key := "conv-2:cafebabe"
	items := []map[string]any{
		{"type": "reasoning", "encrypted_content": "enc-2"},
		{"type": "function_call", "call_id": "call-1", "name": "tool_a", "arguments": "{}"},
	}
	if err := cache.CacheGrokCLIReasoningReplayItems(ctx, model, key, items); err != nil {
		t.Fatalf("cache store error: %v", err)
	}

	body := map[string]any{
		"input": []any{
			map[string]any{"type": "message", "role": "user", "content": "last q"},
		},
	}
	grokcliInjectReasoningReplay(ctx, body, model, key)
	input := body["input"].([]any)
	if len(input) != 3 {
		t.Fatalf("expected 3 input items, got %d: %v", len(input), input)
	}
	if input[0].(map[string]any)["type"] != "reasoning" {
		t.Errorf("expected cached reasoning first, got %v", input[0])
	}
	if input[2].(map[string]any)["role"] != "user" {
		t.Errorf("expected last user message to stay last, got %v", input[2])
	}

	// Duplicate encrypted_content should suppress the cached reasoning item.
	// The cached function_call is not a duplicate, so it should still be injected.
	body2 := map[string]any{
		"input": []any{
			map[string]any{"type": "reasoning", "encrypted_content": "enc-2"},
			map[string]any{"type": "message", "role": "user", "content": "q"},
		},
	}
	grokcliInjectReasoningReplay(ctx, body2, model, key)
	input2 := body2["input"].([]any)
	if len(input2) != 3 {
		t.Fatalf("expected existing reasoning + injected function_call + user message, got %d: %v", len(input2), input2)
	}
	if input2[0].(map[string]any)["type"] != "reasoning" {
		t.Errorf("expected existing reasoning to remain first, got %v", input2[0])
	}
	if input2[1].(map[string]any)["type"] != "function_call" {
		t.Errorf("expected cached function_call injected, got %v", input2[1])
	}
	if input2[2].(map[string]any)["role"] != "user" {
		t.Errorf("expected user message to stay last, got %v", input2[2])
	}

	// Duplicate call_id should suppress the cached function_call, but the
	// existing function_call in the input remains.
	body3 := map[string]any{
		"input": []any{
			map[string]any{"type": "function_call", "call_id": "call-1", "name": "tool_a", "arguments": "{}"},
			map[string]any{"type": "message", "role": "user", "content": "q2"},
		},
	}
	grokcliInjectReasoningReplay(ctx, body3, model, key)
	input3 := body3["input"].([]any)
	if len(input3) != 3 {
		t.Fatalf("expected existing func_call + reasoning + user message, got %d", len(input3))
	}
	funcCallCount := 0
	for _, it := range input3 {
		if m, ok := it.(map[string]any); ok && m["type"] == "function_call" {
			funcCallCount++
		}
	}
	if funcCallCount != 1 {
		t.Errorf("expected exactly one function_call after dedupe, got %d", funcCallCount)
	}
}

func TestGrokCLIExecutor_ReasoningReplayEndToEnd(t *testing.T) {
	completed, _ := json.Marshal(map[string]any{
		"type": "response.completed",
		"response": map[string]any{
			"id":     "r1",
			"status": "completed",
			"output": []any{
				map[string]any{"type": "reasoning", "encrypted_content": "enc-replay"},
				map[string]any{"type": "message", "role": "assistant", "content": "assistant says"},
			},
			"usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2},
		},
	})

	var secondBody []byte
	first := true
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !first {
			secondBody = body
		}
		first = false
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "data: "+string(completed))
		fmt.Fprintln(w)
	}))
	defer ts.Close()

	base := NewBaseExecutor()
	exec := NewGrokCLIExecutor(base)
	psd := map[string]string{}
	var persisted map[string]string
	persist := func(m map[string]string) error {
		persisted = make(map[string]string, len(m))
		for k, v := range m {
			persisted[k] = v
		}
		return nil
	}

	body, _ := json.Marshal(map[string]any{
		"input": []any{map[string]any{"type": "message", "role": "user", "content": "hi"}},
	})
	req := &Request{
		Provider:    "grok-cli",
		Model:       "grok-cli/grok-4.5",
		BaseURL:     ts.URL,
		AccessToken: "tok",
		ConnectionID: "conn-reasoning-e2e",
		ProviderSpecificData:        psd,
		Body:                        body,
		PersistProviderSpecificData: persist,
		StreamConfig: &StreamConfig{FetchTimeoutMs: 5000, StreamIdleTimeoutMs: 5000, StreamReadinessTimeoutMs: 5000},
	}
	if _, err := exec.Execute(context.Background(), req); err != nil {
		t.Fatalf("first Execute error: %v", err)
	}
	if persisted[grokCLIConvIDKey] == "" {
		t.Fatal("expected conv id persisted")
	}

	body2, _ := json.Marshal(map[string]any{
		"input": []any{map[string]any{"type": "message", "role": "user", "content": "follow up"}},
	})
	req2 := &Request{
		Provider:                    "grok-cli",
		Model:                       "grok-cli/grok-4.5",
		BaseURL:                     ts.URL,
		AccessToken:                 "tok",
		ConnectionID:                "conn-reasoning-e2e",
		ProviderSpecificData:        persisted,
		Body:                        body2,
		PersistProviderSpecificData: persist,
		StreamConfig: &StreamConfig{FetchTimeoutMs: 5000, StreamIdleTimeoutMs: 5000, StreamReadinessTimeoutMs: 5000},
	}
	if _, err := exec.Execute(context.Background(), req2); err != nil {
		t.Fatalf("second Execute error: %v", err)
	}
	if secondBody == nil {
		t.Fatal("second request body not captured")
	}
	items := gjson.GetBytes(secondBody, "input").Array()
	if len(items) != 3 {
		t.Fatalf("expected injected reasoning + user message (3 items), got %d: %s", len(items), string(secondBody))
	}
	if items[0].Get("type").String() != "reasoning" || items[0].Get("encrypted_content").String() != "enc-replay" {
		t.Errorf("expected reasoning replay item first, got %s", items[0].Raw)
	}
	if items[2].Get("role").String() != "user" || items[2].Get("content").String() != "follow up" {
		t.Errorf("expected last user message preserved, got %s", items[2].Raw)
	}
}
