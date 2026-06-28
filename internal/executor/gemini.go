package executor

import (
	"context"
	"encoding/json"
	"fmt"
)

// GeminiExecutor handles Google Gemini API.
type GeminiExecutor struct {
	*BaseExecutor
}

// NewGeminiExecutor creates a new Gemini executor.
func NewGeminiExecutor(base *BaseExecutor) *GeminiExecutor {
	return &GeminiExecutor{BaseExecutor: base}
}

// Execute performs a non-streaming Gemini generateContent request.
func (e *GeminiExecutor) Execute(ctx context.Context, req *Request) (*Response, error) {
	model := ExtractModel(req.Model)
	url := req.BaseURL
	if url == "" {
		url = fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", model, req.APIKey)
	}

	headers := map[string]string{
		"Content-Type": "application/json",
	}
	// Gemini uses query param for API key, but Bearer also works
	if req.AccessToken != "" {
		headers["Authorization"] = "Bearer " + req.AccessToken
	}

	resp, err := e.DoRequest(ctx, "POST", url, headers, req.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("gemini error %d: %s", resp.StatusCode, string(resp.Body))
	}

	return resp, nil
}

// ExecuteStream performs a streaming Gemini streamGenerateContent request.
func (e *GeminiExecutor) ExecuteStream(ctx context.Context, req *Request) (*StreamResult, error) {
	model := ExtractModel(req.Model)
	url := req.BaseURL
	if url == "" {
		url = fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:streamGenerateContent?key=%s", model, req.APIKey)
	}

	headers := map[string]string{
		"Content-Type":  "application/json",
		"Accept":        "text/event-stream",
		"Cache-Control": "no-cache",
	}
	if req.AccessToken != "" {
		headers["Authorization"] = "Bearer " + req.AccessToken
	}

	return e.DoStreamRequest(ctx, "POST", url, headers, req.Body)
}

// parseGeminiUsage extracts token usage from a Gemini response.
func parseGeminiUsage(body []byte) (inputTokens, outputTokens int64) {
	var resp struct {
		UsageMetadata *struct {
			PromptTokenCount     int64 `json:"promptTokenCount"`
			CandidatesTokenCount int64 `json:"candidatesTokenCount"`
			TotalTokenCount      int64 `json:"totalTokenCount"`
		} `json:"usageMetadata"`
	}
	if err := json.Unmarshal(body, &resp); err != nil || resp.UsageMetadata == nil {
		return 0, 0
	}
	return resp.UsageMetadata.PromptTokenCount, resp.UsageMetadata.CandidatesTokenCount
}
