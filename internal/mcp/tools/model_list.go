package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/rickicode/AxonRouter-Go/internal/mcp/protocol"
	"github.com/rickicode/AxonRouter-Go/internal/models"
)

type ModelListResponse struct {
	Providers  map[string][]ModelInfo `json:"providers"`
	TotalCount int                    `json:"total_count"`
}

type ModelInfo struct {
	Provider     string   `json:"provider"`
	ID           string   `json:"id"`
	DisplayName  string   `json:"display_name"`
	ServiceKinds []string `json:"service_kinds"`
}

func CallModelList(ctx context.Context, provider string) ([]ModelInfo, error) {
	if provider == "" {
		var allModels []ModelInfo
		for _, providerKey := range models.ProviderKeys() {
			modelIDs := models.GetModelIDs(providerKey)
			displayNames := models.GetModelDisplayNames(providerKey)
			for _, modelID := range modelIDs {
				serviceKinds := models.GetModelServiceKinds(providerKey, modelID)
				allModels = append(allModels, ModelInfo{
					Provider:     providerKey,
					ID:           modelID,
					DisplayName:  displayNameForID(displayNames[modelID], modelID),
					ServiceKinds: serviceKinds,
				})
			}
		}
		return allModels, nil
	}

	modelIDs := models.GetModelIDs(provider)
	if len(modelIDs) == 0 {
		return nil, fmt.Errorf("provider not found: %s", provider)
	}

	displayNames := models.GetModelDisplayNames(provider)
	allModels := make([]ModelInfo, 0, len(modelIDs))
	for _, modelID := range modelIDs {
		allModels = append(allModels, ModelInfo{
			Provider:     provider,
			ID:           modelID,
			DisplayName:  displayNameForID(displayNames[modelID], modelID),
			ServiceKinds: models.GetModelServiceKinds(provider, modelID),
		})
	}
	return allModels, nil
}

func displayNameForID(displayName, modelID string) string {
	if displayName == "" {
		return modelID
	}
	return displayName
}

func BuildModelListResponse(models []ModelInfo) *protocol.ToolResult {
	response := ModelListResponse{
		Providers:  make(map[string][]ModelInfo),
		TotalCount: len(models),
	}

	for _, model := range models {
		provider := model.Provider
		if provider == "" {
			provider = "unknown"
		}
		response.Providers[provider] = append(response.Providers[provider], model)
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
