package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/rickicode/AxonRouter-Go/internal/mcp/protocol"
	"github.com/rickicode/AxonRouter-Go/internal/mcp/server"
)

// toolRegistry holds the registered tools.
var toolRegistry = make(map[string]ToolHandler)

// ToolHandler is the function signature for MCP tools.
type ToolHandler func(ctx context.Context, args json.RawMessage) (*protocol.ToolResult, error)

// GetToolHandler retrieves a registered tool handler.
func GetToolHandler(name string) (ToolHandler, bool) {
	handler, ok := toolRegistry[name]
	return handler, ok
}

// RegisterWebSearchTool registers the axonrouter_web_search tool.
func RegisterWebSearchTool(srv *server.Server) {
	inputSchema := `{
		"type": "object",
		"properties": {
			"query": {
				"type": "string",
				"description": "Search query string"
			},
			"max_results": {
				"type": "integer",
				"description": "Maximum number of results to return",
				"default": 10
			},
			"provider": {
				"type": "string",
				"description": "Search provider to use"
			},
			"search_type": {
				"type": "string",
				"description": "Type of search to perform",
				"default": "web"
			}
		},
		"required": ["query"]
	}`
	
	handler := func(ctx context.Context, args json.RawMessage) (*protocol.ToolResult, error) {
		var params struct {
			Query      string `json:"query"`
			MaxResults int    `json:"max_results,omitempty"`
			Provider   string `json:"provider,omitempty"`
			SearchType string `json:"search_type,omitempty"`
		}
		
		if err := json.Unmarshal(args, &params); err != nil {
			return &protocol.ToolResult{
				Content: []protocol.ToolContent{
					{
						Type:  "text",
						Text:  fmt.Sprintf("Failed to parse arguments: %v", err),
					},
				},
				IsError: true,
			}, nil
		}
		
		// Validate required parameters
		if params.Query == "" {
			return &protocol.ToolResult{
				Content: []protocol.ToolContent{
					{
						Type:  "text",
						Text:  "query parameter is required",
					},
				},
				IsError: true,
			}, nil
		}
		
		// Use default values for optional parameters
		if params.MaxResults <= 0 {
			params.MaxResults = 10
		}
		if params.SearchType == "" {
			params.SearchType = "web"
		}
		
		// Call the search service
		results, err := CallSearch(ctx, "", params.Query, params.MaxResults, params.Provider, params.SearchType)
		if err != nil {
			return &protocol.ToolResult{
				Content: []protocol.ToolContent{
					{
						Type:  "text",
						Text:  fmt.Sprintf("Search failed: %v", err),
					},
				},
				IsError: true,
			}, nil
		}
		
		return BuildSearchResponse(results), nil
	}
	
	srv.RegisterTool(protocol.Tool{
		Name:        "axonrouter_web_search",
		Description: "Perform web search using the AxonRouter internal search service",
		InputSchema: []byte(inputSchema),
	}, handler)
}

// GetAllRegisteredTools returns a list of all registered tools with their schemas.
func GetAllRegisteredTools() []protocol.Tool {
	var tools []protocol.Tool
	for name := range toolRegistry {
		tools = append(tools, protocol.Tool{
			Name: name,
		})
	}
	return tools
}
