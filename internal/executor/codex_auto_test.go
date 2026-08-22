package executor

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestCodexAutoExecutor_WebsocketFlagRoutesToWebsockets(t *testing.T) {
	upgrader := websocket.AcceptOptions{InsecureSkipVerify: true}
	completed := []byte(`{"type":"response.completed","response":{"id":"resp-flag","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`)
	var gotHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		conn, err := websocket.Accept(w, r, &upgrader)
		if err != nil {
			t.Errorf("websocket accept failed: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")
		if _, _, err := conn.Reader(context.Background()); err != nil {
			t.Errorf("reader failed: %v", err)
			return
		}
		if err := conn.Write(context.Background(), websocket.MessageText, completed); err != nil {
			t.Errorf("write failed: %v", err)
		}
	}))
	defer server.Close()

	base := NewBaseExecutor()
	exec := NewCodexAutoExecutor(base)
	req := &Request{
		Provider:    "cx",
		Model:       "cx/gpt-5.4",
		BaseURL:     server.URL,
		AccessToken: "test-token",
		ProviderSpecificData: map[string]string{
			"websockets": "true",
		},
		Body: []byte(`{"input":[{"role":"user","content":"hello"}]}`),
	}

	res, err := exec.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if res == nil {
		t.Fatal("expected response, got nil")
	}
	if got := string(res.Body); !strings.Contains(got, `"type":"response.completed"`) {
		t.Fatalf("expected response.completed in body, got %s", got)
	}
	if got := gotHeaders.Get("OpenAI-Beta"); got != codexResponsesWebsocketBetaHeaderValue {
		t.Fatalf("OpenAI-Beta=%q, want %q", got, codexResponsesWebsocketBetaHeaderValue)
	}
}

func TestCodexAutoExecutor_PlainHTTPRoutesToCodexExecutor(t *testing.T) {
	var transport string
	var gotHeaders http.Header
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isWebsocketUpgrade(r) {
			transport = "websocket"
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		transport = "http"
		gotHeaders = r.Header.Clone()
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `data: {"type":"response.completed","response":{"id":"resp-http","output":[],"usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5}}}`)
		fmt.Fprintln(w)
	}))
	defer server.Close()

	base := NewBaseExecutor()
	exec := NewCodexAutoExecutor(base)
	req := &Request{
		Provider:    "cx",
		Model:       "cx/gpt-5.4",
		BaseURL:     server.URL,
		AccessToken: "test-token",
		Body:        []byte(`{"input":[{"role":"user","content":"hello"}]}`),
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
	if transport != "http" {
		t.Fatalf("expected HTTP transport, got %q", transport)
	}
	if got := gotHeaders.Get("Connection"); got != "Keep-Alive" {
		t.Fatalf("Connection=%q, want Keep-Alive", got)
	}
	if !strings.Contains(string(gotBody), `"stream":true`) {
		t.Fatalf("expected stream=true in HTTP body, got %s", string(gotBody))
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want 200", res.StatusCode)
	}
}

func TestCodexAutoExecutor_MissingAndInvalidFlagEdgeCases(t *testing.T) {
	cases := []struct {
		name string
		psd  map[string]string
	}{
		{name: "missing flag", psd: nil},
		{name: "empty flag", psd: map[string]string{"websockets": ""}},
		{name: "whitespace flag", psd: map[string]string{"websockets": "   "}},
		{name: "invalid flag", psd: map[string]string{"websockets": "maybe"}},
		{name: "false flag", psd: map[string]string{"websockets": "false"}},
		{name: "zero flag", psd: map[string]string{"websockets": "0"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var transport string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if isWebsocketUpgrade(r) {
					transport = "websocket"
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				transport = "http"
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				fmt.Fprintln(w, `data: {"type":"response.completed","response":{"id":"resp-edge","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`)
				fmt.Fprintln(w)
			}))
			defer server.Close()

			base := NewBaseExecutor()
			exec := NewCodexAutoExecutor(base)
			req := &Request{
				Provider:             "cx",
				Model:                "cx/gpt-5.4",
				BaseURL:              server.URL,
				AccessToken:          "test-token",
				ProviderSpecificData: tc.psd,
				Body:                 []byte(`{"input":[{"role":"user","content":"hello"}]}`),
				StreamConfig: &StreamConfig{
					FetchTimeoutMs:           5000,
					StreamIdleTimeoutMs:      5000,
					StreamReadinessTimeoutMs: 5000,
				},
			}

			_, err := exec.Execute(context.Background(), req)
			if err != nil {
				t.Fatalf("Execute error: %v", err)
			}
			if transport != "http" {
				t.Fatalf("expected HTTP fallback for %s, got %q", tc.name, transport)
			}
		})
	}
}

func TestCodexAutoExecutor_ExecuteStreamRoutesToHTTP(t *testing.T) {
	var transport string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isWebsocketUpgrade(r) {
			transport = "websocket"
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		transport = "http"
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `data: {"type":"response.completed","response":{"id":"resp-stream","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`)
		fmt.Fprintln(w)
	}))
	defer server.Close()

	base := NewBaseExecutor()
	base.StreamIdleTimeout = 200 * time.Millisecond
	exec := NewCodexAutoExecutor(base)
	req := &Request{
		Provider:    "cx",
		Model:       "cx/gpt-5.4",
		BaseURL:     server.URL,
		AccessToken: "test-token",
		Body:        []byte(`{"input":[{"role":"user","content":"hello"}]}`),
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
	if res == nil {
		t.Fatal("expected stream result, got nil")
	}
	var found bool
	for chunk := range res.Chunks {
		if chunk.Err != nil {
			t.Fatalf("unexpected chunk error: %v", chunk.Err)
		}
		if chunk.Payload != nil && strings.Contains(string(chunk.Payload), "response.completed") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected response.completed chunk")
	}
	if transport != "http" {
		t.Fatalf("expected HTTP transport for streaming, got %q", transport)
	}
}

func TestCodexAutoExecutor_ExecuteStreamFlagReturnsNotImplemented(t *testing.T) {
	base := NewBaseExecutor()
	base.StreamIdleTimeout = 200 * time.Millisecond
	exec := NewCodexAutoExecutor(base)
	req := &Request{
		Provider:    "cx",
		Model:       "cx/gpt-5.4",
		BaseURL:     "http://127.0.0.1:1",
		AccessToken: "test-token",
		ProviderSpecificData: map[string]string{
			"websockets": "true",
		},
		Body: []byte(`{"input":[{"role":"user","content":"hello"}]}`),
		StreamConfig: &StreamConfig{
			FetchTimeoutMs:           5000,
			StreamIdleTimeoutMs:      5000,
			StreamReadinessTimeoutMs: 5000,
		},
	}

	_, err := exec.ExecuteStream(context.Background(), req)
	if err == nil {
		t.Fatal("expected error from unimplemented websocket streaming")
	}
	if !strings.Contains(err.Error(), "ExecuteStream is not implemented") {
		t.Fatalf("expected not implemented error, got %v", err)
	}
}

func TestCodexAutoExecutor_WithDownstreamWebsocket(t *testing.T) {
	base := NewBaseExecutor()
	exec := NewCodexAutoExecutor(base)
	req := &Request{
		Provider:    "cx",
		Model:       "cx/gpt-5.4",
		AccessToken: "test-token",
		Body:        []byte(`{"input":[{"role":"user","content":"hello"}]}`),
	}

	upgrader := websocket.AcceptOptions{InsecureSkipVerify: true}
	completed := []byte(`{"type":"response.completed","response":{"id":"resp-ws","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &upgrader)
		if err != nil {
			t.Errorf("websocket accept failed: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")
		if _, _, err := conn.Reader(context.Background()); err != nil {
			t.Errorf("reader failed: %v", err)
			return
		}
		if err := conn.Write(context.Background(), websocket.MessageText, completed); err != nil {
			t.Errorf("write failed: %v", err)
		}
	}))
	defer server.Close()
	req.BaseURL = server.URL

	ctx := WithDownstreamWebsocket(context.Background())
	res, err := exec.Execute(ctx, req)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if res == nil {
		t.Fatal("expected response, got nil")
	}
	if got := string(res.Body); !strings.Contains(got, `"type":"response.completed"`) {
		t.Fatalf("expected response.completed in body, got %s", got)
	}
}

func isWebsocketUpgrade(r *http.Request) bool {
	return strings.Contains(strings.ToLower(r.Header.Get("Upgrade")), "websocket")
}
