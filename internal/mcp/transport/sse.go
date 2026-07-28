// Package transport provides MCP server transport bindings.
package transport

import (
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/mcp/server"
)

// SSE handles the Server-Sent Events MCP transport.
type SSE struct {
	server     *server.Server
	messageURL func(sessionID string) string
}

// NewSSE creates an SSE transport for the given MCP server.
// If messageURL is nil, a default relative URL is generated.
func NewSSE(srv *server.Server, messageURL func(sessionID string) string) *SSE {
	if messageURL == nil {
		messageURL = defaultMessageURL
	}
	return &SSE{server: srv, messageURL: messageURL}
}

func defaultMessageURL(sessionID string) string {
	return "/mcp/messages?session_id=" + url.QueryEscape(sessionID)
}

// Handler returns the gin.HandlerFunc for GET /mcp/sse.
func (t *SSE) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		sess := t.server.NewSession()

		flusher, ok := c.Writer.(http.Flusher)
		if !ok {
			sess.Close()
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}

		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		c.Header("X-Accel-Buffering", "no")
		c.Status(http.StatusOK)
		flusher.Flush()

		// Clean up session when the client disconnects.
		disconnect := c.Request.Context().Done()
		sess.OnClose(func() {
			// Best-effort notify via context cancellation if still connected.
		})

		// Announce the message endpoint.
		endpoint := t.messageURL(sess.ID())
		_, _ = c.Writer.Write(formatSSE("endpoint", []byte(endpoint)))
		flusher.Flush()

		// Keep the connection open until disconnect.
		<-disconnect
		sess.Close()
	}
}

// formatSSE formats an SSE event with payload and trailing newlines.
func formatSSE(event string, payload []byte) []byte {
	out := []byte("event: " + event + "\n")
	out = append(out, []byte("data: ")...)
	out = append(out, payload...)
	out = append(out, '\n', '\n')
	return out
}
