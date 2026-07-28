package executor

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
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
