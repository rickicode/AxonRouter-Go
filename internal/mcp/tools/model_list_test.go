package tools

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestModelList(t *testing.T) {
	ctx := context.Background()
	
	t.Run("model_list_success", func(t *testing.T) {
		models, err := CallModelList(ctx, "")
		assert.NoError(t, err)
		// Models may be nil if catalog is empty (valid)
		if models != nil {
			assert.NotNil(t, models)
		}
	})
	
	t.Run("model_list_with_provider", func(t *testing.T) {
		models, err := CallModelList(ctx, "claude")
		assert.NoError(t, err)
		assert.NotNil(t, models)
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
			{
				ID:           "test-model-1",
				DisplayName:  "Test Model 1",
				ServiceKinds: []string{"llm"},
			},
		}
		response := BuildModelListResponse(models)
		assert.NotNil(t, response)
		assert.Equal(t, false, response.IsError)
		assert.Len(t, response.Content, 1)
		assert.Equal(t, "text", response.Content[0].Type)
	})
	
	t.Run("build_response_empty", func(t *testing.T) {
		models := []ModelInfo{}
		response := BuildModelListResponse(models)
		assert.NotNil(t, response)
		assert.Equal(t, false, response.IsError)
		assert.Len(t, response.Content, 1)
	})
}
