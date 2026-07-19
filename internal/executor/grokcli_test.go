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

	"github.com/rickicode/AxonRouter-Go/internal/executor/translator/providers"
	"github.com/tidwall/gjson"
)

func init() {
	RegisterDefaults()
	validateURL = func(string) error { return nil }
}

func TestGrokCLIExecutor_Headers(t *testing.T) {
	var gotHeaders http.Header
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		_, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		completed, _ := json.Marshal(map[string]any{
			"type": "response.completed",
			"response": map[string]any{
				"id":     "r1",
				"status": "completed",
				"output": []any{},
				"usage": map[string]any{
					"input_tokens":  10,
					"output_tokens": 5,
					"total_tokens":  15,
				},
			},
		})
		fmt.Fprintln(w, "data: "+string(completed))
		fmt.Fprintln(w)
	}))
	defer ts.Close()

	base := NewBaseExecutor()
	exec := NewGrokCLIExecutor(base)
	body, _ := json.Marshal(map[string]any{
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	})
	var persisted map[string]string
	req := &Request{
		Provider:     "grok-cli",
		Model:        "grok-cli/grok-4.5",
		BaseURL:      ts.URL,
		AccessToken:  "grok-at-123",
		ConnectionID: "conn-grok-headers",
		ProviderSpecificData: map[string]string{
			"email": "user@example.com",
			"sub":   "grok-sub-abc",
		},
		Body: body,
		PersistProviderSpecificData: func(psd map[string]string) error {
			persisted = make(map[string]string, len(psd))
			for k, v := range psd {
				persisted[k] = v
			}
			return nil
		},
		StreamConfig: &StreamConfig{
			FetchTimeoutMs:           5000,
			StreamIdleTimeoutMs:      5000,
			StreamReadinessTimeoutMs: 5000,
		},
	}
	if _, err := exec.Execute(context.Background(), req); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	mustEqual := map[string]string{
		"Authorization": "Bearer grok-at-123",
		"X-Xai-Token-Auth": "xai-grok-cli",
		"x-grok-client-version": "0.2.99",
		"User-Agent": "grok-shell/0.2.99 (linux; x86_64)",
		"x-grok-client-identifier": "grok-shell",
		"x-grok-client-mode": "headless",
		"x-email": "user@example.com",
		"x-userid": "grok-sub-abc",
		"x-grok-model-override": "grok-4.5",
		"x-grok-turn-idx": "0",
		"Connection": "Keep-Alive",
	}
	for name, want := range mustEqual {
		if got := gotHeaders.Get(name); got != want {
			t.Errorf("%s=%q, want %q", name, got, want)
		}
	}
	
	needPresent := []string{"x-grok-session-id", "x-grok-conv-id", "x-grok-req-id", "x-grok-agent-id", "x-grok-client-identifier", "x-grok-client-mode"}
	for _, name := range needPresent {
		if got := gotHeaders.Get(name); got == "" {
			t.Errorf("%s is empty", name)
		}
	}
	firstSession := gotHeaders.Get("x-grok-session-id")
	firstConv := gotHeaders.Get("x-grok-conv-id")
	firstAgent := gotHeaders.Get("x-grok-agent-id")
	firstReq := gotHeaders.Get("x-grok-req-id")

	// Second request on the same connection should reuse stable IDs and bump turn.
	if _, err := exec.Execute(context.Background(), req); err != nil {
		t.Fatalf("second Execute error: %v", err)
	}
	if got := gotHeaders.Get("x-grok-session-id"); got != firstSession {
		t.Errorf("session id changed from %q to %q", firstSession, got)
	}
	if got := gotHeaders.Get("x-grok-conv-id"); got != firstConv {
		t.Errorf("conv id changed from %q to %q", firstConv, got)
	}
	if got := gotHeaders.Get("x-grok-agent-id"); got != firstAgent {
		t.Errorf("agent id changed from %q to %q", firstAgent, got)
	}
	if got := gotHeaders.Get("x-grok-req-id"); got == firstReq {
		t.Errorf("request id should differ between requests")
	}
	if got := gotHeaders.Get("x-grok-turn-idx"); got != "1" {
		t.Errorf("x-grok-turn-idx=%q, want 1", got)
	}
	if persisted == nil {
		t.Fatalf("expected PSD to be persisted")
	}
	if persisted[grokCLISessionIDKey] != firstSession {
		t.Errorf("persisted session id %q does not match header %q", persisted[grokCLISessionIDKey], firstSession)
	}
	if persisted[grokCLITurnIdxKey] != "2" {
		t.Errorf("persisted turn_idx=%q, want 2", persisted[grokCLITurnIdxKey])
	}
}

func TestGrokCLIExecutor_RequestTransform(t *testing.T) {
	var gotBody []byte
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		completed, _ := json.Marshal(map[string]any{
			"type":     "response.completed",
			"response": map[string]any{"id": "r1", "status": "completed", "output": []any{}, "usage": map[string]any{}},
		})
		fmt.Fprintln(w, "data: "+string(completed))
		fmt.Fprintln(w)
	}))
	defer ts.Close()

	base := NewBaseExecutor()
	exec := NewGrokCLIExecutor(base)
	body, _ := json.Marshal(map[string]any{
		"model":            "grok-cli/grok-4.5",
		"stream":           false,
		"store":            true,
		"messages":         []any{map[string]any{"role": "user", "content": "hello"}},
		"reasoning_effort": "high",
		"unknown_field":    "drop-me",
		"tools": []any{
			map[string]any{
				"namespace": "ns1",
				"tools": []any{
					map[string]any{"type": "function", "name": "tool_a", "parameters": map[string]any{"type": "object"}},
				},
			},
		},
	})
	req := &Request{
		Provider:    "grok-cli",
		Model:       "grok-cli/grok-4.5",
		BaseURL:     ts.URL,
		AccessToken: "grok-at-123",
		Body:        body,
		StreamConfig: &StreamConfig{
			FetchTimeoutMs:           5000,
			StreamIdleTimeoutMs:      5000,
			StreamReadinessTimeoutMs: 5000,
		},
	}
	if _, err := exec.Execute(context.Background(), req); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if got := gjson.GetBytes(gotBody, "stream").Bool(); !got {
		t.Errorf("expected stream=true, got %v", got)
	}
	if got := gjson.GetBytes(gotBody, "store").Bool(); got {
		t.Errorf("expected store=false, got %v", got)
	}
	if gjson.GetBytes(gotBody, "messages").Exists() {
		t.Errorf("messages should have been dropped")
	}
	if gjson.GetBytes(gotBody, "unknown_field").Exists() {
		t.Errorf("unknown_field should have been dropped")
	}
	if !gjson.GetBytes(gotBody, "reasoning").Exists() {
		t.Errorf("expected reasoning object")
	}
	if got := gjson.GetBytes(gotBody, "reasoning.summary").String(); got != "concise" {
		t.Errorf("reasoning.summary=%q, want concise", got)
	}
	include := gjson.GetBytes(gotBody, "include").Array()
	found := false
	for _, v := range include {
		if v.String() == "reasoning.encrypted_content" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected include reasoning.encrypted_content, got %s", gjson.GetBytes(gotBody, "include").Raw)
	}
	if got := gjson.GetBytes(gotBody, "model").String(); got != "grok-4.5" {
		t.Errorf("model=%q, want grok-4.5", got)
	}
	tools := gjson.GetBytes(gotBody, "tools").Array()
	if len(tools) != 1 || tools[0].Get("type").String() != "function" || tools[0].Get("name").String() != "tool_a" {
		t.Errorf("expected flattened tool, got %s", gjson.GetBytes(gotBody, "tools").Raw)
	}
}

func TestGrokCLIExecutor_ToolListTruncated(t *testing.T) {
	var gotBody []byte
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		completed, _ := json.Marshal(map[string]any{"type": "response.completed", "response": map[string]any{"id": "r1", "status": "completed", "output": []any{}, "usage": map[string]any{}}})
		fmt.Fprintln(w, "data: "+string(completed))
		fmt.Fprintln(w)
	}))
	defer ts.Close()

	base := NewBaseExecutor()
	exec := NewGrokCLIExecutor(base)

	rawTools := make([]any, 0, 250)
	for i := 0; i < 250; i++ {
		rawTools = append(rawTools, map[string]any{
			"namespace": fmt.Sprintf("ns_%d", i),
			"tools": []any{
				map[string]any{"type": "function", "name": fmt.Sprintf("tool_%d", i), "parameters": map[string]any{"type": "object"}},
			},
		})
	}
	body, _ := json.Marshal(map[string]any{
		"model":    "grok-cli/grok-4.5",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
		"tools":    rawTools,
	})
	req := &Request{
		Provider:    "grok-cli",
		Model:       "grok-cli/grok-4.5",
		BaseURL:     ts.URL,
		AccessToken: "grok-at-123",
		Body:        body,
		StreamConfig: &StreamConfig{
			FetchTimeoutMs:           5000,
			StreamIdleTimeoutMs:      5000,
			StreamReadinessTimeoutMs: 5000,
		},
	}
	if _, err := exec.Execute(context.Background(), req); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	tools := gjson.GetBytes(gotBody, "tools").Array()
	if len(tools) != grokCLIMaxTools {
		t.Fatalf("expected %d tools after truncation, got %d", grokCLIMaxTools, len(tools))
	}
}

func TestGrokCLIExecutor_ExtractUsage(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		created, _ := json.Marshal(map[string]any{"type": "response.created", "response": map[string]any{"id": "r1"}})
		delta, _ := json.Marshal(map[string]any{"type": "response.output_text.delta", "delta": "Hi"})
		completed, _ := json.Marshal(map[string]any{
			"type": "response.completed",
			"response": map[string]any{
				"id":     "r1",
				"status": "completed",
				"output": []any{},
				"usage": map[string]any{
					"input_tokens":          8,
					"output_tokens":         4,
					"total_tokens":          12,
					"output_tokens_details": map[string]any{"reasoning_tokens": 1},
				},
			},
		})
		fmt.Fprintln(w, "data: "+string(created))
		fmt.Fprintln(w, "data: "+string(delta))
		fmt.Fprintln(w, "data: "+string(completed))
		fmt.Fprintln(w)
	}))
	defer ts.Close()

	base := NewBaseExecutor()
	exec := NewGrokCLIExecutor(base)
	body, _ := json.Marshal(map[string]any{
		"input": []any{map[string]any{"type": "message", "role": "user", "content": "hi"}},
	})
	req := &Request{
		Provider:    "grok-cli",
		Model:       "grok-cli/grok-4.5",
		BaseURL:     ts.URL,
		AccessToken: "grok-at-123",
		Body:        body,
		StreamConfig: &StreamConfig{
			FetchTimeoutMs:           5000,
			StreamIdleTimeoutMs:      5000,
			StreamReadinessTimeoutMs: 5000,
		},
	}
	res, err := exec.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if res.StatusCode != 200 {
		t.Fatalf("status=%d, want 200", res.StatusCode)
	}
	if res.Usage["prompt_tokens"] != 8 {
		t.Fatalf("prompt_tokens=%d, want 8", res.Usage["prompt_tokens"])
	}
	if res.Usage["completion_tokens"] != 4 {
		t.Fatalf("completion_tokens=%d, want 4", res.Usage["completion_tokens"])
	}
	if res.Usage["total_tokens"] != 12 {
		t.Fatalf("total_tokens=%d, want 12", res.Usage["total_tokens"])
	}
	if res.Usage["reasoning_tokens"] != 1 {
		t.Fatalf("reasoning_tokens=%d, want 1", res.Usage["reasoning_tokens"])
	}
}

func TestGrokCLIExecutor_ExecuteStream(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		created, _ := json.Marshal(map[string]any{"type": "response.created"})
		completed, _ := json.Marshal(map[string]any{"type": "response.completed", "response": map[string]any{"id": "r1", "status": "completed"}})
		fmt.Fprintln(w, "data: "+string(created))
		flusher.Flush()
		fmt.Fprintln(w, "data: "+string(completed))
		flusher.Flush()
		fmt.Fprintln(w)
	}))
	defer ts.Close()

	base := NewBaseExecutor()
	exec := NewGrokCLIExecutor(base)
	body, _ := json.Marshal(map[string]any{"input": []any{}})
	req := &Request{
		Provider:    "grok-cli",
		Model:       "grok-cli/grok-4.5",
		BaseURL:     ts.URL,
		AccessToken: "grok-at-123",
		Body:        body,
		StreamConfig: &StreamConfig{
			FetchTimeoutMs:           5000,
			StreamIdleTimeoutMs:      5000,
			StreamReadinessTimeoutMs: 5000,
		},
	}
	res, err := exec.ExecuteStream(context.Background(), req)
	if err != nil {
		t.Fatalf("ExecuteStream error: %v", err)
	}
	var got []string
	for chunk := range res.Chunks {
		if chunk.Err != nil {
			t.Fatalf("chunk error: %v", chunk.Err)
		}
		if chunk.Payload != nil {
			got = append(got, string(chunk.Payload))
		}
	}
	found := false
	for _, s := range got {
		if strings.Contains(s, "response.completed") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected response.completed in stream, got %v", got)
	}
}

func TestGrokCLIExecutor_ErrorTranslation(t *testing.T) {
	errBody, _ := json.Marshal(map[string]any{"error": map[string]any{"message": "Spending limit reached"}})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = w.Write(errBody)
	}))
	defer ts.Close()

	base := NewBaseExecutor()
	exec := NewGrokCLIExecutor(base)
	body, _ := json.Marshal(map[string]any{"input": []any{}})
	req := &Request{
		Provider:    "grok-cli",
		Model:       "grok-cli/grok-4.5",
		BaseURL:     ts.URL,
		AccessToken: "grok-at-123",
		Body:        body,
		StreamConfig: &StreamConfig{
			FetchTimeoutMs:           5000,
			StreamIdleTimeoutMs:      5000,
			StreamReadinessTimeoutMs: 5000,
		},
	}
	_, err := exec.Execute(context.Background(), req)
	if err == nil {
		t.Fatal("expected error")
	}
	upErr, ok := err.(*UpstreamError)
	if !ok {
		t.Fatalf("expected UpstreamError, got %T", err)
	}
	if upErr.StatusCode != http.StatusPaymentRequired {
		t.Fatalf("status=%d, want 402", upErr.StatusCode)
	}
	bodyStr := string(upErr.Body)
	if !strings.Contains(bodyStr, "insufficient_quota") {
		t.Fatalf("expected insufficient_quota, got %s", bodyStr)
	}
}

func TestGrokCLI_GetTurnIdx(t *testing.T) {
	if got := grokcliGetTurnIdx(map[string]string{grokCLITurnIdxKey: "5"}); got != 5 {
		t.Errorf("persisted turn_idx=5: got %d, want 5", got)
	}
	if got := grokcliGetTurnIdx(map[string]string{}); got != 0 {
		t.Errorf("missing turn_idx: got %d, want 0", got)
	}
}

func TestGrokCLI_CountUserMessages(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"messages": []any{
			map[string]any{"role": "system", "content": "sys"},
			map[string]any{"role": "user", "content": "a"},
			map[string]any{"role": "assistant", "content": "b"},
			map[string]any{"role": "tool", "content": "c"},
			map[string]any{"role": "user", "content": "d"},
		},
	})
	if got := grokcliCountUserMessages(body); got != 2 {
		t.Errorf("messages body: got %d user msgs, want 2", got)
	}

	inputBody, _ := json.Marshal(map[string]any{
		"input": []any{
			map[string]any{"type": "message", "role": "user", "content": "a"},
			map[string]any{"type": "message", "role": "assistant", "content": "b"},
			map[string]any{"type": "message", "role": "user", "content": "c"},
		},
	})
	if got := grokcliCountUserMessages(inputBody); got != 2 {
		t.Errorf("input body: got %d user msgs, want 2", got)
	}

	if got := grokcliCountUserMessages([]byte(`{}`)); got != 0 {
		t.Errorf("empty body: got %d, want 0", got)
	}
}

func TestGrokCLI_StableAgentID(t *testing.T) {
	a := grokcliStableAgentID(map[string]string{"deviceId": "dev1"})
	b := grokcliStableAgentID(map[string]string{"deviceId": "dev1"})
	c := grokcliStableAgentID(map[string]string{"deviceId": "dev2"})

	if a == "" {
		t.Fatal("agent id is empty")
	}
	if a != b {
		t.Errorf("same device id should produce same agent id: %q vs %q", a, b)
	}
	if a == c {
		t.Errorf("different device ids should produce different agent ids")
	}

	// Falls back through sub then email.
	sub := grokcliStableAgentID(map[string]string{"sub": "sub-1"})
	email := grokcliStableAgentID(map[string]string{"email": "u@x.com"})
	if sub == "" || email == "" || sub == email {
		t.Errorf("fallback agent ids should be non-empty and distinct: sub=%q email=%q", sub, email)
	}

	// Persisted agent id takes precedence.
	persisted := grokcliStableAgentID(map[string]string{grokCLIAgentIDKey: "agent-42"})
	if persisted != "agent-42" {
		t.Errorf("persisted agent id should win: got %q", persisted)
	}
}

func TestGrokCLIExecutor_ForbiddenFieldsDropped(t *testing.T) {
	var gotBody []byte
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		completed, _ := json.Marshal(map[string]any{"type": "response.completed", "response": map[string]any{"id": "r1", "status": "completed", "output": []any{}, "usage": map[string]any{}}})
		fmt.Fprintln(w, "data: "+string(completed))
		fmt.Fprintln(w)
	}))
	defer ts.Close()

	base := NewBaseExecutor()
	exec := NewGrokCLIExecutor(base)
	body, _ := json.Marshal(map[string]any{
		"model":                "grok-cli/grok-4.5",
		"presence_penalty":     0.5,
		"frequency_penalty":    0.3,
		"seed":                 42,
		"user":                 "rick",
		"previous_response_id": "prev-123",
		"messages":             []any{map[string]any{"role": "user", "content": "hi"}},
	})
	req := &Request{Provider: "grok-cli", Model: "grok-cli/grok-4.5", BaseURL: ts.URL, AccessToken: "grok-at-123", Body: body, StreamConfig: &StreamConfig{FetchTimeoutMs: 5000, StreamIdleTimeoutMs: 5000, StreamReadinessTimeoutMs: 5000}}
	if _, err := exec.Execute(context.Background(), req); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	for _, field := range []string{"presence_penalty", "frequency_penalty", "seed", "user", "previous_response_id"} {
		if gjson.GetBytes(gotBody, field).Exists() {
			t.Errorf("field %s should have been removed", field)
		}
	}
}

func TestGrokCLIExecutor_CustomToolCallConversionAndItemRefStrip(t *testing.T) {
	var gotBody []byte
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		completed, _ := json.Marshal(map[string]any{"type": "response.completed", "response": map[string]any{"id": "r1", "status": "completed", "output": []any{}, "usage": map[string]any{}}})
		fmt.Fprintln(w, "data: "+string(completed))
		fmt.Fprintln(w)
	}))
	defer ts.Close()

	base := NewBaseExecutor()
	exec := NewGrokCLIExecutor(base)
	body, _ := json.Marshal(map[string]any{
		"model": "grok-cli/grok-4.5",
		"input": []any{
			map[string]any{"type": "message", "role": "user", "content": "ok"},
			map[string]any{"type": "custom_tool_call", "call_id": "call-1", "name": "tool_a", "arguments": "{}"},
			map[string]any{"type": "custom_tool_call_output", "call_id": "call-1", "output": "done"},
			map[string]any{"type": "item_reference", "id": "msg_ref_1"},
			map[string]any{"type": "message", "role": "assistant", "id": "msg_abc123", "content": "assistant reply"},
			map[string]any{"type": "internal_chat_message_metadata_passthrough", "meta": "x"},
		},
	})
	req := &Request{Provider: "grok-cli", Model: "grok-cli/grok-4.5", BaseURL: ts.URL, AccessToken: "grok-at-123", Body: body, StreamConfig: &StreamConfig{FetchTimeoutMs: 5000, StreamIdleTimeoutMs: 5000, StreamReadinessTimeoutMs: 5000}}
	if _, err := exec.Execute(context.Background(), req); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	items := gjson.GetBytes(gotBody, "input").Array()
	foundFuncCall := false
	foundFuncOut := false
	for _, it := range items {
		typ := it.Get("type").String()
		if typ == "item_reference" {
			t.Errorf("item_reference should have been stripped")
		}
		if typ == "internal_chat_message_metadata_passthrough" {
			t.Errorf("internal_chat_message_metadata_passthrough should have been stripped")
		}
		if typ == "custom_tool_call" || typ == "custom_tool_call_output" {
			t.Errorf("custom tool types should have been converted, got %s", typ)
		}
		if typ == "function_call" && it.Get("call_id").String() == "call-1" {
			foundFuncCall = true
		}
		if typ == "function_call_output" && it.Get("call_id").String() == "call-1" {
			foundFuncOut = true
		}
	}
	if !foundFuncCall {
		t.Errorf("expected function_call from custom_tool_call")
	}
	if !foundFuncOut {
		t.Errorf("expected function_call_output from custom_tool_call_output")
	}
	if len(items) != 4 {
		t.Errorf("expected 4 items, got %d", len(items))
	}
}

func TestGrokCLIExecutor_ReasoningModelGatingAndMaxMapping(t *testing.T) {
	server := func(t *testing.T) (*httptest.Server, *[]byte) {
		var gotBody []byte
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotBody, _ = io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			completed, _ := json.Marshal(map[string]any{"type": "response.completed", "response": map[string]any{"id": "r1", "status": "completed", "output": []any{}, "usage": map[string]any{}}})
			fmt.Fprintln(w, "data: "+string(completed))
			fmt.Fprintln(w)
		}))
		return ts, &gotBody
	}

	cases := []struct {
		model          string
		effort         string
		wantReasoning  bool
		wantEffort     string
	}{
		{"grok-cli/grok-4.5", "max", true, "xhigh"},
		{"grok-cli/grok-build-0.1", "high", false, ""},
		{"grok-cli/grok-4.3", "low", false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.model+"/"+tc.effort, func(t *testing.T) {
			ts, gotBody := server(t)
			defer ts.Close()
			base := NewBaseExecutor()
			exec := NewGrokCLIExecutor(base)
			body, _ := json.Marshal(map[string]any{
				"model":            tc.model,
				"reasoning_effort": tc.effort,
				"messages":         []any{map[string]any{"role": "user", "content": "hi"}},
			})
			req := &Request{Provider: "grok-cli", Model: tc.model, BaseURL: ts.URL, AccessToken: "grok-at-123", Body: body, StreamConfig: &StreamConfig{FetchTimeoutMs: 5000, StreamIdleTimeoutMs: 5000, StreamReadinessTimeoutMs: 5000}}
			if _, err := exec.Execute(context.Background(), req); err != nil {
				t.Fatalf("Execute error: %v", err)
			}
			hasReasoning := gjson.GetBytes(*gotBody, "reasoning").Exists()
			if hasReasoning != tc.wantReasoning {
				t.Errorf("reasoning exists=%v, want %v", hasReasoning, tc.wantReasoning)
			}
			if tc.wantEffort != "" {
				if got := gjson.GetBytes(*gotBody, "reasoning.effort").String(); got != tc.wantEffort {
					t.Errorf("reasoning.effort=%q, want %q", got, tc.wantEffort)
				}
			}
		})
	}
}

func TestGrokCLIExecutor_HostedToolsPreserved(t *testing.T) {
	var gotBody []byte
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		completed, _ := json.Marshal(map[string]any{"type": "response.completed", "response": map[string]any{"id": "r1", "status": "completed", "output": []any{}, "usage": map[string]any{}}})
		fmt.Fprintln(w, "data: "+string(completed))
		fmt.Fprintln(w)
	}))
	defer ts.Close()

	base := NewBaseExecutor()
	exec := NewGrokCLIExecutor(base)
	body, _ := json.Marshal(map[string]any{
		"model": "grok-cli/grok-4.5",
		"messages": []any{map[string]any{"role": "user", "content": "find"}},
		"tools": []any{
			map[string]any{"type": "web_search"},
			map[string]any{"type": "x_search"},
			map[string]any{"type": "file_search"},
			map[string]any{"type": "image_generation"},
			map[string]any{"type": "code_interpreter"},
			map[string]any{"type": "mcp"},
			map[string]any{"type": "local_shell"},
		},
	})
	req := &Request{Provider: "grok-cli", Model: "grok-cli/grok-4.5", BaseURL: ts.URL, AccessToken: "grok-at-123", Body: body, StreamConfig: &StreamConfig{FetchTimeoutMs: 5000, StreamIdleTimeoutMs: 5000, StreamReadinessTimeoutMs: 5000}}
	if _, err := exec.Execute(context.Background(), req); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	tools := gjson.GetBytes(gotBody, "tools").Array()
	if len(tools) != 7 {
		t.Fatalf("expected 7 tools, got %d", len(tools))
	}
	for _, typ := range []string{"web_search", "x_search", "file_search", "image_generation", "code_interpreter", "mcp", "local_shell"} {
		found := false
		for _, tool := range tools {
			if tool.Get("type").String() == typ {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %s tool to be preserved", typ)
		}
	}
}

func TestTranslateGrokCLI(t *testing.T) {
	cases := []struct {
		name       string
		statusCode int
		body       map[string]any
		wantType   string
		wantCode   string
	}{
		{
			name:       "402 spending limit",
			statusCode: http.StatusPaymentRequired,
			body:       map[string]any{"error": map[string]any{"message": "Spending limit reached"}},
			wantType:   "rate_limit_error",
			wantCode:   "insufficient_quota",
		},
		{
			name:       "401 auth",
			statusCode: http.StatusUnauthorized,
			body:       map[string]any{"error": map[string]any{"message": "Unauthorized"}},
			wantType:   "authentication_error",
			wantCode:   "invalid_api_key",
		},
		{
			name:       "403 forbidden",
			statusCode: http.StatusForbidden,
			body:       map[string]any{"error": map[string]any{"message": "Forbidden"}},
			wantType:   "permission_error",
			wantCode:   "insufficient_quota",
		},
		{
			name:       "429 rate limit",
			statusCode: http.StatusTooManyRequests,
			body:       map[string]any{"error": map[string]any{"message": "Too many requests"}},
			wantType:   "rate_limit_error",
			wantCode:   "rate_limit_exceeded",
		},
		{
			name:       "context length",
			statusCode: http.StatusBadRequest,
			body:       map[string]any{"error": map[string]any{"message": "maximum context length exceeded"}},
			wantType:   "invalid_request_error",
			wantCode:   "context_length_exceeded",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, _ := json.Marshal(tc.body)
			out := providers.TranslateGrokCLI(tc.statusCode, b)
			if !gjson.ValidBytes(out) {
				t.Fatalf("invalid translated body: %s", string(out))
			}
			if got := gjson.GetBytes(out, "error.type").String(); got != tc.wantType {
				t.Errorf("type=%q, want %q", got, tc.wantType)
			}
		if got := gjson.GetBytes(out, "error.code").String(); got != tc.wantCode {
			t.Errorf("code=%q, want %q", got, tc.wantCode)
		}
		})
	}
}

func TestGrokCLIExecutor_Retry_429ThenSuccess(t *testing.T) {
	completed, _ := json.Marshal(map[string]any{"type": "response.completed", "response": map[string]any{"id": "r1", "status": "completed", "output": []any{}, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}})
	count := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		if count == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"rate limit"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "data: "+string(completed))
		fmt.Fprintln(w)
	}))
	defer ts.Close()

	base := NewBaseExecutor()
	exec := NewGrokCLIExecutor(base)
	body, _ := json.Marshal(map[string]any{"input": []any{}})
	req := &Request{
		Provider: "grok-cli",
		Model: "grok-cli/grok-4.5",
		BaseURL: ts.URL,
		AccessToken: "grok-at-123",
		Body: body,
		StreamConfig: &StreamConfig{FetchTimeoutMs: 5000, StreamIdleTimeoutMs: 5000, StreamReadinessTimeoutMs: 5000},
	}

	start := time.Now()
	_, err := exec.Execute(context.Background(), req)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("expected retry success, got error: %v", err)
	}
	if count != 2 {
		t.Fatalf("requests=%d, want 2", count)
	}
	if elapsed < 1900*time.Millisecond {
		t.Fatalf("retry delay too short: %v", elapsed)
	}
}

func TestGrokCLIExecutor_Retry_502ThenSuccess(t *testing.T) {
	completed, _ := json.Marshal(map[string]any{"type": "response.completed", "response": map[string]any{"id": "r1", "status": "completed", "output": []any{}, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}})
	count := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		if count == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":{"message":"bad gateway"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "data: "+string(completed))
		fmt.Fprintln(w)
	}))
	defer ts.Close()

	base := NewBaseExecutor()
	exec := NewGrokCLIExecutor(base)
	body, _ := json.Marshal(map[string]any{"input": []any{}})
	req := &Request{
		Provider: "grok-cli",
		Model: "grok-cli/grok-4.5",
		BaseURL: ts.URL,
		AccessToken: "grok-at-123",
		Body: body,
		StreamConfig: &StreamConfig{FetchTimeoutMs: 5000, StreamIdleTimeoutMs: 5000, StreamReadinessTimeoutMs: 5000},
	}

	start := time.Now()
	_, err := exec.Execute(context.Background(), req)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("expected retry success, got error: %v", err)
	}
	if count != 2 {
		t.Fatalf("requests=%d, want 2", count)
	}
	if elapsed < 1400*time.Millisecond {
		t.Fatalf("retry delay too short: %v", elapsed)
	}
}

func TestGrokCLIExecutor_Retry_429MaxAttempts(t *testing.T) {
	count := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limit"}}`))
	}))
	defer ts.Close()

	base := NewBaseExecutor()
	exec := NewGrokCLIExecutor(base)
	body, _ := json.Marshal(map[string]any{"input": []any{}})
	req := &Request{
		Provider: "grok-cli",
		Model: "grok-cli/grok-4.5",
		BaseURL: ts.URL,
		AccessToken: "grok-at-123",
		Body: body,
		StreamConfig: &StreamConfig{FetchTimeoutMs: 5000, StreamIdleTimeoutMs: 5000, StreamReadinessTimeoutMs: 5000},
	}

	_, err := exec.Execute(context.Background(), req)
	if err == nil {
		t.Fatal("expected error after max retries")
	}
	if count != 2 {
		t.Fatalf("requests=%d, want 2", count)
	}
	upErr, ok := err.(*UpstreamError)
	if !ok || upErr.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 UpstreamError, got %T %v", err, err)
	}
}

func TestGrokCLIExecutor_Retry_NoRetryOnAuth(t *testing.T) {
	count := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"unauthorized"}}`))
	}))
	defer ts.Close()

	base := NewBaseExecutor()
	exec := NewGrokCLIExecutor(base)
	body, _ := json.Marshal(map[string]any{"input": []any{}})
	req := &Request{
		Provider: "grok-cli",
		Model: "grok-cli/grok-4.5",
		BaseURL: ts.URL,
		AccessToken: "grok-at-123",
		Body: body,
		StreamConfig: &StreamConfig{FetchTimeoutMs: 5000, StreamIdleTimeoutMs: 5000, StreamReadinessTimeoutMs: 5000},
	}

	_, err := exec.Execute(context.Background(), req)
	if err == nil {
		t.Fatal("expected error")
	}
	if count != 1 {
		t.Fatalf("requests=%d, want 1 (no retry for 401)", count)
	}
}
