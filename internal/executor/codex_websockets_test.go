package executor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/tidwall/gjson"
)

func TestBuildCodexResponsesWebsocketURL(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "https base",
			input: "https://chatgpt.com/backend-api/codex/responses",
			want:  "wss://chatgpt.com/backend-api/codex/responses",
		},
		{
			name:  "http base",
			input: "http://chatgpt.com/backend-api/codex/responses",
			want:  "ws://chatgpt.com/backend-api/codex/responses",
		},
		{
			name:    "unsupported scheme",
			input:   "ftp://chatgpt.com/backend-api/codex/responses",
			wantErr: true,
		},
		{
			name:    "empty host",
			input:   "https:///backend-api/codex/responses",
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := buildCodexResponsesWebsocketURL(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

func TestCodexWebsocketsExecutor_DialSuccess(t *testing.T) {
	var gotHeaders http.Header
	done := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
		})
		if err != nil {
			t.Errorf("accept failed: %v", err)
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")
		msgType, reader, err := c.Reader(context.Background())
		if err != nil {
			t.Errorf("reader failed: %v", err)
			return
		}
		body, _ := io.ReadAll(reader)
		_ = body
		_ = msgType
		close(done)
	}))
	defer ts.Close()

	base := NewBaseExecutor()
	exec := NewCodexWebsocketsExecutor(base)
	req := &Request{
		Provider:    "codex",
		Model:       "codex/gpt-5.4",
		BaseURL:     ts.URL,
		AccessToken: "test-access-token",
		Headers: map[string]string{
			"User-Agent": "test-agent",
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := exec.Dial(ctx, "session-1", "auth-1", req)
	if err != nil {
		t.Fatalf("Dial error: %v", err)
	}
	if res == nil || res.Conn == nil {
		t.Fatalf("expected connection, got nil")
	}
	// Send a message to trigger server header inspection completion.
	if err := res.Conn.Write(ctx, websocket.MessageText, []byte(`{"type":"response.create"}`)); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for server to accept message")
	}
	_ = res.Conn.Close(websocket.StatusNormalClosure, "")

	if got, want := gotHeaders.Get("Authorization"), "Bearer test-access-token"; got != want {
		t.Errorf("Authorization header: got %q, want %q", got, want)
	}
	if got, want := gotHeaders.Get("OpenAI-Beta"), codexResponsesWebsocketBetaHeaderValue; got != want {
		t.Errorf("OpenAI-Beta header: got %q, want %q", got, want)
	}
	if got, want := gotHeaders.Get("Origin"), "https://chatgpt.com"; got != want {
		t.Errorf("Origin header: got %q, want %q", got, want)
	}
	if got, want := gotHeaders.Get("User-Agent"), "test-agent"; got != want {
		t.Errorf("User-Agent header: got %q, want %q", got, want)
	}
}

func TestCodexWebsocketsExecutor_DialFailure(t *testing.T) {
	// Non-websocket endpoint should reject the upgrade.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not a websocket endpoint", http.StatusBadRequest)
	}))
	defer ts.Close()

	base := NewBaseExecutor()
	exec := NewCodexWebsocketsExecutor(base)
	req := &Request{
		Provider:    "codex",
		BaseURL:     ts.URL,
		AccessToken: "test-token",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := exec.Dial(ctx, "", "", req)
	if err == nil {
		t.Fatal("expected dial error, got nil")
	}
	if !strings.Contains(err.Error(), "dial") {
		t.Errorf("expected dial error to mention dial, got: %v", err)
	}
}

func TestCodexWebsocketsExecutor_CloseExecutionSession(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
		})
		if err != nil {
			t.Errorf("accept failed: %v", err)
			return
		}
		closed := make(chan struct{})
		go func() {
			_, _, _ = c.Read(context.Background())
			close(closed)
		}()
		select {
		case <-closed:
		case <-time.After(5 * time.Second):
			t.Error("connection was not closed within timeout")
		}
	}))
	defer ts.Close()

	base := NewBaseExecutor()
	exec := NewCodexWebsocketsExecutor(base)
	req := &Request{
		Provider:    "codex",
		BaseURL:     ts.URL,
		AccessToken: "test-token",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sessionID := "close-test-session"
	res, err := exec.Dial(ctx, sessionID, "auth-2", req)
	if err != nil {
		t.Fatalf("Dial error: %v", err)
	}
	if res == nil || res.Conn == nil {
		t.Fatalf("expected connection")
	}
	exec.CloseExecutionSession(sessionID)

	// After CloseExecutionSession, the connection should be dead.
	readCtx, cancelRead := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelRead()
	_, _, err = res.Conn.Read(readCtx)
	if err == nil {
		t.Fatal("expected connection to be closed after session close")
	}
}

// websocketTestServer creates a test websocket server that captures the first
// upstream request and optionally replies with a completed response.
func websocketTestServer(t *testing.T, capture chan<- []byte, responses [][]byte) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Fatalf("upgrade failed: %v", err)
		}
		defer c.Close(websocket.StatusNormalClosure, "")
		_, payload, err := c.Read(context.Background())
		if err != nil {
			t.Fatalf("read failed: %v", err)
		}
		if capture != nil {
			capture <- bytes.Clone(payload)
		}
		for _, resp := range responses {
			if err := c.Write(context.Background(), websocket.MessageText, resp); err != nil {
				t.Fatalf("write failed: %v", err)
			}
		}
	}))
}

func TestNormalizeCodexWebsocketRequest_UsesConfiguredImageGenerationModel(t *testing.T) {
	req := &Request{
		Model: "gpt-image-2",
		ProviderSpecificData: map[string]string{
			"imageGenerationModel": "custom-image-model",
		},
	}
	out, reqType := normalizeCodexWebsocketRequest(req, []byte(`{"model":"gpt-image-2","input":[{"type":"message","role":"user","content":"draw"}]}`))
	if reqType != "response.create" {
		t.Fatalf("request type = %q, want response.create", reqType)
	}
	if got := gjson.GetBytes(out, "tools.0.model").String(); got != "custom-image-model" {
		t.Fatalf("injected image model = %q, want custom-image-model; payload=%s", got, out)
	}
}

func TestCodexWebsocketsExecutor_Execute_NormalizesResponseCreate(t *testing.T) {
	captured := make(chan []byte, 1)
	completed := []byte(`{"type":"response.completed","response":{"id":"resp-1","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`)
	ts := websocketTestServer(t, captured, [][]byte{completed})
	defer ts.Close()

	base := NewBaseExecutor()
	exec := NewCodexWebsocketsExecutor(base)
	req := &Request{
		Provider:    "codex",
		Model:       "gpt-5.4",
		BaseURL:     ts.URL,
		AccessToken: "test-token",
		Body:        []byte(`{"model":"gpt-5.4","input":[{"type":"message","role":"user","content":"hello"}]}`),
	}
	_, err := exec.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	select {
	case payload := <-captured:
		if got := gjson.GetBytes(payload, "type").String(); got != "response.create" {
			t.Fatalf("type = %q, want response.create; payload=%s", got, payload)
		}
		if gjson.GetBytes(payload, "stream").Exists() {
			t.Fatalf("stream should be removed; payload=%s", payload)
		}
		if gjson.GetBytes(payload, "input.0.role").String() != "user" {
			t.Fatalf("input role not preserved; payload=%s", payload)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for upstream payload")
	}
}

func TestCodexWebsocketsExecutor_Execute_PreservesExplicitResponseAppend(t *testing.T) {
	captured := make(chan []byte, 1)
	completed := []byte(`{"type":"response.completed","response":{"id":"resp-2","previous_response_id":"resp-1","output":[],"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}}}`)
	ts := websocketTestServer(t, captured, [][]byte{completed})
	defer ts.Close()

	base := NewBaseExecutor()
	exec := NewCodexWebsocketsExecutor(base)
	req := &Request{
		Provider:    "codex",
		Model:       "gpt-5.4",
		BaseURL:     ts.URL,
		AccessToken: "test-token",
		Body:        []byte(`{"type":"response.append","model":"gpt-5.4","previous_response_id":"resp-1","input":[{"type":"message","role":"user","content":"follow-up"}]}`),
	}
	res, err := exec.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	select {
	case payload := <-captured:
		if got := gjson.GetBytes(payload, "type").String(); got != "response.append" {
			t.Fatalf("type = %q, want response.append; payload=%s", got, payload)
		}
		if got := gjson.GetBytes(payload, "previous_response_id").String(); got != "resp-1" {
			t.Fatalf("previous_response_id = %q, want resp-1; payload=%s", got, payload)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for upstream payload")
	}

	if got := gjson.GetBytes(res.Body, "response.id").String(); got != "resp-2" {
		t.Fatalf("response id = %q, want resp-2; body=%s", got, res.Body)
	}
}

func TestCodexWebsocketsExecutor_Execute_PropagatesPreviousResponseIDEndToEnd(t *testing.T) {
	captured := make(chan []byte, 1)
	completed := []byte(`{"type":"response.completed","response":{"id":"resp-upstream","output":[],"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}}}`)
	ts := websocketTestServer(t, captured, [][]byte{completed})
	defer ts.Close()

	base := NewBaseExecutor()
	exec := NewCodexWebsocketsExecutor(base)
	req := &Request{
		Provider:    "codex",
		Model:       "gpt-5.4",
		BaseURL:     ts.URL,
		AccessToken: "test-token",
		Body:        []byte(`{"model":"gpt-5.4","previous_response_id":"client-prev","input":[{"type":"message","role":"user","content":"next"}]}`),
	}
	_, err := exec.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	select {
	case payload := <-captured:
		if got := gjson.GetBytes(payload, "previous_response_id").String(); got != "client-prev" {
			t.Fatalf("previous_response_id = %q, want client-prev; payload=%s", got, payload)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for upstream payload")
	}
}

func TestCodexWebsocketsExecutor_Execute_IncrementalInputStateMachine(t *testing.T) {

	var captured [][]byte
	var mu sync.Mutex
	var responseID int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Fatalf("upgrade failed: %v", err)
		}
		defer c.Close(websocket.StatusNormalClosure, "")
		for {
			_, payload, err := c.Read(context.Background())
			if err != nil {
				return
			}
			mu.Lock()
			captured = append(captured, bytes.Clone(payload))
			responseID++
			id := responseID
			mu.Unlock()
			completed := []byte(`{"type":"response.completed","response":{"id":"resp-` + fmt.Sprintf("%d", id) + `","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`)
			if err := c.Write(context.Background(), websocket.MessageText, completed); err != nil {
				t.Fatalf("write failed: %v", err)
			}
		}
	}))
	defer ts.Close()

	base := NewBaseExecutor()
	exec := NewCodexWebsocketsExecutor(base)
	ctx := context.Background()

	firstReq := &Request{
		Provider:     "codex",
		Model:        "gpt-5.4",
		BaseURL:      ts.URL,
		AccessToken:  "test-token",
		ConnectionID: "session-inc",
		Body:         []byte(`{"model":"gpt-5.4","input":[{"type":"message","role":"user","content":"first"}]}`),
	}
	if _, err := exec.Execute(ctx, firstReq); err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}

	secondReq := &Request{
		Provider:     "codex",
		Model:        "gpt-5.4",
		BaseURL:      ts.URL,
		AccessToken:  "test-token",
		ConnectionID: "session-inc",
		Body:         []byte(`{"model":"gpt-5.4","input":[{"type":"message","role":"user","content":"second"}]}`),
	}
	if _, err := exec.Execute(ctx, secondReq); err != nil {
		t.Fatalf("second Execute() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(captured) != 2 {
		t.Fatalf("expected 2 upstream payloads, got %d", len(captured))
	}
	first := captured[0]
	second := captured[1]
	if got := gjson.GetBytes(first, "type").String(); got != "response.create" {
		t.Fatalf("first type = %q, want response.create", got)
	}
	if got := gjson.GetBytes(second, "type").String(); got != "response.create" {
		t.Fatalf("second type = %q, want response.create", got)
	}
	// Previous_response_id propagation now happens via transcript prepend when missing.
	if prev := gjson.GetBytes(second, "previous_response_id").String(); prev == "" {
		t.Fatalf("second request should propagate previous_response_id, got %q", prev)
	}
	if got := len(gjson.GetBytes(second, "input").Array()); got < 1 {
		t.Fatalf("second upstream input length = %d, want at least 1", got)
	}
}

func TestCodexWebsocketsExecutor_Execute_ResponsesWebsocketLiteMode(t *testing.T) {
	captured := make(chan []byte, 1)
	completed := []byte(`{"type":"response.completed","response":{"id":"resp-lite","output":[],"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}}}`)
	ts := websocketTestServer(t, captured, [][]byte{completed})
	defer ts.Close()

	base := NewBaseExecutor()
	exec := NewCodexWebsocketsExecutor(base)
	req := &Request{
		Provider:    "codex",
		Model:       "gpt-5.6-sol",
		BaseURL:     ts.URL,
		AccessToken: "test-token",
		Headers: map[string]string{
			codexResponsesLiteHeader: "true",
		},
		Body: []byte(`{"model":"gpt-5.6-sol","input":[{"type":"additional_tools","role":"developer","tools":[{"type":"custom","name":"exec"}]},{"role":"user","content":"hello"}],"parallel_tool_calls":true}`),
	}
	_, err := exec.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	select {
	case payload := <-captured:
		if gjson.GetBytes(payload, "tools").Exists() {
			t.Fatalf("lite mode should not have injected tools; payload=%s", payload)
		}
		if got := gjson.GetBytes(payload, "input.0.type").String(); got != "additional_tools" {
			t.Fatalf("input.0.type = %q, want additional_tools; payload=%s", got, payload)
		}
		if ptc := gjson.GetBytes(payload, "parallel_tool_calls"); !ptc.Exists() || ptc.Bool() {
			t.Fatalf("lite mode parallel_tool_calls should be false; payload=%s", payload)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for upstream payload")
	}
}

func TestCodexWebsocketsExecutor_Execute_ResponsesLiteViaMetadata(t *testing.T) {
	captured := make(chan []byte, 1)
	completed := []byte(`{"type":"response.completed","response":{"id":"resp-lite-md","output":[],"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}}}`)
	ts := websocketTestServer(t, captured, [][]byte{completed})
	defer ts.Close()

	base := NewBaseExecutor()
	exec := NewCodexWebsocketsExecutor(base)
	req := &Request{
		Provider:    "codex",
		Model:       "gpt-5.6-luna",
		BaseURL:     ts.URL,
		AccessToken: "test-token",
		Body:        []byte(`{"model":"gpt-5.6-luna","input":[{"type":"message","role":"user","content":"hello"}],"parallel_tool_calls":true,"client_metadata":{"ws_request_header_x_openai_internal_codex_responses_lite":"true"}}`),
	}
	_, err := exec.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	select {
	case payload := <-captured:
		if ptc := gjson.GetBytes(payload, "parallel_tool_calls"); !ptc.Exists() || ptc.Bool() {
			t.Fatalf("lite mode (metadata) parallel_tool_calls should be false; payload=%s", payload)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for upstream payload")
	}
}

func TestCodexWebsocketsExecutor_SanitizeCodexInputItemIDs(t *testing.T) {
	longReasoningID := "rs_" + strings.Repeat("a", 64)
	body := []byte(`{"model":"gpt-5.4","input":[{"type":"reasoning","id":"` + longReasoningID + `","encrypted_content":"gAAAA","summary":[]},{"type":"message","id":"msg-1"}]}`)
	out := sanitizeCodexInputItemIDs(body)
	input := gjson.GetBytes(out, "input").Array()
	if len(input) != 1 {
		t.Fatalf("input length = %d, want 1; got %s", len(input), out)
	}
	if got := input[0].Get("id").String(); got != "msg-1" {
		t.Fatalf("remaining item id = %q, want msg-1; got %s", got, out)
	}
}

func TestCodexWebsocketsExecutor_ExecuteStream_NormalizesAndCompletes(t *testing.T) {
	captured := make(chan []byte, 1)
	created := []byte(`{"type":"response.created","response":{"id":"resp-stream","output":[]}}`)
	itemDone := []byte(`{"type":"response.output_item.done","output_index":0,"item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"streamed"}]}}`)
	completed := []byte(`{"type":"response.completed","response":{"id":"resp-stream","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`)
	ts := websocketTestServer(t, captured, [][]byte{created, itemDone, completed})
	defer ts.Close()

	base := NewBaseExecutor()
	exec := NewCodexWebsocketsExecutor(base)
	req := &Request{
		Provider:    "codex",
		Model:       "gpt-5.4",
		BaseURL:     ts.URL,
		AccessToken: "test-token",
		Body:        []byte(`{"model":"gpt-5.4","input":[{"type":"message","role":"user","content":"stream"}]}`),
	}
	result, err := exec.ExecuteStream(context.Background(), req)
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}

	var chunks []StreamChunk
	for chunk := range result.Chunks {
		chunks = append(chunks, chunk)
		if chunk.Err != nil {
			t.Fatalf("stream chunk error = %v", chunk.Err)
		}
	}

	select {
	case payload := <-captured:
		if got := gjson.GetBytes(payload, "type").String(); got != "response.create" {
			t.Fatalf("type = %q, want response.create; payload=%s", got, payload)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for upstream payload")
	}

	var sawCompleted bool
	for _, c := range chunks {
		if c.Payload == nil {
			continue
		}
		if got := gjson.GetBytes(c.Payload, "type").String(); got == "response.completed" {
			sawCompleted = true
		}
	}
	if !sawCompleted {
		t.Fatalf("expected response.completed in stream chunks; got %d chunks", len(chunks))
	}
}
