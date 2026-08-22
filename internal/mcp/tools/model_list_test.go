package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestModelList(t *testing.T) {
	ctx := context.Background()

	t.Run("model_list_success", func(t *testing.T) {
		models, err := CallModelList(ctx, "")
		assert.NoError(t, err)
		assert.NotEmpty(t, models)
	})

	t.Run("model_list_with_provider", func(t *testing.T) {
		models, err := CallModelList(ctx, "claude")
		assert.NoError(t, err)
		assert.NotEmpty(t, models)
		for _, model := range models {
			assert.Equal(t, "claude", model.Provider)
		}
	})

	t.Run("model_list_invalid_provider", func(t *testing.T) {
		models, err := CallModelList(ctx, "invalid-provider")
		assert.Error(t, err)
		assert.Nil(t, models)
		assert.Contains(t, err.Error(), "provider not found")
	})
}

func TestBuildModelListResponse(t *testing.T) {
	t.Run("build_response", func(t *testing.T) {
		models := []ModelInfo{
			{Provider: "claude", ID: "claude-model", DisplayName: "Claude Model", ServiceKinds: []string{"llm"}},
			{Provider: "gemini", ID: "gemini-model", DisplayName: "Gemini Model", ServiceKinds: []string{"llm", "image"}},
			{Provider: "claude", ID: "claude-image", DisplayName: "Claude Image", ServiceKinds: []string{"image"}},
		}
		response := BuildModelListResponse(models)
		assert.NotNil(t, response)
		assert.False(t, response.IsError)
		assert.Len(t, response.Content, 1)
		assert.Equal(t, "text", response.Content[0].Type)

		var got ModelListResponse
		assert.NoError(t, json.Unmarshal([]byte(response.Content[0].Text), &got))
		assert.Equal(t, len(models), got.TotalCount)
		assert.Len(t, got.Providers["claude"], 2)
		assert.Len(t, got.Providers["gemini"], 1)
	})

	t.Run("build_response_unknown_provider", func(t *testing.T) {
		response := BuildModelListResponse([]ModelInfo{{ID: "unknown-model", ServiceKinds: []string{"llm"}}})
		var got ModelListResponse
		assert.NoError(t, json.Unmarshal([]byte(response.Content[0].Text), &got))
		assert.Len(t, got.Providers["unknown"], 1)
		assert.Equal(t, 1, got.TotalCount)
	})

	t.Run("build_response_empty", func(t *testing.T) {
		response := BuildModelListResponse([]ModelInfo{})
		assert.NotNil(t, response)
		assert.False(t, response.IsError)
		assert.Len(t, response.Content, 1)
	})
}
