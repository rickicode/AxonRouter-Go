package executor

import (
	"context"
	"fmt"
	"strings"
)

// GeminiExecutor handles Google Gemini API.
type GeminiExecutor struct {
	*BaseExecutor
}

// NewGeminiExecutor creates a new Gemini executor.
func NewGeminiExecutor(base *BaseExecutor) *GeminiExecutor {
	return &GeminiExecutor{BaseExecutor: base}
}

// geminiEndpoint builds the Gemini API URL for a model + action.
// Matches OmniRoute/CLIProxyAPI pattern: base URL + /models/{model}:{action}
func geminiEndpoint(baseURL, model, action string) string {
	if baseURL == "" {
		return fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:%s", model, action)
	}
	// Strip trailing slash and ?key= param (auth is via header now)
	u := strings.TrimRight(baseURL, "/")
	if idx := strings.Index(u, "?key="); idx != -1 {
		u = u[:idx]
	}
	// If URL already ends with :action, return as-is
	if strings.HasSuffix(u, ":"+action) {
		return u
	}
	return fmt.Sprintf("%s/models/%s:%s", u, model, action)
}

// geminiHeaders builds headers for Gemini API requests.
// Uses x-goog-api-key header (matches OmniRoute/CLIProxyAPI), not ?key= query param.
func geminiHeaders(apiKey, accessToken string) map[string]string {
	headers := map[string]string{
		"Content-Type": "application/json",
	}
	if apiKey != "" {
		headers["X-Goog-Api-Key"] = apiKey
	}
	if accessToken != "" {
		headers["Authorization"] = "Bearer " + accessToken
	}
	return headers
}

// Execute performs a non-streaming Gemini generateContent request.
func (e *GeminiExecutor) Execute(ctx context.Context, req *Request) (*Response, error) {
	model := ExtractModel(req.Model)
	url := geminiEndpoint(req.BaseURL, model, "generateContent")
	headers := geminiHeaders(req.APIKey, req.AccessToken)

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
	url := geminiEndpoint(req.BaseURL, model, "streamGenerateContent?alt=sse")
	headers := geminiHeaders(req.APIKey, req.AccessToken)
	headers["Accept"] = "text/event-stream"
	headers["Cache-Control"] = "no-cache"

	return e.DoStreamRequest(ctx, "POST", url, headers, req.Body)
}
