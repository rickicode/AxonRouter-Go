package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/rickicode/AxonRouter-Go/internal/mcp/protocol"
	"github.com/rickicode/AxonRouter-Go/internal/mcp/server"
)

var WebSearchTool = protocol.Tool{
	Name:        "axonrouter_web_search",
	Description: "Search the web using AxonRouter's search capabilities",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {"type": "string", "description": "Search query (required)"},
			"max_results": {"type": "number", "description": "Maximum number of results (default 10)", "default": 10},
			"provider": {"type": "string", "description": "Optional provider filter"},
			"search_type": {"type": "string", "description": "Search type (default 'web')", "default": "web"}
		},
		"required": ["query"]
	}`),
}

func WebSearchHandler(ctx context.Context, args json.RawMessage) (*protocol.ToolResult, error) {
	var params struct {
		Query      string `json:"query"`
		MaxResults int    `json:"max_results"`
		Provider   string `json:"provider"`
		SearchType string `json:"search_type"`
	}

	if err := json.Unmarshal(args, &params); err != nil {
		errResult := &protocol.ToolResult{
			Content: []protocol.ToolContent{
				{
					Type: "text",
					Text: fmt.Sprintf("invalid search parameters: %s", err.Error()),
				},
			},
			IsError: true,
		}
		return errResult, fmt.Errorf("invalid parameters: %w", err)
	}

	if params.Query == "" {
		errResult := &protocol.ToolResult{
			Content: []protocol.ToolContent{
				{
					Type: "text",
					Text: "query is required",
				},
			},
			IsError: true,
		}
		return errResult, fmt.Errorf("query is required")
	}

	if params.MaxResults <= 0 {
		params.MaxResults = 10
	}
	if params.SearchType == "" {
		params.SearchType = "web"
	}

	logging.Logger.Info("search request received", "query", params.Query, "max_results", params.MaxResults, "provider", params.Provider, "search_type", params.SearchType)

	mockResults := buildMockSearchResults(params.Query, params.MaxResults)
	logging.Logger.Info("search completed", "query", params.Query, "results_count", len(mockResults))

	return &protocol.ToolResult{
		Content: []protocol.ToolContent{
			{
				Type: "text",
				Text: mockResults,
			},
		},
		IsError: false,
	}, nil
}

func buildMockSearchResults(query string, maxResults int) string {
	results := []map[string]interface{}{
		{
			"title":       query + " - Official Documentation",
			"link":        "https://example.com/docs/" + query,
			"snippet":     "Comprehensive documentation and guides for " + query,
			"score":       1.0,
		},
		{
			"title":       query + " - Best Practices",
			"link":        "https://example.com/best-practices/" + query,
			"snippet":     "Expert tips and best practices for using " + query,
			"score":       0.95,
		},
		{
			"title":       "How to " + query + " - Step by Step",
			"link":        "https://example.com/tutorials/" + query,
			"snippet":     "Learn how to " + query + " with clear step-by-step instructions",
			"score":       0.9,
		},
		{
			"title":       query + " - Compare & Reviews",
			"link":        "https://example.com/reviews/" + query,
			"snippet":     "Compare different " + query + " solutions and read user reviews",
			"score":       0.85,
		},
		{
			"title":       "What is " + query + "?",
			"link":        "https://example.com/what-is/" + query,
			"snippet":     "Understand what " + query + " is and why it matters",
			"score":       0.8,
		},
	}

	if maxResults < len(results) {
		results = results[:maxResults]
	}

	response := map[string]interface{}{
		"object": "list",
		"data":   results,
	}

	jsonBytes, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return `{"error": "failed to marshal results"}`
	}

	return string(jsonBytes)
}

func RegisterToolHandler(srv *server.Server) error {
	if srv == nil {
		return fmt.Errorf("server is nil")
	}

	srv.RegisterTool(WebSearchTool, WebSearchHandler)
	logging.Logger.Info("registered tool", "tool", "axonrouter_web_search")
	return nil
}
