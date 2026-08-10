package tools

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWebSearch(t *testing.T) {
	ctx := context.Background()
	
	// Test 1: Successful search with valid query
	t.Run("successful_search", func(t *testing.T) {
		results, err := CallSearch(ctx, "fake-token", "test query", 5, "fake-provider", "web")
		assert.NoError(t, err)
		assert.NotEmpty(t, results)
		assert.Equal(t, 5, len(results))
		assert.Contains(t, results[0].Title, "test query")
	})
	
	// Test 2: Empty query returns empty results
	t.Run("empty_query", func(t *testing.T) {
		results, err := CallSearch(ctx, "fake-token", "", 5, "fake-provider", "web")
		assert.NoError(t, err)
		assert.Empty(t, results)
	})
	
	// Test 3: Mock search always returns mock results
	t.Run("mock_behavior", func(t *testing.T) {
		results, err := CallSearch(ctx, "fake-token", "anything", 3, "fake-provider", "web")
		assert.NoError(t, err)
		assert.Len(t, results, 3)
	})
	
	// Test 4: BuildSearchResponse
	t.Run("build_response", func(t *testing.T) {
		results := []SearchResult{
			{
				ID:     "result-1",
				Title:  "Test Result",
				URL:    "https://example.com",
				Content: "Test content",
			},
		}
		response := BuildSearchResponse(results)
		assert.NotNil(t, response)
		assert.Equal(t, false, response.IsError)
		assert.Len(t, response.Content, 1)
		assert.Equal(t, "text", response.Content[0].Type)
	})
	
	// Test 5: BuildSearchResponse with multiple results
	t.Run("build_response_multiple", func(t *testing.T) {
		results := []SearchResult{
			{
				ID:   "result-1",
				Title: "Result 1",
				URL:  "https://example.com/1",
			},
			{
				ID:   "result-2",
				Title: "Result 2",
				URL:  "https://example.com/2",
			},
		}
		response := BuildSearchResponse(results)
		assert.Len(t, response.Content, 2)
	})
}

func TestWebSearchHandlesInvalidArguments(t *testing.T) {
	ctx := context.Background()
	
	// Test that invalid arguments are handled gracefully
	t.Run("invalid_max_results", func(t *testing.T) {
		assert.NotPanics(t, func() {
			_, _ = CallSearch(ctx, "fake-token", "test", -1, "fake-provider", "web")
		})
	})
	
	t.Run("large_max_results", func(t *testing.T) {
		assert.NotPanics(t, func() {
			_, _ = CallSearch(ctx, "fake-token", "test", 1000, "fake-provider", "web")
		})
	})
}

func TestWebSearchWithProvider(t *testing.T) {
	ctx := context.Background()
	
	t.Run("different_provider", func(t *testing.T) {
		results, err := CallSearch(ctx, "fake-token", "golang", 3, "tavily", "web")
		assert.NoError(t, err)
		assert.NotEmpty(t, results)
		assert.Contains(t, results[0].Title, "golang")
	})
	
	t.Run("different_search_type", func(t *testing.T) {
		results, err := CallSearch(ctx, "fake-token", "machine learning", 3, "fake", "news")
		assert.NoError(t, err)
		assert.NotEmpty(t, results)
		assert.Contains(t, results[0].Title, "machine learning")
	})
}
