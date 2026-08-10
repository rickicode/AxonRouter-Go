package v1

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	"github.com/rickicode/AxonRouter-Go/internal/db"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/rickicode/AxonRouter-Go/internal/providercfg"
	"github.com/rickicode/AxonRouter-Go/internal/usage"

	// Ensure translator registrations (including Interactions) are loaded.
	_ "github.com/rickicode/AxonRouter-Go/internal/translator"

	"github.com/tidwall/gjson"
)

func setupInteractionsTest(t *testing.T) (*Handler, func()) {
	logging.Init("text")
	h := newTestHandler(t)
	wq := db.NewWriteQueue(h.db)
	tracker := usage.NewTracker(h.db)
	tracker.SetWriteQueue(wq)
	h.tracker = tracker

	if _, err := h.db.Exec(`INSERT OR IGNORE INTO connections (id, provider_type_id, name, auth_type, status, is_active, created_at, updated_at, provider_specific_data)
		VALUES ('gint-conn1','gemini-interactions','c1','none','ready',1,0,0,'')`); err != nil {
		t.Fatalf("seed connection: %v", err)
	}
	h.store.SeedConnection("gint-conn1", "gemini-interactions", "ready", 0)
	h.elig.RecomputeAll()

	executor.RegisterDefaults()

	return h, func() {
		executor.RegisterDefaults()
	}
}

func TestChatCompletions_InteractionsProvider_NonStream(t *testing.T) {
	h, cleanup := setupInteractionsTest(t)
	defer cleanup()

	fe := &fakeExecutor{
		responses: []struct {
			resp *executor.Response
			err  error
		}{
			{
				resp: &executor.Response{
					StatusCode: http.StatusOK,
					Body: []byte(`{
						"id": "interaction-123",
						"model": "gemini-2.5-flash",
						"steps": [
							{"type": "model_output", "content": "Hello from Interactions"}
						],
						"usage": {"input_tokens": 10, "output_tokens": 5, "total_tokens": 15}
					}`),
				},
			},
		},
	}
	executor.GetRegistry().Register("gemini-interactions", executor.FormatInteractions, fe)

	body := []byte(`{"model":"gemini-interactions/gemini-2.5-flash","messages":[{"role":"user","content":"hi"}]}`)
	rec, c := jsonRequestWithAllowedModels(t, http.MethodPost, "/v1/chat/completions", body, nil)
	h.ChatCompletions(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"content":"Hello from Interactions"`)) {
		t.Fatalf("expected translated content, got %s", rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"object":"chat.completion"`)) {
		t.Fatalf("expected chat.completion object, got %s", rec.Body.String())
	}
	if gjson.GetBytes(rec.Body.Bytes(), "model").String() != "gemini-2.5-flash" {
		t.Fatalf("expected model gemini-2.5-flash, got %s", rec.Body.String())
	}
	if fe.callCount != 1 {
		t.Errorf("expected 1 upstream call, got %d", fe.callCount)
	}
}

func TestChatCompletions_InteractionsProvider_Stream(t *testing.T) {
	h, cleanup := setupInteractionsTest(t)
	defer cleanup()
	if err := h.providerCfg.Save("gemini-interactions", providercfg.ProviderSettings{
		RoutingMode: providercfg.RoundRobin,
		HoldbackMs:  intptr(10),
	}); err != nil {
		t.Fatalf("save holdback settings: %v", err)
	}

	chunks := make(chan executor.StreamChunk, 4)
	chunks <- executor.StreamChunk{Payload: []byte(`data: {"event_type":"step.delta","delta":{"text":"Hello"}}` + "\n\n")}
	chunks <- executor.StreamChunk{Payload: []byte(`data: {"event_type":"step.delta","delta":{"text":" world"}}` + "\n\n")}
	chunks <- executor.StreamChunk{Payload: []byte(`data: {"event_type":"step.stop"}` + "\n\n")}
	chunks <- executor.StreamChunk{Payload: []byte("data: [DONE]\n\n")}
	close(chunks)

	fe := &fakeExecutor{
		streamResults: []struct {
			result *executor.StreamResult
			err    error
		}{
			{result: &executor.StreamResult{Chunks: chunks, StatusCode: http.StatusOK}},
		},
	}
	executor.GetRegistry().Register("gemini-interactions", executor.FormatInteractions, fe)

	body := []byte(`{"model":"gemini-interactions/gemini-2.5-flash","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	rec, c := jsonRequestWithAllowedModels(t, http.MethodPost, "/v1/chat/completions", body, nil)
	h.ChatCompletions(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("expected text/event-stream, got %q", rec.Header().Get("Content-Type"))
	}
	bodyStr := rec.Body.String()
	if !strings.Contains(bodyStr, `"content":"Hello"`) {
		t.Fatalf("expected 'Hello' SSE chunk, got %s", bodyStr)
	}
	if !strings.Contains(bodyStr, `"content":" world"`) {
		t.Fatalf("expected ' world' SSE chunk, got %s", bodyStr)
	}
	if !strings.Contains(bodyStr, "data: [DONE]") {
		t.Fatalf("expected terminal [DONE], got %s", bodyStr)
	}
}

func TestResponses_InteractionsProvider_NonStream(t *testing.T) {
	h, cleanup := setupInteractionsTest(t)
	defer cleanup()

	fe := &fakeExecutor{
		responses: []struct {
			resp *executor.Response
			err  error
		}{
			{
				resp: &executor.Response{
					StatusCode: http.StatusOK,
					Body: []byte(`{
						"id": "interaction-456",
						"model": "gemini-2.5-flash",
						"steps": [
							{"type": "model_output", "content": "Hello from Interactions Responses"}
						],
						"usage": {"input_tokens": 10, "output_tokens": 5, "total_tokens": 15}
					}`),
				},
			},
		},
	}
	executor.GetRegistry().Register("gemini-interactions", executor.FormatInteractions, fe)

	body := []byte(`{"model":"gemini-interactions/gemini-2.5-flash","input":"hi"}`)
	rec, c := jsonRequestWithAllowedModels(t, http.MethodPost, "/v1/responses", body, nil)
	h.Responses(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"text":"Hello from Interactions Responses"`)) {
		t.Fatalf("expected translated output_text, got %s", rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"object":"response"`)) {
		t.Fatalf("expected response object, got %s", rec.Body.String())
	}
	if gjson.GetBytes(rec.Body.Bytes(), "model").String() != "gemini-2.5-flash" {
		t.Fatalf("expected model gemini-2.5-flash, got %s", rec.Body.String())
	}
	if fe.callCount != 1 {
		t.Errorf("expected 1 upstream call, got %d", fe.callCount)
	}
}

func TestResponses_InteractionsProvider_Stream(t *testing.T) {
	h, cleanup := setupInteractionsTest(t)
	defer cleanup()
	if err := h.providerCfg.Save("gemini-interactions", providercfg.ProviderSettings{
		RoutingMode: providercfg.RoundRobin,
		HoldbackMs:  intptr(10),
	}); err != nil {
		t.Fatalf("save holdback settings: %v", err)
	}

	chunks := make(chan executor.StreamChunk, 7)
	chunks <- executor.StreamChunk{Payload: []byte(`data: {"event_type":"interaction.created","interaction":{"id":"interaction-789","model":"gemini-2.5-flash"}}` + "\n\n")}
	chunks <- executor.StreamChunk{Payload: []byte(`data: {"event_type":"step.start","index":0,"step":{"type":"model_output","id":"item-0"}}` + "\n\n")}
	chunks <- executor.StreamChunk{Payload: []byte(`data: {"event_type":"step.delta","index":0,"delta":{"text":"Hello"}}` + "\n\n")}
	chunks <- executor.StreamChunk{Payload: []byte(`data: {"event_type":"step.delta","index":0,"delta":{"text":" responses"}}` + "\n\n")}
	chunks <- executor.StreamChunk{Payload: []byte(`data: {"event_type":"step.stop","index":0}` + "\n\n")}
	chunks <- executor.StreamChunk{Payload: []byte(`data: {"event_type":"interaction.completed","interaction":{"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}}` + "\n\n")}
	chunks <- executor.StreamChunk{Payload: []byte("data: [DONE]\n\n")}
	close(chunks)

	fe := &fakeExecutor{
		streamResults: []struct {
			result *executor.StreamResult
			err    error
		}{
			{result: &executor.StreamResult{Chunks: chunks, StatusCode: http.StatusOK}},
		},
	}
	executor.GetRegistry().Register("gemini-interactions", executor.FormatInteractions, fe)

	body := []byte(`{"model":"gemini-interactions/gemini-2.5-flash","input":"hi","stream":true}`)
	rec, c := jsonRequestWithAllowedModels(t, http.MethodPost, "/v1/responses", body, nil)
	h.Responses(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("expected text/event-stream, got %q", rec.Header().Get("Content-Type"))
	}
	bodyStr := rec.Body.String()
	if !strings.Contains(bodyStr, "response.created") {
		t.Fatalf("expected response.created event, got %s", bodyStr)
	}
	if !strings.Contains(bodyStr, "response.output_text.delta") {
		t.Fatalf("expected response.output_text.delta event, got %s", bodyStr)
	}
	if !strings.Contains(bodyStr, `"text":"Hello"`) && !strings.Contains(bodyStr, `"text":"Hello responses"`) {
		t.Fatalf("expected 'Hello' text delta, got %s", bodyStr)
	}
	if !strings.Contains(bodyStr, "data: [DONE]") {
		t.Fatalf("expected terminal [DONE], got %s", bodyStr)
	}
}
