package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tidwall/gjson"
)

// freebuffTestServer routes the three freebuff API endpoints used by the
// executor: /api/v1/freebuff/session, /api/v1/agent-runs, and the chat endpoint.
// Handlers are configurable per test.
type freebuffTestServer struct {
	ts      *httptest.Server
	session func(w http.ResponseWriter, r *http.Request)
	runs    func(w http.ResponseWriter, r *http.Request)
	chat    func(w http.ResponseWriter, r *http.Request)
}

func newFreebuffTestServer() *freebuffTestServer {
	f := &freebuffTestServer{}
	f.ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		switch r.URL.Path {
		case "/api/v1/freebuff/session":
			if f.session != nil {
				f.session(w, r)
				return
			}
		case "/api/v1/agent-runs":
			if f.runs != nil {
				f.runs(w, r)
				return
			}
		case "/api/v1/chat/completions":
			if f.chat != nil {
				f.chat(w, r)
				return
			}
		}
		http.NotFound(w, r)
	}))
	// Route session/run claims to this local server (the executor resolves the
	// fixed codebuff origin otherwise).
	testFreebuffOrigin = func(_ *Request) string { return f.ts.URL }
	return f
}

func (f *freebuffTestServer) Close() { f.ts.Close() }

func (f *freebuffTestServer) baseURL() string {
	return f.ts.URL + "/api/v1/chat/completions"
}

func freebuffTestReq(ts *freebuffTestServer, body []byte) *Request {
	return &Request{
		Provider:    "freebuff",
		Model:       "freebuff/deepseek/deepseek-v4-flash",
		BaseURL:     ts.baseURL(),
		AccessToken: "fb-at-123",
		Body:        body,
		StreamConfig: &StreamConfig{
			FetchTimeoutMs:           5000,
			StreamIdleTimeoutMs:      5000,
			StreamReadinessTimeoutMs: 5000,
		},
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	b, _ := json.Marshal(v)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

// defaultFreebuffHandlers returns handlers that claim a session, register a run,
// and return a 200 chat response.
func defaultFreebuffHandlers() (session, runs, chat http.HandlerFunc) {
	session = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "active", "instanceId": "inst-1",
			"expiresAt": time.Now().Add(time.Hour).Format(time.RFC3339),
		})
	}
	runs = func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Action string `json:"action"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Action == "FINISH" {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"runId": "run-1"})
	}
	chat = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"id": "chat-1", "object": "chat.completion",
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "hi"},
				"finish_reason": "stop",
			}},
		})
	}
	return session, runs, chat
}

func TestFreebuffExecutor_FullFlow(t *testing.T) {
	var sessionHeader atomic.Value // x-freebuff-model
	var chatBody atomic.Value      // body bytes
	var finishStatus atomic.Value  // FINISH status

	session, _, chat := defaultFreebuffHandlers()
	ts := newFreebuffTestServer()
	defer ts.Close()

	ts.session = func(w http.ResponseWriter, r *http.Request) {
		sessionHeader.Store(r.Header.Get("x-freebuff-model"))
		session(w, r)
	}
	ts.runs = func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Action string `json:"action"`
			RunID  string `json:"runId"`
			Status string `json:"status"`
			Agent  string `json:"agentId"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Action == "FINISH" {
			finishStatus.Store(req.Status)
			writeJSON(w, http.StatusOK, map[string]any{"ok": true})
			return
		}
		if req.Agent != "base2-free-deepseek-flash" {
			t.Errorf("agentId=%q, want base2-free-deepseek-flash", req.Agent)
		}
		writeJSON(w, http.StatusOK, map[string]any{"runId": "run-1"})
	}
	// finishRun is fire-and-forget — wait for the async FINISH callback.
	waitFor := func(v *atomic.Value) string {
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if s := v.Load(); s != nil {
				return s.(string)
			}
			time.Sleep(10 * time.Millisecond)
		}
		return ""
	}
	ts.chat = func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		chatBody.Store(b)
		chat(w, r)
	}

	base := NewBaseExecutor()
	exec := NewFreebuffExecutor(base)
	body, _ := json.Marshal(map[string]any{
		"model":     "deepseek/deepseek-v4-flash",
		"stream":    false,
		"messages":  []any{map[string]any{"role": "user", "content": "hi"}},
		"reasoning": map[string]any{"effort": "high"},
	})
	res, err := exec.Execute(context.Background(), freebuffTestReq(ts, body))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if res.StatusCode != 200 {
		t.Fatalf("status=%d, want 200", res.StatusCode)
	}

	if got := sessionHeader.Load().(string); got != "deepseek/deepseek-v4-flash" {
		t.Errorf("x-freebuff-model=%q", got)
	}
	got := chatBody.Load().([]byte)

	// Model should be unprefixed.
	if m := gjson.GetBytes(got, "model").String(); m != "deepseek/deepseek-v4-flash" {
		t.Errorf("model=%q", m)
	}
	// codebuff_metadata at TOP LEVEL (not nested under `codebuff`).
	if !gjson.GetBytes(got, "codebuff_metadata").Exists() {
		t.Fatalf("missing codebuff_metadata: %s", got)
	}
	if gjson.GetBytes(got, "codebuff").Exists() {
		t.Errorf("codebuff should not be nested under a codebuff object")
	}
	if runID := gjson.GetBytes(got, "codebuff_metadata.run_id").String(); runID != "run-1" {
		t.Errorf("run_id=%q, want run-1", runID)
	}
	if cost := gjson.GetBytes(got, "codebuff_metadata.cost_mode").String(); cost != "free" {
		t.Errorf("cost_mode=%q, want free", cost)
	}
	if trace := gjson.GetBytes(got, "codebuff_metadata.trace_session_id").String(); trace == "" {
		t.Errorf("trace_session_id missing")
	}
	if inst := gjson.GetBytes(got, "codebuff_metadata.freebuff_instance_id").String(); inst != "inst-1" {
		t.Errorf("freebuff_instance_id=%q, want inst-1", inst)
	}
	if client := gjson.GetBytes(got, "codebuff_metadata.client_id").String(); client == "" {
		t.Errorf("client_id missing")
	}
	// reasoning fields stripped (backend applies its own server-side).
	if gjson.GetBytes(got, "reasoning_effort").Exists() || gjson.GetBytes(got, "reasoning").Exists() {
		t.Errorf("reasoning fields should be stripped")
	}
	if allow := gjson.GetBytes(got, "provider.allow_fallbacks").Bool(); allow {
		t.Errorf("provider.allow_fallbacks should be false")
	}
	// System marker injected as messages[0].
	first := gjson.GetBytes(got, "messages.0")
	if first.Get("role").String() != "system" {
		t.Errorf("messages[0].role=%q, want system", first.Get("role").String())
	}
	if !strings.HasPrefix(first.Get("content").String(), freebuffSystemMarker) {
		t.Errorf("messages[0] must open with the freebuff system marker")
	}
	if got := waitFor(&finishStatus); got != "completed" {
		t.Errorf("FINISH status=%q, want completed", got)
	}
}

func TestFreebuffExecutor_StreamFlow(t *testing.T) {
	session, runs, _ := defaultFreebuffHandlers()
	ts := newFreebuffTestServer()
	defer ts.Close()
	ts.session = session
	ts.runs = runs
	ts.chat = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		fmt.Fprintln(w, `data: {"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":null}]}`)
		flusher.Flush()
		fmt.Fprintln(w, `data: [DONE]`)
		flusher.Flush()
	}

	base := NewBaseExecutor()
	exec := NewFreebuffExecutor(base)
	body, _ := json.Marshal(map[string]any{
		"model":    "deepseek/deepseek-v4-flash",
		"stream":   true,
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	})
	res, err := exec.ExecuteStream(context.Background(), freebuffTestReq(ts, body))
	if err != nil {
		t.Fatalf("ExecuteStream error: %v", err)
	}
	var chunks []string
	for chunk := range res.Chunks {
		if chunk.Err != nil {
			t.Fatalf("chunk error: %v", chunk.Err)
		}
		if chunk.Payload != nil {
			chunks = append(chunks, string(chunk.Payload))
		}
	}
	if len(chunks) == 0 {
		t.Fatal("no chunks received")
	}
	if !strings.Contains(strings.Join(chunks, "\n"), "chat.completion.chunk") {
		t.Errorf("expected chunk event, got %v", chunks)
	}
}

func TestFreebuffExecutor_StreamStripsSplitToolCalls(t *testing.T) {
	session, runs, _ := defaultFreebuffHandlers()
	ts := newFreebuffTestServer()
	defer ts.Close()
	ts.session = session
	ts.runs = runs
	ts.chat = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		for _, content := range []string{"before <tool", `_call>{"name":"read"}`, `}</tool_call> after`} {
			fmt.Fprintf(w, `data: {"choices":[{"delta":{"content":%q}}]}`+"\n", content)
			flusher.Flush()
		}
		fmt.Fprintln(w, `data: [DONE]`)
		flusher.Flush()
	}

	base := NewBaseExecutor()
	exec := NewFreebuffExecutor(base)
	body, _ := json.Marshal(map[string]any{
		"model":    "deepseek/deepseek-v4-flash",
		"stream":   true,
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	})
	res, err := exec.ExecuteStream(context.Background(), freebuffTestReq(ts, body))
	if err != nil {
		t.Fatalf("ExecuteStream error: %v", err)
	}
	var chunks []string
	for chunk := range res.Chunks {
		if chunk.Err != nil {
			t.Fatalf("chunk error: %v", chunk.Err)
		}
		chunks = append(chunks, string(chunk.Payload))
	}
	joined := strings.Join(chunks, "\n")
	if strings.Contains(joined, "tool_call") {
		t.Fatalf("stream leaked Freebuff tool call: %s", joined)
	}
	if !strings.Contains(joined, "before") || !strings.Contains(joined, "after") {
		t.Fatalf("stream lost visible content: %s", joined)
	}
}

// TestFreebuffExecutor_StreamFinishLifecycle verifies that FINISH for a
// streaming request tracks the REAL stream lifecycle, not the connection open:
//   - stream completes cleanly  -> FINISH status "completed"
//   - stream errors mid-flight  -> FINISH status "failed"
func TestFreebuffExecutor_StreamFinishLifecycle(t *testing.T) {
	t.Run("clean-completion", func(t *testing.T) {
		var finishStatus atomic.Value
		session, _, _ := defaultFreebuffHandlers()
		ts := newFreebuffTestServer()
		defer ts.Close()
		ts.session = session
		ts.runs = func(w http.ResponseWriter, r *http.Request) {
			var req struct {
				Action string `json:"action"`
				Status string `json:"status"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.Action == "FINISH" {
				finishStatus.Store(req.Status)
				writeJSON(w, http.StatusOK, map[string]any{"ok": true})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"runId": "run-1"})
		}
		ts.chat = func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			flusher, _ := w.(http.Flusher)
			for i := 0; i < 5; i++ {
				fmt.Fprintf(w, `data: {"id":"%d","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"c%d"},"finish_reason":null}]}
`, i, i)
				flusher.Flush()
			}
			fmt.Fprintln(w, `data: [DONE]`)
			flusher.Flush()
		}

		base := NewBaseExecutor()
		exec := NewFreebuffExecutor(base)
		body, _ := json.Marshal(map[string]any{
			"model": "deepseek/deepseek-v4-flash", "stream": true,
			"messages": []any{map[string]any{"role": "user", "content": "hi"}},
		})
		res, err := exec.ExecuteStream(context.Background(), freebuffTestReq(ts, body))
		if err != nil {
			t.Fatalf("ExecuteStream error: %v", err)
		}
		// Drain the stream fully (as the v1 handler does). The lifecycle watcher
		// runs concurrently on a tee copy, so every payload chunk must still
		// arrive here.
		payloads := 0
		for chunk := range res.Chunks {
			if chunk.Err != nil {
				t.Fatalf("unexpected stream error: %v", chunk.Err)
			}
			// Count only the 5 content chunks (the trailing `data: [DONE]` line
			// is also a payload chunk but carries no content).
			if chunk.Payload != nil && !bytes.Contains(chunk.Payload, []byte("[DONE]")) {
				payloads++
			}
		}
		if payloads != 5 {
			t.Fatalf("handler received %d payload chunks, want 5 (watcher must not steal chunks)", payloads)
		}
		if got := freebuffWaitForValue(&finishStatus, 3*time.Second); got != "completed" {
			t.Fatalf("FINISH status=%q, want completed", got)
		}
	})

	t.Run("teardown-cancel-after-clean-completion", func(t *testing.T) {
		var finishStatus atomic.Value
		session, _, _ := defaultFreebuffHandlers()
		ts := newFreebuffTestServer()
		defer ts.Close()
		ts.session = session
		ts.runs = func(w http.ResponseWriter, r *http.Request) {
			var req struct {
				Action string `json:"action"`
				Status string `json:"status"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.Action == "FINISH" {
				finishStatus.Store(req.Status)
				writeJSON(w, http.StatusOK, map[string]any{"ok": true})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"runId": "run-1"})
		}
		ts.chat = func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			flusher, _ := w.(http.Flusher)
			fmt.Fprintln(w, `data: {"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"done"},"finish_reason":null}]}`)
			flusher.Flush()
			fmt.Fprintln(w, `data: [DONE]`)
			flusher.Flush()
		}

		base := NewBaseExecutor()
		exec := NewFreebuffExecutor(base)
		body, _ := json.Marshal(map[string]any{
			"model": "deepseek/deepseek-v4-flash", "stream": true,
			"messages": []any{map[string]any{"role": "user", "content": "hi"}},
		})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		res, err := exec.ExecuteStream(ctx, freebuffTestReq(ts, body))
		if err != nil {
			t.Fatalf("ExecuteStream error: %v", err)
		}
		// Drain the stream fully, exactly as the v1 handler does.
		for range res.Chunks {
		}
		// Simulate handler teardown: the request context is cancelled right
		// after the stream completed. The run already completed cleanly, so
		// FINISH must stay "completed" — a later teardown cancel must never
		// flip a successful run to "failed".
		cancel()
		if got := freebuffWaitForValue(&finishStatus, 3*time.Second); got != "completed" {
			t.Fatalf("FINISH status=%q, want completed after clean completion + teardown cancel", got)
		}
	})

	t.Run("mid-stream-error", func(t *testing.T) {
		var finishStatus atomic.Value
		session, _, _ := defaultFreebuffHandlers()
		ts := newFreebuffTestServer()
		defer ts.Close()
		ts.session = session
		ts.runs = func(w http.ResponseWriter, r *http.Request) {
			var req struct {
				Action string `json:"action"`
				Status string `json:"status"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.Action == "FINISH" {
				finishStatus.Store(req.Status)
				writeJSON(w, http.StatusOK, map[string]any{"ok": true})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"runId": "run-1"})
		}
		ts.chat = func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			flusher, _ := w.(http.Flusher)
			fmt.Fprintln(w, `data: {"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"partial"},"finish_reason":null}]}`)
			flusher.Flush()
			// Keep the connection open but send nothing further: the client
			// cancels mid-stream below, which the base executor turns into an
			// error chunk. Block until the client disconnects (read returns),
			// then return so httptest shutdown never hangs.
			_, _ = io.Copy(io.Discard, r.Body)
		}

		base := NewBaseExecutor()
		exec := NewFreebuffExecutor(base)
		body, _ := json.Marshal(map[string]any{
			"model": "deepseek/deepseek-v4-flash", "stream": true,
			"messages": []any{map[string]any{"role": "user", "content": "hi"}},
		})
		ctx, cancel := context.WithCancel(context.Background())
		res, err := exec.ExecuteStream(ctx, freebuffTestReq(ts, body))
		if err != nil {
			t.Fatalf("ExecuteStream error: %v", err)
		}
		// Cancel BEFORE draining, like a client that disconnects as soon as the
		// response headers arrive. The base executor turns this into an error
		// chunk, so the watcher must mark the run "failed".
		cancel()
		for range res.Chunks {
		}
		if got := freebuffWaitForValue(&finishStatus, 3*time.Second); got != "failed" {
			t.Fatalf("FINISH status=%q, want failed", got)
		}
	})
}

// freebuffWaitForValue polls an atomic.Value until it holds a non-nil string
// or the timeout elapses.
func freebuffWaitForValue(v *atomic.Value, timeout time.Duration) string {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if s := v.Load(); s != nil {
			return s.(string)
		}
		time.Sleep(10 * time.Millisecond)
	}
	return ""
}

// TestFreebuffFinalizeStreamRun_CleanCloseCompleted verifies the core contract
// of the streaming FINISH lifecycle at the unit level: a chunk source that
// closes cleanly (no error chunk) must produce markFinished("completed") with
// every chunk delivered to the handler channel — and a context cancelled AFTER
// the stream is fully drained (handler teardown) must not change that outcome.
// This directly guards the teardown-cancel race: the watcher must never consult
// ctx.Err() after the channel closes, or a successful run would be misreported
// as "failed" when the request context is cancelled moments later.
func TestFreebuffFinalizeStreamRun_CleanCloseCompleted(t *testing.T) {
	src := make(chan StreamChunk, 4)
	for i := 0; i < 3; i++ {
		src <- StreamChunk{Payload: []byte(fmt.Sprintf("data: c%d\n\n", i))}
	}
	close(src) // clean close, no error chunk

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var status atomic.Value
	result := freebuffFinalizeStreamRun(ctx, &StreamResult{Chunks: src}, func(s string) {
		status.Store(s)
	})

	// Drain fully, exactly as the v1 handler does. The fan-out has closed
	// handlerCh by the time this loop returns, so the watcher has already
	// finalized the run before the teardown cancel below.
	payloads := 0
	for chunk := range result.Chunks {
		if chunk.Err == nil && chunk.Payload != nil {
			payloads++
		}
	}
	if payloads != 3 {
		t.Fatalf("handler received %d payload chunks, want 3", payloads)
	}

	// Simulate handler teardown cancelling the request context after the
	// stream completed. FINISH must stay "completed".
	cancel()
	if got := freebuffWaitForValue(&status, 2*time.Second); got != "completed" {
		t.Fatalf("FINISH status=%q, want completed (teardown cancel must not flip it)", got)
	}
}

func TestFreebuffExecutor_StaleSessionReclaim(t *testing.T) {
	var sessionCalls int32
	var runCalls int32
	var finishCalls int32
	var chatCalls int32

	session, _, chat := defaultFreebuffHandlers()
	ts := newFreebuffTestServer()
	defer ts.Close()

	ts.session = func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&sessionCalls, 1)
		session(w, r)
	}
	ts.runs = func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Action string `json:"action"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Action == "FINISH" {
			atomic.AddInt32(&finishCalls, 1)
			writeJSON(w, http.StatusOK, map[string]any{"ok": true})
			return
		}
		atomic.AddInt32(&runCalls, 1)
		writeJSON(w, http.StatusOK, map[string]any{"runId": fmt.Sprintf("run-%d", atomic.LoadInt32(&runCalls))})
	}
	ts.chat = func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&chatCalls, 1)
		if n == 1 {
			// First chat: stale session (428 waiting_room_required).
			writeJSON(w, 428, map[string]any{"status": "waiting_room_required", "message": "need session"})
			return
		}
		chat(w, r)
	}

	base := NewBaseExecutor()
	exec := NewFreebuffExecutor(base)
	body, _ := json.Marshal(map[string]any{
		"model":    "deepseek/deepseek-v4-flash",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	})
	res, err := exec.Execute(context.Background(), freebuffTestReq(ts, body))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if res.StatusCode != 200 {
		t.Fatalf("status=%d, want 200", res.StatusCode)
	}
	// Session claimed twice (initial + force re-claim), run started twice, old
	// run FINISH'd cancelled, new run FINISH'd completed.
	if got := atomic.LoadInt32(&sessionCalls); got != 2 {
		t.Errorf("session calls=%d, want 2", got)
	}
	if got := atomic.LoadInt32(&runCalls); got != 2 {
		t.Errorf("run starts=%d, want 2", got)
	}
	if got := atomic.LoadInt32(&chatCalls); got != 2 {
		t.Errorf("chat calls=%d, want 2", got)
	}
	// finishRun is fire-and-forget — wait for both async FINISH calls.
	deadline := time.Now().Add(3 * time.Second)
	for atomic.LoadInt32(&finishCalls) < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := atomic.LoadInt32(&finishCalls); got != 2 {
		t.Errorf("FINISH calls=%d, want 2", got)
	}
}

func TestFreebuffExecutor_ModelLocked409(t *testing.T) {
	// Only count non-FINISH upstream calls (session, run START, chat) — the
	// async finishRun goroutine makes FINISH counting timing-dependent.
	var upstreamCalls int32
	session, _, _ := defaultFreebuffHandlers()
	ts := newFreebuffTestServer()
	defer ts.Close()

	ts.session = func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&upstreamCalls, 1)
		session(w, r)
	}
	ts.runs = func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Action string `json:"action"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Action == "FINISH" {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true})
			return
		}
		atomic.AddInt32(&upstreamCalls, 1)
		writeJSON(w, http.StatusOK, map[string]any{"runId": "run-1"})
	}
	ts.chat = func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&upstreamCalls, 1)
		writeJSON(w, http.StatusConflict, map[string]any{
			"status": "model_locked", "currentModel": "deepseek/deepseek-v4-pro",
		})
	}

	base := NewBaseExecutor()
	exec := NewFreebuffExecutor(base)
	body, _ := json.Marshal(map[string]any{
		"model":    "deepseek/deepseek-v4-flash",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	})
	req := freebuffTestReq(ts, body)

	_, err := exec.Execute(context.Background(), req)
	if err == nil {
		t.Fatal("expected error")
	}
	upErr, ok := err.(*UpstreamError)
	if !ok {
		t.Fatalf("expected UpstreamError, got %T", err)
	}
	if upErr.StatusCode != http.StatusConflict {
		t.Errorf("status=%d, want 409", upErr.StatusCode)
	}
	if !strings.Contains(string(upErr.Body), "locked") {
		t.Errorf("body should mention lock: %s", upErr.Body)
	}
	// Wait for the async FINISH to land before snapshotting the counter.
	time.Sleep(50 * time.Millisecond)
	firstCalls := atomic.LoadInt32(&upstreamCalls)

	// Second call on the same (token, model) pair must fail fast on the cooldown
	// without any upstream calls.
	_, err = exec.Execute(context.Background(), req)
	if err == nil {
		t.Fatal("expected cooldown fail-fast error")
	}
	if got := atomic.LoadInt32(&upstreamCalls); got != firstCalls {
		t.Errorf("upstream calls increased after cooldown: %d -> %d", firstCalls, got)
	}
}

func TestFreebuffExecutor_LimitedIP409(t *testing.T) {
	session, runs, _ := defaultFreebuffHandlers()
	ts := newFreebuffTestServer()
	defer ts.Close()
	ts.session = session
	ts.runs = runs
	ts.chat = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusConflict, map[string]any{
			"status":  "session_model_mismatch",
			"message": "limited tier: only deepseek-v4-flash allowed",
		})
	}

	base := NewBaseExecutor()
	exec := NewFreebuffExecutor(base)
	body, _ := json.Marshal(map[string]any{
		"model":    "mimo/mimo-v2.5",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	})
	_, err := exec.Execute(context.Background(), freebuffTestReq(ts, body))
	if err == nil {
		t.Fatal("expected error")
	}
	upErr, ok := err.(*UpstreamError)
	if !ok {
		t.Fatalf("expected UpstreamError, got %T", err)
	}
	if upErr.StatusCode != http.StatusConflict {
		t.Errorf("status=%d, want 409", upErr.StatusCode)
	}
	if !strings.Contains(string(upErr.Body), "limited-mode IP") {
		t.Errorf("body should mention limited IP: %s", upErr.Body)
	}
}

// freebuffForwardProxy is a minimal HTTP forward proxy used by the pool-failover
// test. Go's HTTP client sends absolute-form request URIs to HTTP proxies, so the
// handler reads the target from r.URL, forwards the request to the backend
// ORIGIN (scheme+host only — the incoming path is appended), and tags the
// forwarded request with the pool's marker header so the mock backend can
// distinguish which egress pool served it.
func freebuffForwardProxy(t *testing.T, origin, marker string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			http.Error(w, "CONNECT not supported", http.StatusBadRequest)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		fwd, err := http.NewRequest(r.Method, origin+r.URL.Path, bytes.NewReader(body))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.URL.RawQuery != "" {
			fwd.URL.RawQuery = r.URL.RawQuery
		}
		fwd.Header = r.Header.Clone()
		fwd.Header.Set("X-Pool", marker)
		resp, err := http.DefaultClient.Do(fwd)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		out, err := io.ReadAll(resp.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		for k, vv := range resp.Header {
			for _, v := range vv {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(out)
	}))
}

// TestFreebuffExecutor_LimitedIPPoolFailover verifies pool-scoped failover:
// when the first proxy candidate is refused with a limited-tier IP gate, the
// executor marks that pool unfit and retries the SAME request through the next
// candidate, which succeeds. A single session claim and run registration serve
// both attempts (they are account/model-scoped, not egress-scoped).
func TestFreebuffExecutor_LimitedIPPoolFailover(t *testing.T) {
	var chatCalls int32
	session, runs, _ := defaultFreebuffHandlers()
	ts := newFreebuffTestServer()
	defer ts.Close()
	ts.session = session
	ts.runs = runs
	ts.chat = func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&chatCalls, 1)
		// Requests arriving via pool-a are refused; pool-b is healthy.
		if r.Header.Get("X-Pool") == "a" {
			writeJSON(w, http.StatusConflict, map[string]any{
				"status":  "session_model_mismatch",
				"message": "limited tier: only deepseek-v4-flash allowed",
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id": "chat-2", "object": "chat.completion",
			"choices": []any{map[string]any{
				"index": 0, "message": map[string]any{"role": "assistant", "content": "ok"},
				"finish_reason": "stop",
			}},
		})
	}

	// Real forward proxies standing in for two different egress pools. They
	// forward to the backend ORIGIN only (the incoming request path is appended),
	// so session/run/chat paths all reach the mock backend intact.
	proxyA := freebuffForwardProxy(t, ts.ts.URL, "a")
	defer proxyA.Close()
	proxyB := freebuffForwardProxy(t, ts.ts.URL, "b")
	defer proxyB.Close()

	base := NewBaseExecutor()
	exec := NewFreebuffExecutor(base)
	body, _ := json.Marshal(map[string]any{
		"model":    "mimo/mimo-v2.5",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	})

	// Two distinct proxy candidates: pool-a refuses this model (limited IP),
	// pool-b is healthy. Distinct ProxyURLs keep the cooldown keys separate.
	cands := []ProxyConfig{
		{Enabled: true, ProxyPoolID: "pool-a", ProxyURL: proxyA.URL},
		{Enabled: true, ProxyPoolID: "pool-b", ProxyURL: proxyB.URL},
	}
	ctx := ContextWithProxy(context.Background(), cands[0])
	ctx = ContextWithProxyCandidates(ctx, cands)

	res, err := exec.Execute(ctx, freebuffTestReq(ts, body))
	if err != nil {
		t.Fatalf("Execute error (should fail over to pool-b): %v", err)
	}
	if res.StatusCode != 200 {
		t.Fatalf("status=%d, want 200", res.StatusCode)
	}
	if got := atomic.LoadInt32(&chatCalls); got != 2 {
		t.Fatalf("chat calls=%d, want 2 (pool-a refused, pool-b served)", got)
	}

	// The refused pool must now be in a cooldown: a follow-up request with ONLY
	// pool-a as a candidate must fail fast with a 409 without any upstream chat.
	poolAOnly := []ProxyConfig{{Enabled: true, ProxyPoolID: "pool-a", ProxyURL: proxyA.URL}}
	ctxA := ContextWithProxy(context.Background(), poolAOnly[0])
	ctxA = ContextWithProxyCandidates(ctxA, poolAOnly)
	atomic.StoreInt32(&chatCalls, 0)
	_, err = exec.Execute(ctxA, freebuffTestReq(ts, body))
	if err == nil {
		t.Fatal("expected 409 for pool still in limited-IP cooldown")
	}
	upErr, ok := err.(*UpstreamError)
	if !ok {
		t.Fatalf("expected UpstreamError, got %T", err)
	}
	if upErr.StatusCode != http.StatusConflict {
		t.Errorf("status=%d, want 409", upErr.StatusCode)
	}
	if got := atomic.LoadInt32(&chatCalls); got != 0 {
		t.Errorf("pool-a was hit %d times during cooldown, want 0", got)
	}
}

// TestFreebuffExecutor_NoProxyDirectAllowed verifies that freebuff works
// without a proxy pool configured. When no proxy pool is assigned, the
// resolver returns no candidates and the executor uses a direct connection.
func TestFreebuffExecutor_NoProxyDirectAllowed(t *testing.T) {
	var upstreamCalls int32
	session, _, chat := defaultFreebuffHandlers()
	ts := newFreebuffTestServer()
	defer ts.Close()
	ts.session = func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&upstreamCalls, 1)
		session(w, r)
	}
	ts.runs = func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Action string `json:"action"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Action == "FINISH" {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true})
			return
		}
		atomic.AddInt32(&upstreamCalls, 1)
		writeJSON(w, http.StatusOK, map[string]any{"runId": "run-1"})
	}
	ts.chat = func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&upstreamCalls, 1)
		chat(w, r)
	}

	base := NewBaseExecutor()
	exec := NewFreebuffExecutor(base)
	body, _ := json.Marshal(map[string]any{
		"model":    "deepseek/deepseek-v4-flash",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	})

	// No proxy candidates — simulates a connection without a proxy pool.
	// The executor should fall back to a direct connection.
	_, err := exec.Execute(context.Background(), freebuffTestReq(ts, body))
	if err != nil {
		t.Fatalf("expected direct connection to succeed, got: %v", err)
	}
	if got := atomic.LoadInt32(&upstreamCalls); got < 3 {
		t.Fatalf("upstream calls=%d, want >= 3 (session + run + chat)", got)
	}
}

// TestFreebuffExecutor_DeadPoolMarkerSkippedAfterRealPool verifies the
// direct-fallback marker is skipped even when it follows a real pool in the
// candidate list: pool-a refuses the model (limited IP, pool-scoped cooldown),
// and the trailing strict direct marker must NOT receive the failover attempt.
// The request fails with the pool-cooldown message and every upstream call was
// routed through the real proxy — none direct.
func TestFreebuffExecutor_DeadPoolMarkerSkippedAfterRealPool(t *testing.T) {
	var chatViaPoolA int32
	var chatDirect int32
	session, runs, _ := defaultFreebuffHandlers()
	ts := newFreebuffTestServer()
	defer ts.Close()
	ts.session = session
	ts.runs = runs
	ts.chat = func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Pool") == "a" {
			atomic.AddInt32(&chatViaPoolA, 1)
			writeJSON(w, http.StatusConflict, map[string]any{
				"status":  "session_model_mismatch",
				"message": "limited tier: only deepseek-v4-flash allowed",
			})
			return
		}
		atomic.AddInt32(&chatDirect, 1)
		writeJSON(w, http.StatusOK, map[string]any{
			"id": "chat-1", "object": "chat.completion",
			"choices": []any{map[string]any{
				"index": 0, "message": map[string]any{"role": "assistant", "content": "leaked"},
				"finish_reason": "stop",
			}},
		})
	}

	proxyA := freebuffForwardProxy(t, ts.ts.URL, "a")
	defer proxyA.Close()

	base := NewBaseExecutor()
	exec := NewFreebuffExecutor(base)
	body, _ := json.Marshal(map[string]any{
		"model":    "mimo/mimo-v2.5",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	})

	// Real pool first (refuses with a limited-IP gate), then the handler's
	// force-strict direct-fallback marker.
	cands := []ProxyConfig{
		{Enabled: true, ProxyPoolID: "pool-a", ProxyURL: proxyA.URL},
		{StrictProxy: true}, // direct-fallback marker, force-strict
	}
	ctx := ContextWithProxy(context.Background(), cands[0])
	ctx = ContextWithProxyCandidates(ctx, cands)

	_, err := exec.Execute(ctx, freebuffTestReq(ts, body))
	if err == nil {
		t.Fatal("expected 409 after pool-a limited-IP refusal with no usable failover")
	}
	upErr, ok := err.(*UpstreamError)
	if !ok {
		t.Fatalf("expected UpstreamError, got %T", err)
	}
	if upErr.StatusCode != http.StatusConflict {
		t.Errorf("status=%d, want 409", upErr.StatusCode)
	}
	if !strings.Contains(string(upErr.Body), "retry with a full-access proxy") {
		t.Errorf("body should mention the pool cooldown: %s", upErr.Body)
	}
	if got := atomic.LoadInt32(&chatViaPoolA); got != 1 {
		t.Errorf("chat via pool-a=%d, want 1", got)
	}
	if got := atomic.LoadInt32(&chatDirect); got != 0 {
		t.Fatalf("chat went DIRECT %d times — failover leaked to the real IP", got)
	}
}

func TestFreebuffExecutor_429RetryThenSuccess(t *testing.T) {
	var chatCalls int32
	var chatBodies [][]byte

	session, runs, chat := defaultFreebuffHandlers()
	ts := newFreebuffTestServer()
	defer ts.Close()
	ts.session = session
	ts.runs = runs
	ts.chat = func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		chatBodies = append(chatBodies, b)
		if atomic.AddInt32(&chatCalls, 1) == 1 {
			writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": map[string]any{"message": "rate limit"}})
			return
		}
		chat(w, r)
	}

	base := NewBaseExecutor()
	exec := NewFreebuffExecutor(base)
	body, _ := json.Marshal(map[string]any{
		"model":    "deepseek/deepseek-v4-flash",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	})
	start := time.Now()
	res, err := exec.Execute(context.Background(), freebuffTestReq(ts, body))
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if res.StatusCode != 200 {
		t.Fatalf("status=%d, want 200", res.StatusCode)
	}
	if got := atomic.LoadInt32(&chatCalls); got != 2 {
		t.Fatalf("chat calls=%d, want 2", got)
	}
	if elapsed < 1900*time.Millisecond {
		t.Fatalf("retry delay too short: %v", elapsed)
	}
	// Same run_id reused across retries.
	r1 := gjson.GetBytes(chatBodies[0], "codebuff_metadata.run_id").String()
	r2 := gjson.GetBytes(chatBodies[1], "codebuff_metadata.run_id").String()
	if r1 == "" || r1 != r2 {
		t.Errorf("run_id should be stable across retries: %q vs %q", r1, r2)
	}
}

// TestFreebuffExecutor_429ExhaustedFinishesFailed guards the exhausted-retry
// status mapping (NOT the deferred safety net — that is covered by the cancel
// test below): a NON-streaming request which stays rate-limited (429) through
// every retry ends with FINISH "failed" — never a dangling run and never a
// premature "completed". The run is registered before the chat loop, so it must
// be accounted for even when the upstream never recovers.
func TestFreebuffExecutor_429ExhaustedFinishesFailed(t *testing.T) {
	var finishStatus atomic.Value
	var chatCalls int32
	session, _, _ := defaultFreebuffHandlers()
	ts := newFreebuffTestServer()
	defer ts.Close()
	ts.session = session
	ts.runs = func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Action string `json:"action"`
			Status string `json:"status"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Action == "FINISH" {
			finishStatus.Store(req.Status)
			writeJSON(w, http.StatusOK, map[string]any{"ok": true})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"runId": "run-1"})
	}
	ts.chat = func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&chatCalls, 1)
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": map[string]any{"message": "rate limit"}})
	}

	base := NewBaseExecutor()
	exec := NewFreebuffExecutor(base)
	body, _ := json.Marshal(map[string]any{
		"model":    "deepseek/deepseek-v4-flash",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	})
	_, err := exec.Execute(context.Background(), freebuffTestReq(ts, body))
	if err == nil {
		t.Fatal("expected 429 error after retries exhausted")
	}
	upErr, ok := err.(*UpstreamError)
	if !ok {
		t.Fatalf("expected UpstreamError, got %T", err)
	}
	if upErr.StatusCode != http.StatusTooManyRequests {
		t.Errorf("status=%d, want 429", upErr.StatusCode)
	}
	// Initial attempt + 2 retries (429 config: attempts=2).
	if got := atomic.LoadInt32(&chatCalls); got != 3 {
		t.Errorf("chat calls=%d, want 3", got)
	}
	// finishRun is fire-and-forget — wait for the async FINISH callback.
	if got := freebuffWaitForValue(&finishStatus, 3*time.Second); got != "failed" {
		t.Errorf("FINISH status=%q, want failed", got)
	}
}

// TestFreebuffExecutor_429CancelDuringBackoffFinishesFailed is the regression
// for the deferred safety net (mirrors the reference's try/finally): when the
// caller cancels the request context DURING the rate-limit retry backoff sleep,
// the run must still be FINISH'd "failed" — never left dangling on the upstream
// (the server only sweeps stale runs lazily).
func TestFreebuffExecutor_429CancelDuringBackoffFinishesFailed(t *testing.T) {
	var finishStatus atomic.Value
	var chatCalls int32
	firstChat := make(chan struct{})
	session, _, _ := defaultFreebuffHandlers()
	ts := newFreebuffTestServer()
	defer ts.Close()
	ts.session = session
	ts.runs = func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Action string `json:"action"`
			Status string `json:"status"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Action == "FINISH" {
			finishStatus.Store(req.Status)
			writeJSON(w, http.StatusOK, map[string]any{"ok": true})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"runId": "run-1"})
	}
	ts.chat = func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&chatCalls, 1)
		if n == 1 {
			close(firstChat) // signal: first 429 served, executor is about to sleep
		}
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": map[string]any{"message": "rate limit"}})
	}

	base := NewBaseExecutor()
	exec := NewFreebuffExecutor(base)
	body, _ := json.Marshal(map[string]any{
		"model":    "deepseek/deepseek-v4-flash",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Cancel shortly after the first 429 lands — well inside the 2s retry
	// backoff sleep, so the executor is mid-sleep when the context dies.
	go func() {
		<-firstChat
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	_, err := exec.Execute(ctx, freebuffTestReq(ts, body))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if got := atomic.LoadInt32(&chatCalls); got != 1 {
		t.Errorf("chat calls=%d, want 1 (cancelled during first backoff)", got)
	}
	// The deferred safety net must still FINISH the run "failed" even though
	// the request context is already gone.
	if got := freebuffWaitForValue(&finishStatus, 3*time.Second); got != "failed" {
		t.Errorf("FINISH status=%q, want failed (run must not be left dangling)", got)
	}
}

func TestFreebuffExecutor_401DropsSession(t *testing.T) {
	var sessionCalls int32
	var chatCalls int32

	session, runs, chat := defaultFreebuffHandlers()
	ts := newFreebuffTestServer()
	defer ts.Close()
	ts.session = func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&sessionCalls, 1)
		session(w, r)
	}
	ts.runs = runs
	ts.chat = func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&chatCalls, 1)
		// Only the dead token (fb-at-123) returns 401; the refreshed token succeeds.
		if r.Header.Get("Authorization") == "Bearer fb-at-123" {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": map[string]any{"message": "token dead"}})
			return
		}
		chat(w, r)
	}

	base := NewBaseExecutor()
	exec := NewFreebuffExecutor(base)
	body, _ := json.Marshal(map[string]any{
		"model":    "deepseek/deepseek-v4-flash",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	})
	req := freebuffTestReq(ts, body)

	// First call: 401 kills the cached session.
	_, err := exec.Execute(context.Background(), req)
	if err == nil {
		t.Fatal("expected 401 error")
	}
	if got := atomic.LoadInt32(&chatCalls); got != 1 {
		t.Fatalf("chat calls=%d, want 1", got)
	}
	// Second call with a fresh token: session cache was dropped for the old
	// token, so the session claim runs again (new token -> new key).
	req.AccessToken = "fb-at-456"
	_, err = exec.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("second Execute error: %v", err)
	}
	if got := atomic.LoadInt32(&sessionCalls); got != 2 {
		t.Errorf("session calls=%d, want 2 (reclaim after token change)", got)
	}
}

func TestFreebuffExecutor_SessionClaim401(t *testing.T) {
	_, runs, _ := defaultFreebuffHandlers()
	ts := newFreebuffTestServer()
	defer ts.Close()
	ts.session = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": map[string]any{"message": "bad token"}})
	}
	ts.runs = runs

	base := NewBaseExecutor()
	exec := NewFreebuffExecutor(base)
	body, _ := json.Marshal(map[string]any{"model": "deepseek/deepseek-v4-flash", "messages": []any{map[string]any{"role": "user", "content": "hi"}}})
	_, err := exec.Execute(context.Background(), freebuffTestReq(ts, body))
	if err == nil {
		t.Fatal("expected error")
	}
	upErr, ok := err.(*UpstreamError)
	if !ok || upErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 UpstreamError, got %T %v", err, err)
	}
}

func TestFreebuffExecutor_SystemMarker(t *testing.T) {
	// No system message -> marker inserted first.
	body, _ := json.Marshal(map[string]any{"messages": []any{map[string]any{"role": "user", "content": "hi"}}})
	out := ensureFreebuffSystemMarker(body)
	if got := gjson.GetBytes(out, "messages.0.role").String(); got != "system" {
		t.Errorf("role=%q, want system", got)
	}
	if !strings.HasPrefix(gjson.GetBytes(out, "messages.0.content").String(), freebuffSystemMarker) {
		t.Errorf("marker not prepended")
	}
	// Existing system message -> marker prepended before its content.
	body, _ = json.Marshal(map[string]any{"messages": []any{map[string]any{"role": "system", "content": "You are helpful."}}})
	out = ensureFreebuffSystemMarker(body)
	if got := gjson.GetBytes(out, "messages.0.content").String(); !strings.HasPrefix(got, freebuffSystemMarker+"\n\nYou are helpful.") {
		t.Errorf("marker should be prepended to system content: %q", got)
	}
	// Already marked -> unchanged (idempotent).
	body, _ = json.Marshal(map[string]any{"messages": []any{map[string]any{"role": "system", "content": freebuffSystemMarker + "\n\ncustom"}}})
	out = ensureFreebuffSystemMarker(body)
	if got := gjson.GetBytes(out, "messages.0.content").String(); got != freebuffSystemMarker+"\n\ncustom" {
		t.Errorf("already-marked body should be unchanged, got %q", got)
	}
	// Alternate canonical opening -> unchanged.
	body, _ = json.Marshal(map[string]any{"messages": []any{map[string]any{"role": "system", "content": "You are Buffy, the Freebuff Cloud project planner."}}})
	out = ensureFreebuffSystemMarker(body)
	if got := gjson.GetBytes(out, "messages.0.content").String(); got != "You are Buffy, the Freebuff Cloud project planner." {
		t.Errorf("alternate opening should be unchanged, got %q", got)
	}
	// Non-string system content (multimodal parts array) must NOT be clobbered
	// — a new marker message is inserted instead.
	body, _ = json.Marshal(map[string]any{
		"messages": []any{
			map[string]any{
				"role":    "system",
				"content": []any{map[string]any{"type": "text", "text": "keep me"}},
			},
		},
	})
	out = ensureFreebuffSystemMarker(body)
	if got := gjson.GetBytes(out, "messages.0.content").String(); !strings.HasPrefix(got, freebuffSystemMarker) {
		t.Errorf("marker message should be inserted first, got %q", got)
	}
	if got := gjson.GetBytes(out, "messages.1.content.0.text").String(); got != "keep me" {
		t.Errorf("original multimodal content was destroyed: %q", got)
	}
}

func TestFreebuffExecutor_RootAgentByModel(t *testing.T) {
	cases := map[string]string{
		"deepseek/deepseek-v4-flash": "base2-free-deepseek-flash",
		"deepseek/deepseek-v4-pro":   "base2-free-deepseek",
		"mimo/mimo-v2.5":             "base2-free-mimo",
		"minimax/minimax-m3":         "base2-free-minimax-m3",
		"openai/gpt-5.6-luna":        "base2-free-luna",
	}
	for model, want := range cases {
		if got := freebuffRootAgentByModel[model]; got != want {
			t.Errorf("root agent for %s=%q, want %q", model, got, want)
		}
	}
}

func TestFreebuffExecutor_GateClassification(t *testing.T) {
	cases := []struct {
		code    string
		message string
		want    string
	}{
		{"model_locked", "", "model_locked"},
		{"session_superseded", "", "superseded"},
		{"session_model_mismatch", "limited tier: only flash allowed", "limited_ip"},
		{"session_model_mismatch", "session bound to gpt-5", "model_locked"},
		{"session_expired", "", "stale"},
		{"waiting_room_required", "", "stale"},
		{"session_is_active", "", "stale"},
		{"", "", "stale"},
	}
	for _, tc := range cases {
		got := classifySessionGate(tc.code, tc.message, "")
		if got.kind != tc.want {
			t.Errorf("classifySessionGate(%q, %q).kind=%q, want %q", tc.code, tc.message, got.kind, tc.want)
		}
	}
}

func TestFreebuffExecutor_GateFromText(t *testing.T) {
	gate := sessionGateFromText([]byte(`{"status":"model_locked","currentModel":"grok-4.5"}`))
	if gate.kind != "model_locked" || gate.currentModel != "grok-4.5" {
		t.Errorf("gate=%+v, want model_locked/grok-4.5", gate)
	}
	gate = sessionGateFromText([]byte(`{"error_type":"session_expired"}`))
	if gate.kind != "stale" {
		t.Errorf("gate=%+v, want stale", gate)
	}
	gate = sessionGateFromText([]byte(`not json`))
	if gate.kind != "stale" {
		t.Errorf("gate=%+v, want stale", gate)
	}
}

func TestFreebuffExecutor_CooldownSweep(t *testing.T) {
	base := NewBaseExecutor()
	exec := NewFreebuffExecutor(base)

	// Expired cooldown is pruned on read.
	exec.setCooldown(exec.modelLockCooldowns, "t::m", time.Now().Add(-time.Minute))
	if got := exec.getCooldown(exec.modelLockCooldowns, "t::m"); got != nil {
		t.Errorf("expired cooldown should be pruned, got %v", got)
	}
	// Active cooldown survives and is pruned only after expiry.
	exec.setCooldown(exec.modelLockCooldowns, "t::m", time.Now().Add(time.Hour))
	if got := exec.getCooldown(exec.modelLockCooldowns, "t::m"); got == nil {
		t.Errorf("active cooldown should be returned")
	}
	exec.setCooldown(exec.modelLockCooldowns, "t::m", time.Now().Add(-time.Minute))
	exec.setCooldown(exec.modelLockCooldowns, "t::n", time.Now().Add(time.Hour))
	if got := exec.getCooldown(exec.modelLockCooldowns, "t::m"); got != nil {
		t.Errorf("stale key should be swept on write, got %v", got)
	}
}

func TestFreebuffExecutor_ConcurrentSessionClaims(t *testing.T) {
	// Two concurrent requests for the same (token, model) must share ONE session
	// claim — the second waiter must wake with the same result instead of
	// hanging forever on a once-delivered channel.
	var sessionCalls int32
	session, runs, chat := defaultFreebuffHandlers()
	ts := newFreebuffTestServer()
	defer ts.Close()
	ts.session = func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&sessionCalls, 1)
		time.Sleep(30 * time.Millisecond) // widen the overlap window
		session(w, r)
	}
	ts.runs = runs
	ts.chat = chat

	base := NewBaseExecutor()
	exec := NewFreebuffExecutor(base)
	body, _ := json.Marshal(map[string]any{
		"model":    "deepseek/deepseek-v4-flash",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	})

	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := exec.Execute(context.Background(), freebuffTestReq(ts, body))
			if err != nil {
				t.Errorf("Execute error: %v", err)
				return
			}
			if res.StatusCode != 200 {
				t.Errorf("status=%d, want 200", res.StatusCode)
			}
		}()
	}
	wg.Wait()
	// Exactly one session claim for the concurrent burst (subsequent requests
	// hit the cache), plus zero re-claims.
	if got := atomic.LoadInt32(&sessionCalls); got != 1 {
		t.Errorf("session claims=%d, want 1 (deduplicated)", got)
	}
}

func TestFreebuffExecutor_MissingToken(t *testing.T) {
	base := NewBaseExecutor()
	exec := NewFreebuffExecutor(base)
	body, _ := json.Marshal(map[string]any{"model": "deepseek/deepseek-v4-flash", "messages": []any{map[string]any{"role": "user", "content": "hi"}}})
	req := &Request{Provider: "freebuff", Model: "freebuff/deepseek/deepseek-v4-flash", Body: body}
	_, err := exec.Execute(context.Background(), req)
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("expected missing-token error, got %v", err)
	}
}
