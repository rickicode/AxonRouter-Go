package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/rickicode/AxonRouter-Go/internal/mcp/protocol"
	"github.com/rickicode/AxonRouter-Go/internal/mcp/server"
)

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
						Type: "text",
						Text: fmt.Sprintf("Failed to parse arguments: %v", err),
					},
				},
				IsError: true,
			}, nil
		}

		if params.Query == "" {
			return &protocol.ToolResult{
				Content: []protocol.ToolContent{
					{
						Type: "text",
						Text: "query parameter is required",
					},
				},
				IsError: true,
			}, nil
		}

		if params.MaxResults <= 0 {
			params.MaxResults = 10
		}
		if params.SearchType == "" {
			params.SearchType = "web"
		}

		results, err := CallSearch(ctx, "", params.Query, params.MaxResults, params.Provider, params.SearchType)
		if err != nil {
			return &protocol.ToolResult{
				Content: []protocol.ToolContent{
					{
						Type: "text",
						Text: fmt.Sprintf("Search failed: %v", err),
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

func RegisterModelListTool(srv *server.Server) {
	inputSchema := `{
		"type": "object",
		"properties": {
			"provider": {
				"type": "string",
				"description": "Optional provider filter (e.g., 'claude', 'gemini', 'codex-pro')"
			}
		}
	}`

	handler := func(ctx context.Context, args json.RawMessage) (*protocol.ToolResult, error) {
		var params struct {
			Provider string `json:"provider,omitempty"`
		}

		if err := json.Unmarshal(args, &params); err != nil {
			return &protocol.ToolResult{
				Content: []protocol.ToolContent{
					{
						Type: "text",
						Text: fmt.Sprintf("Failed to parse arguments: %v", err),
					},
				},
				IsError: true,
			}, nil
		}

		models, err := CallModelList(ctx, params.Provider)
		if err != nil {
			return &protocol.ToolResult{
				Content: []protocol.ToolContent{
					{
						Type: "text",
						Text: fmt.Sprintf("Failed to list models: %v", err),
					},
				},
				IsError: true,
			}, nil
		}

		return BuildModelListResponse(models), nil
	}

	srv.RegisterTool(protocol.Tool{
		Name:        "axonrouter_model_list",
		Description: "List available models from the AxonRouter model catalog",
		InputSchema: []byte(inputSchema),
	}, handler)
}

func RegisterQuotaStatusTool(srv *server.Server) {
	inputSchema := `{
		"type": "object",
		"properties": {
			"provider": {
				"type": "string",
				"description": "Optional provider filter"
			}
		}
	}`

	handler := func(ctx context.Context, args json.RawMessage) (*protocol.ToolResult, error) {
		var params struct {
			Provider string `json:"provider,omitempty"`
		}

		if err := json.Unmarshal(args, &params); err != nil {
			return &protocol.ToolResult{
				Content: []protocol.ToolContent{
					{
						Type: "text",
						Text: fmt.Sprintf("Failed to parse arguments: %v", err),
					},
				},
				IsError: true,
			}, nil
		}

		statuses, err := CallQuotaStatus(ctx, params.Provider)
		if err != nil {
			return &protocol.ToolResult{
				Content: []protocol.ToolContent{
					{
						Type: "text",
						Text: fmt.Sprintf("Failed to get quota status: %v", err),
					},
				},
				IsError: true,
			}, nil
		}

		return BuildQuotaStatusResponse(statuses), nil
	}

	srv.RegisterTool(protocol.Tool{
		Name:        "axonrouter_quota_status",
		Description: "Get quota and cooldown status for providers",
		InputSchema: []byte(inputSchema),
	}, handler)
}
