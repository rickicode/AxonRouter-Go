package transport

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/mcp/protocol"
	"github.com/rickicode/AxonRouter-Go/internal/mcp/server"
)

// Messages handles POST /mcp/messages for both SSE and HTTP Streamable transports.
type Messages struct {
	server *server.Server
}

// NewMessages creates a message handler for the given MCP server.
func NewMessages(srv *server.Server) *Messages {
	return &Messages{server: srv}
}

// Handler returns the gin.HandlerFunc for POST /mcp/messages.
func (h *Messages) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.Query("session_id")
		if sessionID == "" {
			server.WriteErrorResponse(c.Writer, http.StatusBadRequest, protocol.ErrInvalidRequest, "session_id is required")
			return
		}

		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			server.WriteErrorResponse(c.Writer, http.StatusBadRequest, protocol.ErrInvalidRequest, "failed to read body")
			return
		}
		defer c.Request.Body.Close()

		res := h.server.Dispatch(c.Request.Context(), sessionID, body)
		if res == nil {
			c.Status(http.StatusAccepted)
			return
		}

		c.Header("Content-Type", "application/json")
		c.JSON(http.StatusOK, res)
	}
}

// StreamableHandler returns the HTTP Streamable transport variant.
// It is identical to Handler in this implementation because Dispatch returns
// JSON-RPC responses directly; future work may add SSE-framed streaming responses.
func (h *Messages) StreamableHandler() gin.HandlerFunc {
	return h.Handler()
}

// Must is a small helper that panics on error; useful for tests.
func Must(v interface{}, err error) interface{} {
	if err != nil {
		panic(err)
	}
	return v
}

// IsNotification reports whether body is a JSON-RPC notification.
func IsNotification(body []byte) bool {
	var req protocol.Request
	if err := json.Unmarshal(body, &req); err != nil {
		return false
	}
	return req.IsNotification()
}
