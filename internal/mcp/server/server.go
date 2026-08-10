// Package server implements the core Model Context Protocol server.
// It handles method dispatch, session management, and tool registration.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rickicode/AxonRouter-Go/internal/mcp/protocol"
)

// ToolHandler is the function signature implemented by MCP tools.
type ToolHandler func(ctx context.Context, args json.RawMessage) (*protocol.ToolResult, error)

// registeredTool binds a tool definition to its runtime handler.
type registeredTool struct {
	def     protocol.Tool
	handler ToolHandler
}

// Server is the MCP server core.
type Server struct {
	mu         sync.RWMutex
	tools      map[string]registeredTool
	sessions   map[string]*Session
	sessionTTL time.Duration
	closeCh    chan string
	closeOnce  sync.Once
	stop       chan struct{}
}

// Session represents a single MCP client session.
type Session struct {
	id        string
	server    *Server
	createdAt time.Time
	lastRead  time.Time
	mu        sync.Mutex
	closed    bool
	onClose   []func()
}

// NewServer creates an MCP server with default TTL and cleanup behavior.
func NewServer() *Server {
	return NewServerWithTTL(10 * time.Minute)
}

// NewServerWithTTL creates an MCP server with a configurable session TTL.
func NewServerWithTTL(ttl time.Duration) *Server {
	s := &Server{
		tools:      make(map[string]registeredTool),
		sessions:   make(map[string]*Session),
		sessionTTL: ttl,
		closeCh:    make(chan string, 64),
		stop:       make(chan struct{}),
	}
	go s.reapLoop()
	return s
}

// Stop shuts down the server and closes all sessions.
func (s *Server) Stop(ctx context.Context) error {
	s.closeOnce.Do(func() {
		close(s.stop)
		s.mu.Lock()
		list := make([]*Session, 0, len(s.sessions))
		for _, sess := range s.sessions {
			list = append(list, sess)
		}
		s.sessions = make(map[string]*Session)
		s.mu.Unlock()
		for _, sess := range list {
			sess.Close()
		}
	})
	return nil
}

// RegisterTool adds or replaces a tool.
func (s *Server) RegisterTool(tool protocol.Tool, handler ToolHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tools == nil {
		s.tools = make(map[string]registeredTool)
	}
	s.tools[tool.Name] = registeredTool{def: tool, handler: handler}
}

// ListTools returns the registered tools.
func (s *Server) ListTools() []protocol.Tool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]protocol.Tool, 0, len(s.tools))
	for _, t := range s.tools {
		out = append(out, protocol.Tool{
			Name:        t.def.Name,
			Description: t.def.Description,
			InputSchema: t.def.InputSchema,
			Annotations: t.def.Annotations,
		})
	}
	return out
}

// NewSession creates and tracks a new client session.
func (s *Server) NewSession() *Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := &Session{
		id:        uuid.NewString(),
		server:    s,
		createdAt: time.Now(),
		lastRead:  time.Now(),
		onClose:   make([]func(), 0),
	}
	if s.sessions == nil {
		s.sessions = make(map[string]*Session)
	}
	s.sessions[sess.id] = sess
	return sess
}

// GetSession returns a tracked session by id.
func (s *Server) GetSession(id string) (*Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[id]
	if !ok {
		return nil, false
	}
	sess.Touch()
	return sess, true
}

// SessionIDs returns a snapshot of active session IDs.
func (s *Server) SessionIDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.sessions))
	for id := range s.sessions {
		out = append(out, id)
	}
	return out
}

// removeSession deletes a session from the store.
func (s *Server) removeSession(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}

// Dispatch parses a request and routes it to the appropriate handler.
func (s *Server) Dispatch(ctx context.Context, sessionID string, body []byte) *protocol.Response {
	var req protocol.Request
	if err := json.Unmarshal(body, &req); err != nil {
		return errorResponse(protocol.RequestID{}, protocol.ErrParseError, "parse error: "+err.Error())
	}

	if req.JSONRPC != protocol.JSONRPCVersion {
		return errorResponse(req.ID, protocol.ErrInvalidRequest, "invalid jsonrpc version")
	}

	sess, ok := s.GetSession(sessionID)
	if !ok {
		return errorResponse(req.ID, protocol.ErrInvalidRequest, "session not found")
	}
	sess.Touch()

	switch req.Method {
	case protocol.MethodInitialize:
		return s.handleInitialize(req)
	case protocol.MethodInitialized:
		// Notification: no response required, but we return empty result for HTTP streamable.
		if req.IsNotification() {
			return nil
		}
		return successResponse(req.ID, &protocol.EmptyResult{})
	case protocol.MethodPing:
		return successResponse(req.ID, &protocol.PingResult{})
	case protocol.MethodToolsList:
		return s.handleToolsList(req)
	case protocol.MethodToolsCall:
		return s.handleToolsCall(ctx, req)
	default:
		// Notifications outside our supported set are accepted silently.
		if req.IsNotification() {
			return nil
		}
		return errorResponse(req.ID, protocol.ErrMethodNotFound, "method not found: "+req.Method)
	}
}

// handleInitialize negotiates protocol version and returns server info.
func (s *Server) handleInitialize(req protocol.Request) *protocol.Response {
	if req.IsNotification() {
		return errorResponse(req.ID, protocol.ErrInvalidRequest, "initialize must be a request")
	}
	var params protocol.InitializeParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return errorResponse(req.ID, protocol.ErrInvalidParams, "invalid initialize params: "+err.Error())
		}
	}

	version := params.ProtocolVersion
	if version != protocol.ProtocolVersion20241105 && version != protocol.ProtocolVersion20250326 {
		version = protocol.ProtocolVersion20250326
	}

	result := protocol.InitializeResult{
		ProtocolVersion: version,
		ServerInfo: protocol.Implementation{
			Name:    protocol.ServerName,
			Version: protocol.ServerVersion,
		},
		Capabilities: protocol.ServerCapabilities{
			Tools: &protocol.ToolsCapability{ListChanged: false},
		},
	}
	return successResponse(req.ID, result)
}

// handleToolsList returns the registered tool list.
func (s *Server) handleToolsList(req protocol.Request) *protocol.Response {
	result := protocol.ListToolsResult{Tools: s.ListTools()}
	return successResponse(req.ID, result)
}

// handleToolsCall invokes a registered tool handler or returns an error.
func (s *Server) handleToolsCall(ctx context.Context, req protocol.Request) *protocol.Response {
	if req.IsNotification() {
		return errorResponse(req.ID, protocol.ErrInvalidRequest, "tools/call must be a request")
	}
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return errorResponse(req.ID, protocol.ErrInvalidParams, "invalid tools/call params: "+err.Error())
		}
	}

	s.mu.RLock()
	tool, ok := s.tools[params.Name]
	s.mu.RUnlock()
	if !ok {
		return errorResponse(req.ID, protocol.ErrToolNotFound, "tool not found: "+params.Name)
	}

	if tool.handler == nil {
		return errorResponse(req.ID, protocol.ErrMethodNotFound, "tool not implemented: "+params.Name)
	}

	res, err := tool.handler(ctx, params.Arguments)
	if err != nil {
		return errorResponse(req.ID, protocol.ErrToolExecutionError, err.Error())
	}
	return successResponse(req.ID, res)
}

// Success/Error helpers.

func successResponse(id protocol.RequestID, result interface{}) *protocol.Response {
	raw, err := json.Marshal(result)
	if err != nil {
		return errorResponse(id, protocol.ErrInternalError, "failed to marshal result")
	}
	return &protocol.Response{JSONRPC: protocol.JSONRPCVersion, ID: id, Result: raw}
}

func errorResponse(id protocol.RequestID, code int, message string) *protocol.Response {
	return &protocol.Response{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      id,
		Error: &protocol.ErrorObject{
			Code:    code,
			Message: message,
		},
	}
}

// Session helpers.

// ID returns the session id.
func (sess *Session) ID() string { return sess.id }

// Touch updates last activity time.
func (sess *Session) Touch() {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	sess.lastRead = time.Now()
}

// LastRead returns last activity time.
func (sess *Session) LastRead() time.Time {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return sess.lastRead
}

// OnClose registers a cleanup callback.
func (sess *Session) OnClose(fn func()) {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	sess.onClose = append(sess.onClose, fn)
}

// Close marks the session closed and runs callbacks.
func (sess *Session) Close() error {
	sess.mu.Lock()
	if sess.closed {
		sess.mu.Unlock()
		return nil
	}
	sess.closed = true
	callbacks := make([]func(), len(sess.onClose))
	copy(callbacks, sess.onClose)
	sess.mu.Unlock()

	for _, fn := range callbacks {
		fn()
	}
	return nil
}

// IsClosed reports whether the session is closed.
func (sess *Session) IsClosed() bool {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return sess.closed
}

// reapLoop runs background cleanup for timed-out sessions.
func (s *Server) reapLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case id := <-s.closeCh:
			s.removeSession(id)
		case <-ticker.C:
			s.reap()
		}
	}
}

func (s *Server) reap() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for id, sess := range s.sessions {
		if sess.IsClosed() {
			delete(s.sessions, id)
			continue
		}
		if now.Sub(sess.LastRead()) > s.sessionTTL {
			delete(s.sessions, id)
			_ = sess.Close()
		}
	}
}

// WriteErrorResponse writes a JSON-RPC error to an http.ResponseWriter.
func WriteErrorResponse(w http.ResponseWriter, status int, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(&protocol.Response{
		JSONRPC: protocol.JSONRPCVersion,
		Error: &protocol.ErrorObject{
			Code:    code,
			Message: message,
		},
	})
}

// MustMarshal is a test helper that marshals v or panics.
func MustMarshal(v interface{}) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("marshal: %v", err))
	}
	return b
}
