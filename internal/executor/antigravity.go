package executor

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// Fields that must not reach the Google Antigravity API.
// OmniRoute strips these via destructuring (antigravity.ts:832-844).
// Google rejects them with 400 "oneOf at / not met" or "Unknown name".
var antigravityStripFields = []string{
	"thinking", "reasoning_effort", "reasoning",
	"enable_thinking", "thinking_budget",
	"output_config", "output_format",
}

// maxAntigravityOutputTokens is the hard ceiling on generationConfig.maxOutputTokens.
// OmniRoute MAX_ANTIGRAVITY_OUTPUT_TOKENS (antigravity.ts:527).
const maxAntigravityOutputTokens = 16384

// defaultSafetySettings turns off all Google content safety filters to prevent
// false-positive blocks on benign technical prompts.
// OmniRoute DEFAULT_SAFETY_SETTINGS (geminiHelper.ts:90).
var defaultSafetySettings = []map[string]string{
	{"category": "HARM_CATEGORY_HATE_SPEECH", "threshold": "OFF"},
	{"category": "HARM_CATEGORY_DANGEROUS_CONTENT", "threshold": "OFF"},
	{"category": "HARM_CATEGORY_SEXUALLY_EXPLICIT", "threshold": "OFF"},
	{"category": "HARM_CATEGORY_HARASSMENT", "threshold": "OFF"},
	{"category": "HARM_CATEGORY_CIVIC_INTEGRITY", "threshold": "OFF"},
}

// AntigravityExecutor handles Google Antigravity (Gemini Code Assist) API.
type AntigravityExecutor struct {
	*BaseExecutor
}

// NewAntigravityExecutor creates a new Antigravity executor.
func NewAntigravityExecutor(base *BaseExecutor) *AntigravityExecutor {
	return &AntigravityExecutor{BaseExecutor: base}
}

// sanitizeRequest strips fields Google rejects and applies generation defaults.
func sanitizeRequest(inner map[string]any) {
	// Strip thinking/reasoning and Anthropic-only fields (OmniRoute destructuring)
	for _, f := range antigravityStripFields {
		delete(inner, f)
	}

	// Cap maxOutputTokens (OmniRoute applyAntigravityGenerationDefaults)
	if gc, ok := inner["generationConfig"].(map[string]any); ok {
		if v, ok := gc["maxOutputTokens"].(float64); ok && v > maxAntigravityOutputTokens {
			gc["maxOutputTokens"] = maxAntigravityOutputTokens
		}
	}

	// Default safety settings if caller didn't provide them
	if _, ok := inner["safetySettings"]; !ok {
		inner["safetySettings"] = defaultSafetySettings
	}
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

	sanitizeRequest(inner)

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
		"User-Agent":     "antigravity",
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
		"User-Agent":     "antigravity",
		"X-Goog-Api-Key": req.APIKey,
	}

	return e.DoStreamRequest(ctx, "POST", url, headers, body)
}
