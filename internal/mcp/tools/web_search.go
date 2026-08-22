package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/rickicode/AxonRouter-Go/internal/mcp/protocol"
)

// SearchRequest represents the internal search service request.
type SearchRequest struct {
	Query      string  `json:"query"`
	MaxResults int     `json:"max_results,omitempty"`
	Provider   string  `json:"provider,omitempty"`
	SearchType string  `json:"search_type,omitempty"`
}

// SearchResult represents a single search result.
type SearchResult struct {
	ID     string       `json:"id"`
	Title  string       `json:"title"`
	URL    string       `json:"url"`
	Score  float64      `json:"score,omitempty"`
	Content string      `json:"content"`
	Partial *PartialResult `json:"partial,omitempty"`
}

// PartialResult represents partial content for a result.
type PartialResult struct {
	Content string `json:"content"`
	SubResults []ResultInfo `json:"subResults,omitempty"`
}

// ResultInfo represents a subsection of a result.
type ResultInfo struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// CallSearch calls the internal search service.
func CallSearch(ctx context.Context, token string, query string, maxResults int, provider string, searchType string) ([]SearchResult, error) {
	// For now, we mock the search service. When the real implementation is available,
	// replace this with actual HTTP calls to the search service.
	_ = token
	_ = searchType
	
	// TODO: Replace with actual search service call
	// Example for future implementation:
	// url := fmt.Sprintf("%s/search", config.SearchServiceURL)
	// client := &http.Client{Timeout: 30 * time.Second}
	// req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	// req.Header.Set("Authorization", token)
	// resp, err := client.Do(req)
	// ...
	
	return mockSearchResults(query, maxResults, provider)
}

// mockSearchResults generates mock search results for testing.
func mockSearchResults(query string, maxResults int, provider string) ([]SearchResult, error) {
	if query == "" {
		return []SearchResult{}, nil
	}
	
	results := []SearchResult{}
	for i := 0; i < min(maxResults, 10); i++ {
		results = append(results, SearchResult{
			ID:     fmt.Sprintf("result-%d", i),
			Title:  fmt.Sprintf("%s Result %d", query, i),
			URL:    fmt.Sprintf("https://example.com/search?q=%s&page=%d", query, i),
			Score:  float64(100 - i*5),
			Content: fmt.Sprintf("This is a sample result for query: %s.", query),
		})
	}
	return results, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// BuildSearchResponse constructs an MCP result from search results.
func BuildSearchResponse(results []SearchResult) *protocol.ToolResult {
	content := make([]protocol.ToolContent, 0, len(results))
	
	for _, r := range results {
		resultJSON, _ := json.Marshal(r)
		content = append(content, protocol.ToolContent{
			Type: "text",
			Text: string(resultJSON),
		})
	}
	
	return &protocol.ToolResult{
		Content: content,
		IsError: false,
	}
}
