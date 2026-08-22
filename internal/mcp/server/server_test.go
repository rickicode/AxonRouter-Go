package server

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/mcp/protocol"
)

func TestInitializeNegotiatesVersion(t *testing.T) {
	srv := NewServer()
	defer srv.Stop(context.Background())

	sess := srv.NewSession()
	if sess.ID() == "" {
		t.Fatal("expected session id")
	}

	req := protocol.Request{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      protocol.NewRequestIDNumber(1),
		Method:  protocol.MethodInitialize,
		Params:  MustMarshal(protocol.InitializeParams{ProtocolVersion: protocol.ProtocolVersion20241105}),
	}
	res := srv.Dispatch(context.Background(), sess.ID(), MustMarshal(req))
	if res == nil {
		t.Fatal("expected response")
	}
	if res.Error != nil {
		t.Fatalf("unexpected error: %v", res.Error)
	}
	var result protocol.InitializeResult
	if err := json.Unmarshal(res.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result.ProtocolVersion != protocol.ProtocolVersion20241105 {
		t.Fatalf("unexpected version: %s", result.ProtocolVersion)
	}
	if result.ServerInfo.Name != protocol.ServerName {
		t.Fatalf("unexpected server name: %s", result.ServerInfo.Name)
	}
}

func TestInitializeDefaultsToLatest(t *testing.T) {
	srv := NewServer()
	defer srv.Stop(context.Background())
	sess := srv.NewSession()

	req := protocol.Request{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      protocol.NewRequestIDString("init-1"),
		Method:  protocol.MethodInitialize,
	}
	res := srv.Dispatch(context.Background(), sess.ID(), MustMarshal(req))
	var result protocol.InitializeResult
	if err := json.Unmarshal(res.Result, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.ProtocolVersion != protocol.ProtocolVersion20250326 {
		t.Fatalf("expected latest version, got %s", result.ProtocolVersion)
	}
}

func TestToolsListEmpty(t *testing.T) {
	srv := NewServer()
	defer srv.Stop(context.Background())
	sess := srv.NewSession()

	req := protocol.Request{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      protocol.NewRequestIDNumber(2),
		Method:  protocol.MethodToolsList,
	}
	res := srv.Dispatch(context.Background(), sess.ID(), MustMarshal(req))
	if res.Error != nil {
		t.Fatalf("unexpected error: %v", res.Error)
	}
	var result protocol.ListToolsResult
	if err := json.Unmarshal(res.Result, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(result.Tools) != 0 {
		t.Fatalf("expected empty tools, got %d", len(result.Tools))
	}
}

func TestRegisteredToolListedAndCalled(t *testing.T) {
	srv := NewServer()
	defer srv.Stop(context.Background())

	called := false
	srv.RegisterTool(protocol.Tool{
		Name:        "echo",
		Description: "Echo tool",
		InputSchema: MustMarshal(map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{"message": map[string]interface{}{"type": "string"}},
		}),
	}, func(ctx context.Context, args json.RawMessage) (*protocol.ToolResult, error) {
		called = true
		return &protocol.ToolResult{Content: []protocol.ToolContent{{Type: "text", Text: "pong"}}}, nil
	})

	sess := srv.NewSession()
	req := protocol.Request{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      protocol.NewRequestIDNumber(3),
		Method:  protocol.MethodToolsCall,
		Params: MustMarshal(map[string]interface{}{
			"name":      "echo",
			"arguments": map[string]interface{}{"message": "ping"},
		}),
	}
	res := srv.Dispatch(context.Background(), sess.ID(), MustMarshal(req))
	if res.Error != nil {
		t.Fatalf("unexpected error: %v", res.Error)
	}
	if !called {
		t.Fatal("handler not called")
	}
}

func TestUnknownToolReturnsError(t *testing.T) {
	srv := NewServer()
	defer srv.Stop(context.Background())
	sess := srv.NewSession()

	req := protocol.Request{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      protocol.NewRequestIDNumber(4),
		Method:  protocol.MethodToolsCall,
		Params: MustMarshal(map[string]interface{}{
			"name":      "missing",
			"arguments": map[string]interface{}{},
		}),
	}
	res := srv.Dispatch(context.Background(), sess.ID(), MustMarshal(req))
	if res.Error == nil {
		t.Fatal("expected error")
	}
	if res.Error.Code != protocol.ErrToolNotFound {
		t.Fatalf("expected tool-not-found, got %d", res.Error.Code)
	}
}

func TestPing(t *testing.T) {
	srv := NewServer()
	defer srv.Stop(context.Background())
	sess := srv.NewSession()
	req := protocol.Request{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      protocol.NewRequestIDNumber(5),
		Method:  protocol.MethodPing,
	}
	res := srv.Dispatch(context.Background(), sess.ID(), MustMarshal(req))
	if res.Error != nil {
		t.Fatalf("unexpected error: %v", res.Error)
	}
}

func TestSessionCleanupAfterTTL(t *testing.T) {
	srv := NewServerWithTTL(50 * time.Millisecond)
	defer srv.Stop(context.Background())

	sess := srv.NewSession()
	id := sess.ID()
	if _, ok := srv.GetSession(id); !ok {
		t.Fatal("session missing immediately after creation")
	}
	// Wait longer than TTL, then trigger reap.
	time.Sleep(120 * time.Millisecond)
	srv.reap()
	if _, ok := srv.GetSession(id); ok {
		t.Fatal("expected expired session to be cleaned up")
	}
}

func TestCloseSession(t *testing.T) {
	srv := NewServer()
	defer srv.Stop(context.Background())
	sess := srv.NewSession()
	id := sess.ID()
	if err := sess.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if !sess.IsClosed() {
		t.Fatal("session should be marked closed")
	}
	srv.reap()
	if _, ok := srv.GetSession(id); ok {
		t.Fatal("closed session should be removed")
	}
}

func TestDispatchUnknownMethod(t *testing.T) {
	srv := NewServer()
	defer srv.Stop(context.Background())
	sess := srv.NewSession()
	req := protocol.Request{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      protocol.NewRequestIDNumber(6),
		Method:  "unknown/method",
	}
	res := srv.Dispatch(context.Background(), sess.ID(), MustMarshal(req))
	if res.Error == nil || res.Error.Code != protocol.ErrMethodNotFound {
		t.Fatalf("expected method not found, got %+v", res)
	}
}
