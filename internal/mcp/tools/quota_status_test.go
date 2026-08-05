package tools

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestQuotaStatus(t *testing.T) {
	ctx := context.Background()
	
	t.Run("quota_status_success", func(t *testing.T) {
		statuses, err := CallQuotaStatus(ctx, "")
		assert.NoError(t, err)
		assert.NotNil(t, statuses)
	})
	
	t.Run("quota_status_with_provider", func(t *testing.T) {
		statuses, err := CallQuotaStatus(ctx, "unknown")
		assert.NoError(t, err)
		assert.NotNil(t, statuses)
	})
}

func TestBuildQuotaStatusResponse(t *testing.T) {
	t.Run("build_response", func(t *testing.T) {
		statuses := []ProviderStatus{
			{
				Provider:    "test-provider",
				RateLimited: false,
				Exhausted:   false,
			},
		}
		response := BuildQuotaStatusResponse(statuses)
		assert.NotNil(t, response)
		assert.Equal(t, false, response.IsError)
		assert.Len(t, response.Content, 1)
		assert.Equal(t, "text", response.Content[0].Type)
	})
	
	t.Run("build_response_empty", func(t *testing.T) {
		statuses := []ProviderStatus{}
		response := BuildQuotaStatusResponse(statuses)
		assert.NotNil(t, response)
		assert.Equal(t, false, response.IsError)
		assert.Len(t, response.Content, 1)
	})
}
