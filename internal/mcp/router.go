// Package mcp exposes the AxonRouter MCP server at /mcp/*.
package mcp

import (
	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/mcp/server"
	"github.com/rickicode/AxonRouter-Go/internal/mcp/transport"
	"github.com/rickicode/AxonRouter-Go/internal/mcp/tools"
)

// API exposes the MCP server HTTP/SSE surface.
type API struct {
	server   *server.Server
	sse      *transport.SSE
	messages *transport.Messages
}

// NewAPI creates a new MCP API wired to the provided dependencies.
func NewAPI() *API {
	srv := server.NewServer()
	
	// Register the axonrouter_web_search tool
	tools.RegisterWebSearchTool(srv)
	
	return &API{
		server:   srv,
		sse:      transport.NewSSE(srv, nil),
		messages: transport.NewMessages(srv),
	}
}

// RegisterRoutes mounts MCP routes on the given router group.
// The caller is responsible for applying authentication middleware.
func (a *API) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/sse", a.sse.Handler())
	g.POST("/messages", a.messages.Handler())
}

// Server returns the underlying MCP server so callers can register tools.
func (a *API) Server() *server.Server {
	return a.server
}
