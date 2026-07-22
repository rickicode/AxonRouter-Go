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
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
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

// antigravityModelAliases maps public/client-facing model IDs to upstream-valid
// Antigravity model IDs. Kept minimal after the catalog cleanup; primarily for
// backward compatibility and retired IDs that external clients or configs still use.
// Sources: OmniRoute open-sse/config/antigravityModelAliases.ts + CLIProxyAPI model list.
var antigravityModelAliases = map[string]string{
	// OmniRoute forward aliases (kept for backward compatibility even after catalog cleanup)
	"gemini-3-pro-preview":                    "gemini-3.1-pro",
	"gemini-3-pro-image-preview":                "gemini-3-pro-image",
	"gemini-2.5-computer-use-preview-10-2025":   "rev19-uic3-1p",
	// Resilience alias: older client configs may still reference the plain Pro ID
	"gemini-3.1-pro":                            "gemini-pro-agent",
}

// antigravityProFallbackChains provides per-request upstream-id retries for the
// Gemini Pro family, which Antigravity renames frequently. Only HTTP 400 from an
// invalid upstream id triggers the next candidate; rate-limit/quota errors are
// handled by the normal failover path.
// See OmniRoute open-sse/config/antigravityModelAliases.ts:208-218.
var antigravityProFallbackChains = map[string][]string{
	"gemini-3.1-pro-high": {"gemini-3.1-pro-high", "gemini-pro-agent", "gemini-3-pro-high"},
	"gemini-3.1-pro-low":  {"gemini-3.1-pro-low", "gemini-3-pro-low"},
}

// resolveAntigravityModelID resolves a public model ID to the upstream ID that
// should be sent to Antigravity. It follows alias chains (e.g. preview -> public
// -> upstream) and stops when no further mapping exists or a cycle is detected.
func resolveAntigravityModelID(modelID string) string {
	seen := map[string]bool{}
	for {
		if seen[modelID] {
			return modelID
		}
		seen[modelID] = true
		v, ok := antigravityModelAliases[modelID]
		if !ok {
			return modelID
		}
		modelID = v
	}
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

// normalizeAntigravityToolKeys renames translator-internal tool keys to the
// snake-case keys the upstream Antigravity/Gemini API expects.
//   - parametersJsonSchema -> parameters
//   - functionDeclarations -> function_declarations
// The translator uses parametersJsonSchema as an internal representation so it
// does not collide with OpenAI's tools[].function.parameters. Antigravity
// expects the Gemini-native "parameters" key under "function_declarations".
func normalizeAntigravityToolKeys(v any) {
	switch val := v.(type) {
	case map[string]any:
		if ps, ok := val["parametersJsonSchema"]; ok {
			val["parameters"] = ps
			delete(val, "parametersJsonSchema")
		}
		if fds, ok := val["functionDeclarations"]; ok {
			val["function_declarations"] = fds
			delete(val, "functionDeclarations")
		}
		for _, child := range val {
			normalizeAntigravityToolKeys(child)
		}
	case []any:
		for _, item := range val {
			normalizeAntigravityToolKeys(item)
		}
	case []map[string]any:
		for _, item := range val {
			normalizeAntigravityToolKeys(item)
		}
	}
}

// antigravityUnsupportedSchemaKeywords are JSON Schema keywords the upstream
// Antigravity/Gemini API does not accept inside tool parameter schemas.
// Mirrors CLIProxyAPI internal/util/gemini_schema.go.
var antigravityUnsupportedSchemaKeywords = map[string]bool{
	"$schema": true, "$id": true, "$comment": true, "$ref": true,
	"$defs": true, "definitions": true,
	"propertyNames": true, "patternProperties": true, "additionalProperties": true,
	"enumDescriptions": true, "enumTitles": true,
	"prefill": true, "deprecated": true,
	"format": true, "default": true, "examples": true,
	"minLength": true, "maxLength": true,
	"minItems": true, "maxItems": true, "uniqueItems": true,
	"pattern": true, "exclusiveMinimum": true, "exclusiveMaximum": true,
	"nullable": true, "title": true, "const": true,
}

// sanitizeAntigravityJSONSchema recursively strips unsupported JSON Schema
// keywords from the request body. This is applied to the whole inner request;
// the blacklist only overlaps with tool schemas, so message contents etc. are
// unaffected.
func sanitizeAntigravityJSONSchema(v any) {
	switch val := v.(type) {
	case map[string]any:
		for k := range val {
			if antigravityUnsupportedSchemaKeywords[k] {
				delete(val, k)
				continue
			}
			if strings.HasPrefix(k, "x-") {
				delete(val, k)
				continue
			}
		}
		for _, child := range val {
			sanitizeAntigravityJSONSchema(child)
		}
	case []any:
		for _, item := range val {
			sanitizeAntigravityJSONSchema(item)
		}
	case []map[string]any:
		for _, item := range val {
			sanitizeAntigravityJSONSchema(item)
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

// wrapEnvelope takes the Antigravity request envelope produced by the translator
// and finalizes it: project id, request id/session id, and provider-specific
// request normalization. The translator already emits {"request":{...},"model":...};
// the executor must NOT wrap it again.
// Reference: CLIProxyAPI geminiToAntigravity + AntigravityRequestEnvelope.
func (e *AntigravityExecutor) wrapEnvelope(ctx context.Context, req *Request) ([]byte, error) {
	return e.buildEnvelope(ctx, req, resolveAntigravityModelID(req.Model))
}

// buildEnvelope finalizes the Antigravity envelope for a specific upstream model id.
func (e *AntigravityExecutor) buildEnvelope(ctx context.Context, req *Request, upstreamModelID string) ([]byte, error) {
	var envelope map[string]any
	if err := json.Unmarshal(req.Body, &envelope); err != nil {
		envelope = map[string]any{
			"request": map[string]any{
				"contents": []map[string]any{
					{"role": "user", "parts": []map[string]string{{"text": "Hi"}}},
				},
			},
		}
	}

	// Operate on the inner Gemini request ('request' key), not on the envelope itself.
	// If the translator emitted a plain inner request (older/caller tests), wrap it.
	inner, _ := envelope["request"].(map[string]any)
	if inner == nil {
		inner = envelope
		envelope = map[string]any{
			"request": inner,
		}
	}

	normalizeAntigravityContents(inner)
	injectToolConfig(inner)
	sanitizeRequest(inner)

	// CLIProxyAPI removes per-request safety settings from the inner request;
	// the envelope-level safety handling is implied by the same defaults.
	delete(inner, "safetySettings")

	// The translator stores tool schemas under parametersJsonSchema and
	// functionDeclarations so they do not collide with OpenAI keys. Antigravity
	// upstream expects the Gemini-native snake-case keys, so normalize them
	// before sending upstream.
	normalizeAntigravityToolKeys(inner)

	// Strip JSON Schema keywords the upstream API rejects (e.g. $schema,
	// propertyNames, patternProperties, format, x-* extensions).
	sanitizeAntigravityJSONSchema(inner)

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

	// Finalize the envelope. The translator already built the outer shape.
	envelope["project"] = projectID
	envelope["model"] = upstreamModelID
	envelope["userAgent"] = envelopeUserAgent(req)
	envelope["requestType"] = "agent"
	envelope["enabledCreditTypes"] = []string{"GOOGLE_ONE_AI"}
	envelope["requestId"] = generateAntigravityRequestId()
	// Stable per-conversation session id inside the inner request, matching CLIProxyAPI.
	envelope["request"] = inner
	if contents, ok := inner["contents"].([]any); ok && len(contents) > 0 {
		first := normalizeStringMap(contents[0])
		if first != nil {
			if text := normalizeStringMapValue(first["parts"]); len(text) > 0 {
				if t := normalizeStringMap(text[0]); t != nil {
					if s, _ := t["text"].(string); s != "" {
						inner["sessionId"] = generateStableAntigravitySessionID(s)
					}
				}
			}
		}
	}
	if _, ok := inner["sessionId"].(string); !ok {
		inner["sessionId"] = uuid.NewString()
	}

	b, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("marshal antigravity envelope: %w", err)
	}
	return b, nil
}

// normalizeStringMapValue coerces a slice of any to a slice of map[string]any.
func normalizeStringMapValue(v any) []map[string]any {
	switch val := v.(type) {
	case []any:
		var out []map[string]any
		for _, item := range val {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	case []map[string]any:
		return val
	}
	return nil
}

// generateStableAntigravitySessionID mirrors CLIProxyAPI generateStableSessionID.
func generateStableAntigravitySessionID(text string) string {
	return "-" + text[:min(len(text), 16)]
}

func generateAntigravityRequestId() string {
	b := make([]byte, 4)
	rand.Read(b)
	return fmt.Sprintf("agent/%d/%s", time.Now().UnixMilli(), hex.EncodeToString(b))
}

// Execute performs a non-streaming Antigravity request.
// Uses generateContent (not streamGenerateContent) so the upstream returns a single
// JSON response that can be translated to OpenAI Chat Completions format.
//
// For Pro-family model IDs, an upstream 400 (commonly caused by an invalid or
// renamed model id) triggers a per-model fallback chain (OmniRoute #3786).
func (e *AntigravityExecutor) Execute(ctx context.Context, req *Request) (*Response, error) {
	url := antigravityNonStreamURL(req.BaseURL)
	headers := map[string]string{
		"Content-Type":   "application/json",
		"Authorization":  "Bearer " + req.AccessToken,
		"User-Agent":     envelopeUserAgent(req),
		"X-Goog-Api-Key": req.APIKey,
	}

	baseModel := resolveAntigravityModelID(req.Model)
	candidates := antigravityProFallbackChains[baseModel]
	if len(candidates) == 0 {
		candidates = []string{baseModel}
	}

	var lastErr error
	for _, modelID := range candidates {
		body, err := e.buildEnvelope(ctx, req, modelID)
		if err != nil {
			return nil, err
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
			lastErr = upErr
			// Retry 400s only when we have another Pro-family candidate id.
			if resp.StatusCode == http.StatusBadRequest && len(candidates) > 1 {
				continue
			}
			return nil, upErr
		}
		return resp, nil
	}
	return nil, lastErr
}

// antigravityNonStreamURL converts the streaming/base URL into the non-streaming
// generateContent endpoint. CLIProxyAPI uses antigravityGeneratePath for non-stream.
func antigravityNonStreamURL(base string) string {
	if base == "" {
		return "https://cloudcode-pa.googleapis.com/v1internal:generateContent"
	}
	u, err := url.Parse(base)
	if err != nil {
		return base
	}
	if strings.Contains(u.Path, "streamGenerateContent") {
		u.Path = strings.Replace(u.Path, "streamGenerateContent", "generateContent", 1)
	} else if !strings.Contains(u.Path, "generateContent") {
		u.Path = "/v1internal:generateContent"
	}
	u.RawQuery = ""
	return u.String()
}

// ExecuteStream performs a streaming Antigravity request.
// Similar to Execute, it retries Pro-family model ids on an upstream 400.
func (e *AntigravityExecutor) ExecuteStream(ctx context.Context, req *Request) (*StreamResult, error) {
	url := req.BaseURL
	if url == "" {
		url = "https://cloudcode-pa.googleapis.com/v1internal:streamGenerateContent?alt=sse"
	}
	headers := map[string]string{
		"Content-Type":   "application/json",
		"Accept":         "text/event-stream",
		"Cache-Control":  "no-cache",
		"Authorization":  "Bearer " + req.AccessToken,
		"User-Agent":     envelopeUserAgent(req),
		"X-Goog-Api-Key": req.APIKey,
	}

	baseModel := resolveAntigravityModelID(req.Model)
	candidates := antigravityProFallbackChains[baseModel]
	if len(candidates) == 0 {
		candidates = []string{baseModel}
	}

	var lastErr error
	for _, modelID := range candidates {
		body, err := e.buildEnvelope(ctx, req, modelID)
		if err != nil {
			return nil, err
		}
		result, err := e.DoStreamRequest(ContextWithProvider(ctx, req.Provider), "POST", url, headers, body)
		if err != nil {
			// Network-level errors are not retried here.
			return nil, err
		}
		// If the upstream rejects the model id with 400, attempt the next candidate.
		// DoStreamRequest surfaces the HTTP status on StreamResult immediately.
		if result != nil && result.StatusCode == http.StatusBadRequest && len(candidates) > 1 {
			lastErr = fmt.Errorf("antigravity stream rejected model %s with status %d", modelID, result.StatusCode)
			continue
		}
		return result, nil
	}
	return nil, lastErr
}
