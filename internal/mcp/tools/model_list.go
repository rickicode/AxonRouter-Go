package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/rickicode/AxonRouter-Go/internal/mcp/protocol"
	"github.com/rickicode/AxonRouter-Go/internal/models"
)

type ModelListResponse struct {
	Providers map[string][]ModelInfo `json:"providers"`
	TotalCount int                   `json:"total_count"`
}

type ModelInfo struct {
	ID           string   `json:"id"`
	DisplayName  string   `json:"display_name"`
	ServiceKinds []string `json:"service_kinds"`
}

func CallModelList(ctx context.Context, provider string) ([]ModelInfo, error) {
	if provider == "" {
		providers := make(map[string][]ModelInfo)
		displayNames := models.GetModelDisplayNames("")
		
		for providerKey := range displayNames {
			modelIDs := models.GetModelIDs(providerKey)
			for _, modelID := range modelIDs {
				displayName := displayNameForID(displayNames[modelID], modelID)
				serviceKinds := models.GetModelServiceKinds(providerKey, modelID)
				
				if len(serviceKinds) == 0 {
					continue
				}
				
				model := ModelInfo{
					ID:           modelID,
					DisplayName:  displayName,
					ServiceKinds: serviceKinds,
				}
				providers[providerKey] = append(providers[providerKey], model)
			}
		}
		
		var allModels []ModelInfo
		for _, providerModels := range providers {
			allModels = append(allModels, providerModels...)
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
		displayName := displayNameForID(displayNames[modelID], modelID)
		serviceKinds := models.GetModelServiceKinds(provider, modelID)
		allModels = append(allModels, ModelInfo{
			ID:           modelID,
			DisplayName:  displayName,
			ServiceKinds: serviceKinds,
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
		Providers:    make(map[string][]ModelInfo),
		TotalCount:   len(models),
	}
	
	for _, model := range models {
		provider := getProviderFromServiceKinds(model.ServiceKinds)
		if provider == "" {
			provider = "unknown"
		}
		response.Providers[provider] = append(response.Providers[provider], model)
		if len(response.Providers[provider]) == 1 {
			response.TotalCount++
		}
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

func getProviderFromServiceKinds(serviceKinds []string) string {
	for _, kind := range serviceKinds {
		switch kind {
		case "llm":
			return "claude"
		case "image":
			return "unknown"
		case "embedding":
			return "unknown"
		case "stt":
			return "unknown"
		case "tts":
			return "unknown"
		}
	}
	return ""
}
