package executor

import (
	"context"
	"fmt"
	"strings"

	"github.com/rickicode/AxonRouter-Go/internal/signature"
)

// GeminiInteractionsExecutor handles Google's native Interactions API.
// It reuses the shared HTTP plumbing from BaseExecutor and targets the
// /v1beta/interactions endpoint instead of the generateContent path.
type GeminiInteractionsExecutor struct {
	*BaseExecutor
}

// NewGeminiInteractionsExecutor creates a new Interactions executor.
func NewGeminiInteractionsExecutor(base *BaseExecutor) *GeminiInteractionsExecutor {
	return &GeminiInteractionsExecutor{BaseExecutor: base}
}

// geminiInteractionsEndpoint builds the Interactions API URL.
func geminiInteractionsEndpoint(baseURL string) string {
	if baseURL == "" {
		return fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/interactions")
	}
	u := strings.TrimRight(baseURL, "/")
	if idx := strings.Index(u, "?key="); idx != -1 {
		u = u[:idx]
	}
	if strings.HasSuffix(u, "/interactions") {
		return u
	}
	return fmt.Sprintf("%s/interactions", u)
}

// geminiInteractionsHeaders builds headers for Interactions API requests.
func geminiInteractionsHeaders(apiKey, accessToken string) map[string]string {
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

// Execute performs a non-streaming Interactions request.
func (e *GeminiInteractionsExecutor) Execute(ctx context.Context, req *Request) (*Response, error) {
	url := geminiInteractionsEndpoint(req.BaseURL)
	headers := geminiInteractionsHeaders(req.APIKey, req.AccessToken)
	body := signature.SanitizeGeminiRequestThoughtSignatures(req.Body, "input")

	resp, err := e.DoRequest(ctx, "POST", url, headers, body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		upErr := &UpstreamError{
			StatusCode: resp.StatusCode,
			Body:       resp.Body,
			RawBody:    resp.Body,
			Headers:    resp.Headers,
		}
		upErr.TranslateErrorBody(req.Provider)
		return nil, upErr
	}

	return resp, nil
}

// ExecuteStream performs a streaming Interactions request.
func (e *GeminiInteractionsExecutor) ExecuteStream(ctx context.Context, req *Request) (*StreamResult, error) {
	url := geminiInteractionsEndpoint(req.BaseURL)
	headers := geminiInteractionsHeaders(req.APIKey, req.AccessToken)
	headers["Accept"] = "text/event-stream"
	headers["Cache-Control"] = "no-cache"
	body := signature.SanitizeGeminiRequestThoughtSignatures(req.Body, "input")

	result, err := e.DoStreamRequest(ContextWithProvider(ctx, req.Provider), "POST", url, headers, body)
	return result, err
}
