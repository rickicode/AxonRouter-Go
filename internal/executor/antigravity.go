package executor

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// AntigravityExecutor handles Google Antigravity (Gemini Code Assist) API.
type AntigravityExecutor struct {
	*BaseExecutor
}

// NewAntigravityExecutor creates a new Antigravity executor.
func NewAntigravityExecutor(base *BaseExecutor) *AntigravityExecutor {
	return &AntigravityExecutor{BaseExecutor: base}
}

// wrapEnvelope wraps the request body in the Antigravity envelope format.
// OmniRoute reference: open-sse/executors/antigravity.ts lines 844-858.
func (e *AntigravityExecutor) wrapEnvelope(req *Request) []byte {
	// Parse the inner request body
	var inner map[string]any
	if err := json.Unmarshal(req.Body, &inner); err != nil {
		inner = map[string]any{
			"contents": []map[string]any{
				{"role": "user", "parts": []map[string]string{{"text": "Hi"}}},
			},
		}
	}

	// Get projectId from provider-specific data
	projectId := ""
	if req.ProviderSpecificData != nil {
		projectId = req.ProviderSpecificData["projectId"]
	}

	// Build envelope (matches OmniRoute AntigravityRequestEnvelope)
	envelope := map[string]any{
		"project":            projectId,
		"requestId":          generateAntigravityRequestId(),
		"request":            inner,
		"model":              req.Model,
		"userAgent":          "antigravity",
		"requestType":        "agent",
		"enabledCreditTypes": []string{"GOOGLE_ONE_AI"},
	}

	b, _ := json.Marshal(envelope)
	return b
}

func generateAntigravityRequestId() string {
	b := make([]byte, 4)
	rand.Read(b)
	return fmt.Sprintf("agent/%d/%s", time.Now().UnixMilli(), hex.EncodeToString(b))
}

// Execute performs a non-streaming Antigravity request.
func (e *AntigravityExecutor) Execute(ctx context.Context, req *Request) (*Response, error) {
	url := req.BaseURL
	if url == "" {
		url = "https://cloudcode-pa.googleapis.com/v1internal:streamGenerateContent?alt=sse"
	}

	body := e.wrapEnvelope(req)

	headers := map[string]string{
		"Content-Type":   "application/json",
		"Authorization":  "Bearer " + req.AccessToken,
		"User-Agent":     "google-assist-cli/1.0",
		"X-Goog-Api-Key": req.APIKey,
	}

	resp, err := e.DoRequest(ctx, "POST", url, headers, body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("antigravity error %d: %s", resp.StatusCode, string(resp.Body))
	}

	return resp, nil
}

// ExecuteStream performs a streaming Antigravity request.
func (e *AntigravityExecutor) ExecuteStream(ctx context.Context, req *Request) (*StreamResult, error) {
	url := req.BaseURL
	if url == "" {
		url = "https://cloudcode-pa.googleapis.com/v1internal:streamGenerateContent?alt=sse"
	}

	body := e.wrapEnvelope(req)

	headers := map[string]string{
		"Content-Type":   "application/json",
		"Accept":         "text/event-stream",
		"Cache-Control":  "no-cache",
		"Authorization":  "Bearer " + req.AccessToken,
		"User-Agent":     "google-assist-cli/1.0",
		"X-Goog-Api-Key": req.APIKey,
	}

	return e.DoStreamRequest(ctx, "POST", url, headers, body)
}
