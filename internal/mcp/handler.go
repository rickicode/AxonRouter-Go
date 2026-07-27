package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"
)

const sseRetryMS = 3000

// Handler exposes admin HTTP and SSE endpoints for MCP servers.
type Handler struct {
	store   *Store
	manager *Manager
}

// NewHandler creates a Handler and starts background session maintenance.
func NewHandler(db *sql.DB) *Handler {
	store := NewStore(db)
	mgr := NewManager(store)
	go mgr.idleReaper(context.Background(), 15*time.Second)
	return &Handler{store: store, manager: mgr}
}

// Stop cleanly terminates any running subprocesses.
func (h *Handler) Stop(ctx context.Context) {
	h.manager.Stop(ctx)
}

// adminServer is the JSON representation returned to dashboard clients.
type adminServerView struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Command       string            `json:"command"`
	Args          []string          `json:"args"`
	Env           map[string]string `json:"env"`
	Enabled       bool              `json:"enabled"`
	RestartPolicy string            `json:"restart_policy"`
	MaxClients    int               `json:"max_clients"`
	MaxIdleSec    int               `json:"max_idle_sec"`
	Status        string            `json:"status"`
	CreatedAt     int64             `json:"created_at"`
	UpdatedAt     int64             `json:"updated_at"`
}

func toAdminView(s *Server) adminServerView {
	return adminServerView{
		ID:            s.ID,
		Name:          s.Name,
		Command:       s.Command,
		Args:          s.Args,
		Env:           s.Env,
		Enabled:       s.Enabled,
		RestartPolicy: s.RestartPolicy,
		MaxClients:    s.MaxClients,
		MaxIdleSec:    s.MaxIdleSec,
		CreatedAt:     s.CreatedAt,
		UpdatedAt:     s.UpdatedAt,
	}
}

// ServerStatus reports whether a server registration is running/stopped/error.
func (h *Handler) ServerStatus(serverID string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	server, err := h.store.Get(ctx, serverID)
	if err != nil {
		return "unknown"
	}
	if !server.Enabled {
		return "stopped"
	}
	if h.manager.ActiveSessionCount(serverID) > 0 {
		return "running"
	}
	return "stopped"
}

// ListAdmin returns all registered MCP servers.
func (h *Handler) ListAdmin(c *gin.Context) {
	servers, err := h.store.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	views := make([]adminServerView, 0, len(servers))
	for _, s := range servers {
		v := toAdminView(s)
		v.Status = h.ServerStatus(s.ID)
		views = append(views, v)
	}
	c.JSON(http.StatusOK, gin.H{"data": views})
}

// CreateAdmin registers a new MCP server.
func (h *Handler) CreateAdmin(c *gin.Context) {
	var req Server
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.store.Create(c.Request.Context(), &req); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, ErrNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": toAdminView(&req)})
}

// UpdateAdmin modifies an existing MCP server. Missing fields are merged from
// the existing registration so callers can send partial updates.
func (h *Handler) UpdateAdmin(c *gin.Context) {
	id := c.Param("id")
	var req Server
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	existing, err := h.store.Get(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if req.Name != "" {
		existing.Name = req.Name
	}
	if req.Command != "" {
		existing.Command = req.Command
	}
	if req.Args != nil {
		existing.Args = req.Args
	}
	if req.Env != nil {
		existing.Env = req.Env
	}
	if req.RestartPolicy != "" {
		existing.RestartPolicy = req.RestartPolicy
	}
	if req.MaxClients > 0 {
		existing.MaxClients = req.MaxClients
	}
	if req.MaxIdleSec > 0 {
		existing.MaxIdleSec = req.MaxIdleSec
	}
	existing.Enabled = req.Enabled // allow explicit false
	existing.ID = id
	if err := h.store.Update(c.Request.Context(), existing); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": toAdminView(existing)})
}

// DeleteAdmin removes an MCP server registration.
func (h *Handler) DeleteAdmin(c *gin.Context) {
	id := c.Param("id")
	if err := h.store.Delete(c.Request.Context(), id); err != nil {
		if errors.Is(err, ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// TestAdmin verifies that a server can be spawned.
func (h *Handler) TestAdmin(c *gin.Context) {
	id := c.Param("id")
	if err := h.manager.TestServer(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "server started successfully"})
}

// ToolsAdmin connects to a server, asks for tools/list, and returns the result.
func (h *Handler) ToolsAdmin(c *gin.Context) {
	id := c.Param("id")
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	sess, err := h.manager.StartSession(ctx, id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	defer sess.Close()

	ch := sess.Subscribe()
	defer sess.Unsubscribe(ch)

	reqID := fmt.Sprintf("tools-%d", time.Now().UnixNano())
	if err := sess.Write(Message{
		JSONRPC: "2.0",
		ID:      reqID,
		Method:  "tools/list",
		Params:  map[string]interface{}{},
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	for {
		select {
		case line := <-ch:
			if len(line) == 0 {
				continue
			}
			msg, err := ParseMessage(line)
			if err != nil {
				continue
			}
			if fmt.Sprint(msg.ID) == reqID && msg.Result != nil {
				var tools ToolsResponse
				resJSON, _ := json.Marshal(msg.Result)
				if err := json.Unmarshal(resJSON, &tools); err == nil {
					c.JSON(http.StatusOK, gin.H{"data": tools.Tools})
					return
				}
				c.JSON(http.StatusOK, gin.H{"data": msg.Result})
				return
			}
		case <-ctx.Done():
			c.JSON(http.StatusGatewayTimeout, gin.H{"error": "timeout waiting for tools response"})
			return
		case <-time.After(100 * time.Millisecond):
			// keep polling to avoid blocking too tight
		}
	}
}

// SSEAdmin opens a server-sent-events stream for an MCP session.
// It implements the Anthropic MCP SSE contract:
//   1. Sends an "endpoint" event containing the POST message URL.
//   2. Forwards subprocess stdout frames as "message" events.
func (h *Handler) SSEAdmin(c *gin.Context) {
	id := c.Param("id")
	sess, err := h.manager.StartSession(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ch := sess.Subscribe()
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		sess.Close()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming not supported"})
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	// Announce the client message endpoint.
	messageEndpoint := fmt.Sprintf("/api/admin/mcp/%s/message?sessionId=%s", url.PathEscape(id), url.QueryEscape(sess.id))
	_, _ = c.Writer.Write([]byte("event: endpoint\ndata: " + messageEndpoint + "\n\n"))
	_, _ = c.Writer.Write([]byte(fmt.Sprintf("event: connected\ndata: %s\n\n", sess.id)))
	flusher.Flush()

	disconnect := c.Request.Context().Done()
	for {
		select {
		case line, ok := <-ch:
			if !ok {
				_, _ = c.Writer.Write(FormatSSE([]byte(`{"type":"session_closed"}`)))
				flusher.Flush()
				sess.Close()
				return
			}
			_, _ = c.Writer.Write([]byte("event: message\n"))
			_, _ = c.Writer.Write(FormatSSE(line))
			flusher.Flush()
		case <-disconnect:
			sess.Close()
			return
		}
	}
}

// MessageAdmin accepts a JSON-RPC message from a client and writes it to the
// matching session's stdin. The sessionId query parameter is required.
func (h *Handler) MessageAdmin(c *gin.Context) {
	sessionID := c.Query("sessionId")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "sessionId is required"})
		return
	}
	sess, ok := h.manager.GetSession(sessionID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	msg, err := ParseMessage(body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := sess.Write(msg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"success": true})
}

// AuthTokenFromQuery reads the JWT from the ?token= query parameter when the
// browser EventSource cannot set Authorization headers.
func AuthTokenFromQuery() gin.HandlerFunc {
	return func(c *gin.Context) {
		tok := c.Query("token")
		if tok != "" && c.GetHeader("Authorization") == "" {
			c.Request.Header.Set("Authorization", "Bearer "+tok)
		}
		c.Next()
	}
}


