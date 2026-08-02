package tools

import (
	"context"
	"encoding/json"

	"github.com/rickicode/AxonRouter-Go/internal/mcp/protocol"
)

type QuotaStatusResponse struct {
	Providers map[string]ProviderStatus `json:"providers"`
	TotalQuotaProviders int             `json:"total_quota_providers"`
}

type ProviderStatus struct {
	Provider    string          `json:"provider"`
	RateLimited bool            `json:"rate_limited"`
	CooldownUntil *string       `json:"cooldown_until,omitempty"`
	Exhausted   bool            `json:"exhausted"`
	Consumed    int64           `json:"consumed,omitempty"`
	Remaining   int64           `json:"remaining,omitempty"`
	Total       int64           `json:"total,omitempty"`
}

func CallQuotaStatus(ctx context.Context, provider string) ([]ProviderStatus, error) {
	if provider == "" {
		return []ProviderStatus{
			{
				Provider:    "unknown",
				RateLimited: false,
				Exhausted:   false,
			},
		}, nil
	}
	
	return []ProviderStatus{
		{
			Provider:    provider,
			RateLimited: false,
			Exhausted:   false,
		},
	}, nil
}

func BuildQuotaStatusResponse(statuses []ProviderStatus) *protocol.ToolResult {
	response := QuotaStatusResponse{
		Providers:          make(map[string]ProviderStatus),
		TotalQuotaProviders: len(statuses),
	}
	
	for _, status := range statuses {
		response.Providers[status.Provider] = status
	}
	
	contentJSON, _ := json.MarshalIndent(response, "", "  ")
	return &protocol.ToolResult{
		Content: []protocol.ToolContent{
			{
				Type: "text",
				Text: string(contentJSON),
			},
		},
		IsError: false,
	}
}
