package v1

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/rickicode/AxonRouter-Go/internal/cache"
	"github.com/rickicode/AxonRouter-Go/internal/db"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/rickicode/AxonRouter-Go/internal/providercfg"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
)

func setupResponsesTest(t *testing.T) (*Handler, func()) {
	logging.Init("text")
	h := newTestHandler(t)
	wq := db.NewWriteQueue(h.db)
	tracker := usage.NewTracker(h.db)
	tracker.SetWriteQueue(wq)
	h.tracker = tracker

	if _, err := h.db.Exec(`INSERT OR IGNORE INTO provider_types (id, display_name, format, base_url, created_at) VALUES ('restest','ResTest','openai-responses','http://x',0)`); err != nil {
		t.Fatalf("seed provider_type: %v", err)
	}
	if _, err := h.db.Exec(`INSERT OR IGNORE INTO connections (id, provider_type_id, name, auth_type, status, is_active, created_at, updated_at, provider_specific_data) VALUES ('restest-conn1','restest','c1','none','ready',1,0,0,'{"account_id":"acc-restest"}'), ('restest-conn2','restest','c2','none','ready',1,0,0,'')`); err != nil {
		t.Fatalf("seed connection: %v", err)
	}
	h.store.SeedConnection("restest-conn1", "restest", "ready", 0)
	h.store.SeedConnection("restest-conn2", "restest", "ready", 0)
	h.elig.RecomputeAll()

	return h, func() {
		executor.GetRegistry().Unregister("restest")
	}
}

func intptr(n int) *int { return &n }

func TestResponses_UpstreamClientErrorPassedThrough(t *testing.T) {
	h, cleanup := setupResponsesTest(t)
	defer cleanup()

	upBody := []byte(`{"error":{"message":"context too long","type":"invalid_request_error","code":"context_length_exceeded"}}`)
	fe := &fakeExecutor{
		responses: []struct {
			resp *executor.Response
			err  error
		}{
			{nil, &executor.UpstreamError{StatusCode: http.StatusBadRequest, Body: upBody}},
		},
	}
	executor.GetRegistry().Register("restest", executor.FormatOpenAIResponses, fe)

	body := []byte(`{"model":"restest/model","input":"hi"}`)
	rec, c := jsonRequestWithAllowedModels(t, http.MethodPost, "/v1/responses", body, nil)
	h.Responses(c)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400: %s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("invalid response body: %v", err)
	}
	if got["error"].(map[string]any)["code"] != "context_length_exceeded" {
		t.Errorf("response=%v, want upstream error code", got)
	}
	if fe.callCount != 1 {
		t.Errorf("expected 1 upstream call, got %d", fe.callCount)
	}
}

func TestResponses_NonStreamSuccess(t *testing.T) {
	h, cleanup := setupResponsesTest(t)
	defer cleanup()

	fe := &fakeExecutor{
		responses: []struct {
			resp *executor.Response
			err  error
		}{
			{
				resp: &executor.Response{
					StatusCode: http.StatusOK,
					Body:       []byte(`{"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Hello"}]}]}`),
				},
			},
		},
	}
	executor.GetRegistry().Register("restest", executor.FormatOpenAIResponses, fe)

	body := []byte(`{"model":"restest/model","input":"hi"}`)
	rec, c := jsonRequestWithAllowedModels(t, http.MethodPost, "/v1/responses", body, nil)
	h.Responses(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"text":"Hello"`)) {
		t.Fatalf("expected response body to contain Hello, got %s", rec.Body.String())
	}
	if fe.callCount != 1 {
		t.Errorf("expected 1 upstream call, got %d", fe.callCount)
	}
	if rec.Header().Get("X-Cache-Status") != "MISS" {
		t.Errorf("expected cache miss header, got %q", rec.Header().Get("X-Cache-Status"))
	}
}

func TestResponses_StreamSuccess(t *testing.T) {
	h, cleanup := setupResponsesTest(t)
	defer cleanup()
	if err := h.providerCfg.Save("restest", providercfg.ProviderSettings{
		RoutingMode:   providercfg.RoundRobin,
		HoldbackMs:    intptr(10),
		HoldbackBytes: intptr(1),
	}); err != nil {
		t.Fatalf("save holdback settings: %v", err)
	}

	chunks := make(chan executor.StreamChunk, 1)
	chunks <- executor.StreamChunk{
		Payload: []byte(`{"type":"response.completed","response":{"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Hi"}]}]}}`),
	}
	close(chunks)

	fe := &fakeExecutor{
		streamResults: []struct {
			result *executor.StreamResult
			err    error
		}{
			{result: &executor.StreamResult{Chunks: chunks, StatusCode: http.StatusOK}},
		},
	}
	executor.GetRegistry().Register("restest", executor.FormatOpenAIResponses, fe)

	body := []byte(`{"model":"restest/model","input":"hi","stream":true}`)
	rec, c := jsonRequestWithAllowedModels(t, http.MethodPost, "/v1/responses", body, nil)
	h.Responses(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("expected text/event-stream, got %q", rec.Header().Get("Content-Type"))
	}
	bodyStr := rec.Body.String()
	if !strings.Contains(bodyStr, `"text":"Hi"`) {
		t.Fatalf("expected streamed content, got %s", bodyStr)
	}
	if !strings.Contains(bodyStr, "[DONE]") {
		t.Fatalf("expected terminal [DONE], got %s", bodyStr)
	}
}

func TestResponses_FailoverAfterHoldbackError(t *testing.T) {
	h, cleanup := setupResponsesTest(t)
	defer cleanup()
	if err := h.providerCfg.Save("restest", providercfg.ProviderSettings{
		RoutingMode:   providercfg.RoundRobin,
		HoldbackMs:    intptr(50),
		HoldbackBytes: intptr(1),
	}); err != nil {
		t.Fatalf("save holdback settings: %v", err)
	}

	errCh := make(chan executor.StreamChunk, 1)
	errCh <- executor.StreamChunk{Err: errors.New("boom")}
	close(errCh)

	okCh := make(chan executor.StreamChunk, 1)
	okCh <- executor.StreamChunk{
		Payload: []byte(`{"type":"response.completed","response":{"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Recovered"}]}]}}`),
	}
	close(okCh)

	fe := &fakeExecutor{
		streamResults: []struct {
			result *executor.StreamResult
			err    error
		}{
			{result: &executor.StreamResult{Chunks: errCh, StatusCode: http.StatusOK}},
			{result: &executor.StreamResult{Chunks: okCh, StatusCode: http.StatusOK}},
		},
	}
	executor.GetRegistry().Register("restest", executor.FormatOpenAIResponses, fe)

	body := []byte(`{"model":"restest/model","input":"hi","stream":true}`)
	rec, c := jsonRequestWithAllowedModels(t, http.MethodPost, "/v1/responses", body, nil)
	h.Responses(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200: %s", rec.Code, rec.Body.String())
	}
	if fe.callCount != 2 {
		t.Fatalf("expected 2 upstream calls after failover, got %d", fe.callCount)
	}
	if !strings.Contains(rec.Body.String(), `"text":"Recovered"`) {
		t.Fatalf("expected recovered content, got %s", rec.Body.String())
	}
}

func TestResponses_ExactCacheHit(t *testing.T) {
	h, cleanup := setupResponsesTest(t)
	defer cleanup()

	executor.GetRegistry().Register("restest", executor.FormatOpenAIResponses, &fakeExecutor{})

	body := []byte(`{"model":"restest/model","input":"hi"}`)
	key := cache.ComputeKey(body, "restest/model")
	h.exactCache.Set(key, cache.CacheEntry{
		Body:        []byte(`{"cached":true}`),
		StatusCode:  http.StatusOK,
		ContentType: "application/json",
	})

	rec, c := jsonRequestWithAllowedModels(t, http.MethodPost, "/v1/responses", body, nil)
	h.Responses(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Cache-Status") != "HIT" {
		t.Errorf("expected cache hit header, got %q", rec.Header().Get("X-Cache-Status"))
	}
	if string(rec.Body.Bytes()) != `{"cached":true}` {
		t.Errorf("expected cached body, got %s", rec.Body.String())
	}
}
