package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/mcp/protocol"
	"github.com/rickicode/AxonRouter-Go/internal/mcp/server"
)

func TestSSEEndpointEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv := server.NewServer()
	defer srv.Stop(context.Background())
	sse := NewSSE(srv, nil)

	r := gin.New()
	r.GET("/mcp/sse", sse.Handler())

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/mcp/sse", nil)
	// Use a context we can cancel to unblock the SSE loop.
	ctx, cancel := context.WithCancel(context.Background())
	req = req.WithContext(ctx)

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "event: endpoint") {
		t.Fatalf("expected endpoint event, got:\n%s", body)
	}
	if !strings.Contains(body, "/mcp/messages?session_id=") {
		t.Fatalf("expected session-specific message URL, got:\n%s", body)
	}
}

func TestMessagesInitialize(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv := server.NewServer()
	defer srv.Stop(context.Background())
	msg := NewMessages(srv)

	r := gin.New()
	r.POST("/mcp/messages", msg.Handler())
	r.GET("/mcp/sse", NewSSE(srv, nil).Handler())

	// Create session via SSE endpoint.
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/mcp/sse", nil)
	ctx, cancel := context.WithCancel(context.Background())
	req = req.WithContext(ctx)
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	r.ServeHTTP(w, req)

	body := w.Body.String()
	prefix := "/mcp/messages?session_id="
	idx := strings.Index(body, prefix)
	if idx == -1 {
		t.Fatalf("session id not found in SSE body:\n%s", body)
	}
	rest := body[idx+len(prefix):]
	end := strings.IndexAny(rest, "\r\n")
	if end == -1 {
		t.Fatalf("could not terminate session id")
	}
	sessionID := rest[:end]

	initReq := protocol.Request{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      protocol.NewRequestIDNumber(1),
		Method:  protocol.MethodInitialize,
	}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(initReq); err != nil {
		t.Fatalf("encode: %v", err)
	}

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/mcp/messages?session_id="+sessionID, &buf)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var res protocol.Response
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if res.Error != nil {
		t.Fatalf("unexpected error: %v", res.Error)
	}
	var result protocol.InitializeResult
	if err := json.Unmarshal(res.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result.ServerInfo.Name != protocol.ServerName {
		t.Fatalf("unexpected server info: %+v", result.ServerInfo)
	}
}

func TestMessagesToolsList(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv := server.NewServer()
	defer srv.Stop(context.Background())
	msg := NewMessages(srv)

	r := gin.New()
	r.GET("/mcp/sse", NewSSE(srv, nil).Handler())
	r.POST("/mcp/messages", msg.Handler())

	// Create session.
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/mcp/sse", nil)
	ctx, cancel := context.WithCancel(context.Background())
	req = req.WithContext(ctx)
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	r.ServeHTTP(w, req)
	body := w.Body.String()
	prefix := "/mcp/messages?session_id="
	idx := strings.Index(body, prefix)
	rest := body[idx+len(prefix):]
	end := strings.IndexAny(rest, "\r\n")
	sessionID := rest[:end]

	listReq := protocol.Request{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      protocol.NewRequestIDNumber(10),
		Method:  protocol.MethodToolsList,
	}
	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode(listReq)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/mcp/messages?session_id="+sessionID, &buf)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	var res protocol.Response
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if res.Error != nil {
		t.Fatalf("unexpected error: %v", res.Error)
	}
	var result protocol.ListToolsResult
	if err := json.Unmarshal(res.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(result.Tools) != 0 {
		t.Fatalf("expected 0 tools, got %d", len(result.Tools))
	}
}

func TestMessagesMissingSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv := server.NewServer()
	defer srv.Stop(context.Background())
	msg := NewMessages(srv)

	r := gin.New()
	r.POST("/mcp/messages", msg.Handler())

	payload := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/mcp/messages", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func drainSSE(w *httptest.ResponseRecorder) io.Reader {
	return w.Body
}
