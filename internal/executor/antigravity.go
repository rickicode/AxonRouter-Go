package executor

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Fields that must not reach the Google Antigravity API.
// OmniRoute strips these via destructuring (antigravity.ts:832-844).
// Google rejects them with 400 "oneOf at / not met" or "Unknown name".
var antigravityStripFields = []string{
	"thinking",
	"reasoning_effort",
	"reasoning",
	"enable_thinking",
	"thinking_budget",
	"output_config",
	"output_format",
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

// antigravityDiscoveryBaseURLs mirrors the OAuth-time loadCodeAssist endpoints.
// Order matches OmniRoute ANTIGRAVITY_BASE_URLS for the same fallback behavior.
var antigravityDiscoveryBaseURLs = []string{
	"https://daily-cloudcode-pa.googleapis.com",
	"https://cloudcode-pa.googleapis.com",
	"https://daily-cloudcode-pa.sandbox.googleapis.com",
}

// antigravityProjectCache memoizes loadCodeAssist results per access token.
// This avoids repeated discovery round-trips within the process lifetime.
var antigravityProjectCache sync.Map

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

// normalizeStringMap coerces generic map values to map[string]any.
func normalizeStringMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

// normalizeAntigravityContents fixes Gemini-compatible Cloud Code request contents.
// Mirrors OmniRoute antigravity.ts:646-681:
//   - forces role="user" for any turn containing a functionResponse part
//   - strips empty text parts only when they carry no other payload
//   - strips functionCall parts without a name
//   - strips thought parts and thoughtSignature markers (keeps bypass sentinel when function calls are present)
//   - merges consecutive turns with the same role
func normalizeAntigravityContents(inner map[string]any) {
	rawContents, ok := inner["contents"].([]any)
	if !ok || len(rawContents) == 0 {
		return
	}

	var normalized []map[string]any
	for _, item := range rawContents {
		c := normalizeStringMap(item)
		if c == nil {
			continue
		}

		role := "user"
		if r, ok := c["role"].(string); ok && strings.TrimSpace(r) != "" {
			role = r
		}

		rawParts, _ := c["parts"].([]any)

		// Detect whether this turn has a functionCall, so we can keep the
		// skip_thought_signature_validator sentinel on tool-call turns.
		hasFunctionCall := false
		for _, p := range rawParts {
			pm := normalizeStringMap(p)
			if pm == nil {
				continue
			}
			if _, ok := pm["functionCall"].(map[string]any); ok {
				hasFunctionCall = true
				break
			}
		}

		// functionResponse turns must be addressed to role "user".
		for _, p := range rawParts {
			pm := normalizeStringMap(p)
			if pm == nil {
				continue
			}
			if _, ok := pm["functionResponse"].(map[string]any); ok {
				role = "user"
				break
			}
		}

		var filtered []map[string]any
		for _, p := range rawParts {
			pm := normalizeStringMap(p)
			if pm == nil {
				continue
			}

			// Drop malformed functionCalls missing a name.
			if fc, ok := pm["functionCall"].(map[string]any); ok {
				name, _ := fc["name"].(string)
				if strings.TrimSpace(name) == "" {
					continue
				}
			}

			// Drop raw thought parts.
			if thought, ok := pm["thought"].(bool); ok && thought {
				continue
			}

			// Drop thoughtSignature unless this is a tool-call turn carrying the bypass sentinel.
			if sig, ok := pm["thoughtSignature"].(string); ok && sig != "" {
				if sig != "skip_thought_signature_validator" || !hasFunctionCall {
					continue
				}
			}

			// Drop empty text parts only if they carry no function/tool payload.
			if text, ok := pm["text"].(string); ok && strings.TrimSpace(text) == "" {
				_, hasFC := pm["functionCall"].(map[string]any)
				_, hasFR := pm["functionResponse"].(map[string]any)
				_, hasID := pm["inlineData"].(map[string]any)
				if !hasFC && !hasFR && !hasID {
					continue
				}
			}

			filtered = append(filtered, pm)
		}

		if len(filtered) == 0 {
			continue
		}

		// Merge consecutive turns with the same role.
		if len(normalized) > 0 {
			last := normalized[len(normalized)-1]
			lastRole, _ := last["role"].(string)
			if lastRole == role {
				lastParts, _ := last["parts"].([]map[string]any)
				lastParts = append(lastParts, filtered...)
				last["parts"] = lastParts
				continue
			}
		}

		normalized = append(normalized, map[string]any{
			"role":  role,
			"parts": filtered,
		})
	}

	if len(normalized) > 0 {
		inner["contents"] = normalized
	}
}

// injectToolConfig adds Gemini function-calling mode when tools are present.
// OmniRoute antigravity.ts:696-699 sets functionCallingConfig.mode = "VALIDATED".
func injectToolConfig(inner map[string]any) {
	tools, ok := inner["tools"].([]any)
	if ok && len(tools) > 0 {
		inner["toolConfig"] = map[string]any{
			"functionCallingConfig": map[string]any{
				"mode": "VALIDATED",
			},
		}
	}
}

// envelopeUserAgent returns "antigravity" or "jetski" based on account/client profile.
// Mirrors OmniRoute getAntigravityEnvelopeUserAgent (antigravityIdentity.ts:65-68).
func envelopeUserAgent(req *Request) string {
	clientProfile := ""
	email := ""
	if req.ProviderSpecificData != nil {
		clientProfile = strings.ToLower(req.ProviderSpecificData["clientProfile"])
		email = req.ProviderSpecificData["email"]
	}
	if clientProfile == "harness" {
		return "jetski"
	}
	if email != "" && !strings.HasSuffix(email, "@gmail.com") && !strings.HasSuffix(email, "@googlemail.com") {
		return "jetski"
	}
	return "antigravity"
}

// discoverProjectID auto-discovers the Google Cloud project via loadCodeAssist.
// OmniRoute antigravity.ts:588-591 + antigravityProjectBootstrap.ts.
// The result is memoized per access token for the process lifetime.
func (e *AntigravityExecutor) discoverProjectID(ctx context.Context, accessToken, clientProfile string) (string, error) {
	if accessToken == "" {
		return "", fmt.Errorf("no access token for Antigravity project discovery")
	}

	cacheKey := clientProfile + ":" + accessToken
	if v, ok := antigravityProjectCache.Load(cacheKey); ok {
		return v.(string), nil
	}

	headers := map[string]string{
		"Content-Type":    "application/json",
		"User-Agent":      "vscode/1.X.X (Antigravity/4.2.0)",
		"Client-Metadata": `{"ideType":"ANTIGRAVITY"}`,
	}
	if clientProfile == "harness" {
		headers["User-Agent"] = "antigravity"
	}

	body := []byte(`{"metadata":{"ideType":"ANTIGRAVITY"}}`)

	for _, base := range antigravityDiscoveryBaseURLs {
		url := base + "/v1internal:loadCodeAssist"
		discoveryCtx, cancel := context.WithTimeout(ctx, 8*time.Second)

		client, targetURL, err := e.clientForContext(discoveryCtx, url, headers)
		if err != nil {
			cancel()
			continue
		}

		req, _ := http.NewRequestWithContext(discoveryCtx, http.MethodPost, targetURL, bytes.NewReader(body))
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)

		resp, err := client.Do(req)
		if err != nil {
			cancel()
			continue
		}

		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		cancel()

		if resp.StatusCode != http.StatusOK {
			continue
		}

		var data map[string]any
		if err := json.Unmarshal(respBody, &data); err != nil {
			continue
		}

		projectID := pickAntigravityProjectID(data)
		if projectID != "" {
			antigravityProjectCache.Store(cacheKey, projectID)
			return projectID, nil
		}
	}

	return "", fmt.Errorf("could not discover Antigravity projectId via loadCodeAssist")
}

// pickAntigravityProjectID extracts the project id from a loadCodeAssist response.
// cloudaicompanionProject may be a plain string or an object with an id field.
func pickAntigravityProjectID(data map[string]any) string {
	raw, ok := data["cloudaicompanionProject"]
	if !ok {
		return ""
	}
	if s, ok := raw.(string); ok {
		return strings.TrimSpace(s)
	}
	if obj, ok := raw.(map[string]any); ok {
		if id, ok := obj["id"].(string); ok {
			return strings.TrimSpace(id)
		}
	}
	return ""
}

// wrapEnvelope wraps the request body in the Antigravity envelope format.
// OmniRoute reference: open-sse/executors/antigravity.ts lines 580-758.
func (e *AntigravityExecutor) wrapEnvelope(ctx context.Context, req *Request) ([]byte, error) {
	// Parse the inner request body
	var inner map[string]any
	if err := json.Unmarshal(req.Body, &inner); err != nil {
		inner = map[string]any{
			"contents": []map[string]any{
				{"role": "user", "parts": []map[string]string{{"text": "Hi"}}},
			},
		}
	}

	normalizeAntigravityContents(inner)
	injectToolConfig(inner)
	sanitizeRequest(inner)

	// Get projectId from provider-specific data, or auto-discover if missing.
	projectID := ""
	clientProfile := ""
	if req.ProviderSpecificData != nil {
		projectID = req.ProviderSpecificData["projectId"]
		clientProfile = strings.ToLower(req.ProviderSpecificData["clientProfile"])
	}
	if projectID == "" && req.AccessToken != "" {
		discovered, err := e.discoverProjectID(ctx, req.AccessToken, clientProfile)
		if err == nil && discovered != "" {
			projectID = discovered
			req.ProviderSpecificData["projectId"] = discovered
		}
	}

	// Build envelope (matches OmniRoute AntigravityRequestEnvelope)
	envelope := map[string]any{
		"project":            projectID,
		"requestId":          generateAntigravityRequestId(),
		"request":            inner,
		"model":              req.Model,
		"userAgent":          envelopeUserAgent(req),
		"requestType":        "agent",
		"enabledCreditTypes": []string{"GOOGLE_ONE_AI"},
	}

	b, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("marshal antigravity envelope: %w", err)
	}
	return b, nil
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
	body, err := e.wrapEnvelope(ctx, req)
	if err != nil {
		return nil, err
	}
	headers := map[string]string{
		"Content-Type":   "application/json",
		"Authorization":  "Bearer " + req.AccessToken,
		"User-Agent":     envelopeUserAgent(req),
		"X-Goog-Api-Key": req.APIKey,
	}
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

// ExecuteStream performs a streaming Antigravity request.
func (e *AntigravityExecutor) ExecuteStream(ctx context.Context, req *Request) (*StreamResult, error) {
	url := req.BaseURL
	if url == "" {
		url = "https://cloudcode-pa.googleapis.com/v1internal:streamGenerateContent?alt=sse"
	}
	body, err := e.wrapEnvelope(ctx, req)
	if err != nil {
		return nil, err
	}
	headers := map[string]string{
		"Content-Type":   "application/json",
		"Accept":         "text/event-stream",
		"Cache-Control":  "no-cache",
		"Authorization":  "Bearer " + req.AccessToken,
		"User-Agent":     envelopeUserAgent(req),
		"X-Goog-Api-Key": req.APIKey,
	}
	return e.DoStreamRequest(ctx, "POST", url, headers, body)
}
