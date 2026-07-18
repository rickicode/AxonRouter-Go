package executor

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	defaultGrokCLIBaseURL = "https://cli-chat-proxy.grok.com/v1/responses"
	defaultGrokCLIClientVersion = "0.2.93"
	defaultGrokCLIUserAgent     = "xai-grok-workspace/" + defaultGrokCLIClientVersion
)

// GrokCLIExecutor handles xAI Grok CLI's Responses API over OAuth tokens.
// It streams to upstream and collects the final response for non-streaming calls.
type GrokCLIExecutor struct {
	*BaseExecutor
}

// NewGrokCLIExecutor creates a new Grok CLI executor.
func NewGrokCLIExecutor(base *BaseExecutor) *GrokCLIExecutor {
	return &GrokCLIExecutor{BaseExecutor: base}
}

func grokcliURL(req *Request) string {
	base := req.BaseURL
	if base == "" {
		return defaultGrokCLIBaseURL
	}
	// CLIProxyAPI resolves chat requests to cli-chat-proxy.grok.com/v1/responses.
	// Tolerate older DB/config rows that end at /v1 by forcing the /responses path.
	base = strings.TrimSuffix(base, "/")
	if strings.HasSuffix(base, "/responses") {
		return base
	}
	if strings.HasSuffix(base, "/v1") {
		return base + "/responses"
	}
	return base + "/responses"
}

func jwtClaimFromToken(token, claim string) string {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}
	payload := parts[1]
	payload += strings.Repeat("=", (4-len(payload)%4)%4)
	raw, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		return ""
	}
	var claims map[string]any
	if err := json.Unmarshal(raw, &claims); err != nil {
		return ""
	}
	if v, ok := claims[claim].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func grokcliHeaders(req *Request) map[string]string {
	email := ""
	userID := ""
	if req.ProviderSpecificData != nil {
		email = req.ProviderSpecificData["email"]
		userID = req.ProviderSpecificData["sub"]
	}
	if email == "" && req.AccessToken != "" {
		email = jwtClaimFromToken(req.AccessToken, "email")
	}
	if userID == "" && req.AccessToken != "" {
		userID = jwtClaimFromToken(req.AccessToken, "sub")
	}

	token := req.AccessToken
	if token == "" {
		token = req.APIKey
	}

	ua := defaultGrokCLIUserAgent
	if req.Headers != nil && req.Headers["User-Agent"] != "" {
		ua = req.Headers["User-Agent"]
	}

	// Keep headers aligned with CLIProxyAPI's proven set for the Grok CLI
	// chat-proxy endpoint. Extra identity headers can trigger Cloudflare/404.
	headers := map[string]string{
		"Content-Type":          "application/json",
		"Accept":                "text/event-stream",
		"Authorization":         "Bearer " + token,
		"X-XAI-Token-Auth":      "xai-grok-cli",
		"x-grok-client-version": defaultGrokCLIClientVersion,
		"x-grok-conv-id":        uuid.NewString(),
		"User-Agent":            ua,
		"Connection":            "Keep-Alive",
	}
	if email != "" {
		headers["x-email"] = email
	}
	if userID != "" {
		headers["x-userid"] = userID
	}

	for k, v := range req.Headers {
		if _, ok := headers[k]; !ok && v != "" {
			headers[k] = v
		}
	}
	return headers
}

var grokCLIAllowedTopLevel = map[string]bool{
	"model": true,
	"input": true,
	"instructions": true,
	"tools": true,
	"tool_choice": true,
	"parallel_tool_calls": true,
	"reasoning": true,
	"metadata": true,
	"text": true,
	"max_output_tokens": true,
	"temperature": true,
	"top_p": true,
	"presence_penalty": true,
	"frequency_penalty": true,
	"seed": true,
	"service_tier": true,
	"include": true,
	"stream": true,
	"store": true,
	"user": true,
	"previous_response_id": true,
	"prompt_cache_key": true,
}

var grokCLIAllowedInputTypes = map[string]bool{
	"message":                 true,
	"reasoning":               true,
	"function_call":           true,
	"function_call_output":    true,
	"tool_use":                true,
	"tool_result":             true,
	"file_search_call":        true,
	"file_search_call_output": true,
	"computer_call":           true,
	"computer_call_output":    true,
	"web_search_call":         true,
	"web_search_call_output":  true,
	"additional_tools":        true,
}

func grokcliRequestBody(req *Request) ([]byte, error) {
	var body map[string]any
	if err := json.Unmarshal(req.Body, &body); err != nil || body == nil {
		body = map[string]any{}
	}

	model := ExtractModel(req.Model)
	body["model"] = model
	body["stream"] = true
	body["store"] = false

	// Drop Responses fields that the CLI chat-proxy does not accept; mirrors
	// CLIProxyAPI's prepareResponsesRequest cleanup.
	delete(body, "previous_response_id")
	delete(body, "stream_options")
	delete(body, "prompt_cache_retention")
	delete(body, "safety_identifier")

	reasoning := map[string]any{}
	if existing, ok := body["reasoning"].(map[string]any); ok {
		reasoning = existing
	}
	if re, ok := body["reasoning_effort"].(string); ok {
		re = strings.ToLower(strings.TrimSpace(re))
		delete(body, "reasoning_effort")
		if re != "" && re != "none" {
			if _, ok := reasoning["effort"]; !ok {
				reasoning["effort"] = re
			}
		}
	}
	baseModel := model
	effort := ""
	for _, level := range []string{"xhigh", "high", "medium", "low"} {
		suffix := "-" + level
		if strings.HasSuffix(baseModel, suffix) {
			baseModel = strings.TrimSuffix(baseModel, suffix)
			effort = level
			break
		}
	}
	if effort == "" && strings.HasSuffix(baseModel, "-reasoning") {
		baseModel = strings.TrimSuffix(baseModel, "-reasoning")
		effort = "medium"
	}
	if effort == "" && strings.HasSuffix(baseModel, "-thinking") {
		baseModel = strings.TrimSuffix(baseModel, "-thinking")
		effort = "medium"
	}
	// Back-compat: older clients may refer to the pre-release build alias.
	if baseModel == "grok-build-0.1" {
		baseModel = "grok-build"
	}
	if baseModel != model {
		body["model"] = baseModel
	}
	if effort != "" {
		if _, ok := reasoning["effort"]; !ok {
			reasoning["effort"] = effort
		}
	}
	if len(reasoning) > 0 {
		body["reasoning"] = reasoning
		include := map[string]bool{}
		if arr, ok := body["include"].([]any); ok {
			for _, v := range arr {
				include[fmt.Sprint(v)] = true
			}
		}
		include["reasoning.encrypted_content"] = true
		newInclude := make([]any, 0, len(include))
		for k := range include {
			newInclude = append(newInclude, k)
		}
		body["include"] = newInclude
	}

	if rawTools, ok := body["tools"]; ok {
		body["tools"] = grokcliFlattenTools(rawTools)
	}

	if rawInput, ok := body["input"].([]any); ok {
		body["input"] = grokcliFilterInput(rawInput)
	}

	for k := range body {
		if !grokCLIAllowedTopLevel[k] {
			delete(body, k)
		}
	}

	return json.Marshal(body)
}

func grokcliFlattenTools(raw any) []any {
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}
	var out []any
	for _, t := range arr {
		m, ok := t.(map[string]any)
		if !ok {
			continue
		}
		if _, hasNS := m["namespace"]; hasNS {
			if nested, ok := m["tools"].([]any); ok {
				out = append(out, grokcliFlattenTools(nested)...)
				continue
			}
		}
		if fn, ok := m["function"].(map[string]any); ok {
			fn["type"] = "function"
			out = append(out, fn)
			continue
		}
		if typ, ok := m["type"].(string); ok {
			if typ == "custom" {
				m["type"] = "function"
			}
			if typ == "function" {
				if _, hasName := m["name"]; !hasName {
					continue
				}
			}
			out = append(out, m)
		}
	}
	return out
}

func grokcliFilterInput(input []any) []any {
	var out []any
	for _, raw := range input {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		typ, _ := m["type"].(string)
		if !grokCLIAllowedInputTypes[typ] {
			continue
		}
		out = append(out, m)
	}
	return out
}

// GrokCLIUsage holds token counts parsed from a Grok CLI response.usage object.
type GrokCLIUsage struct {
	InputTokens     int64
	OutputTokens    int64
	TotalTokens     int64
	ReasoningTokens int64
}

func (u GrokCLIUsage) ToMap() map[string]int64 {
	return map[string]int64{
		"prompt_tokens":     u.InputTokens,
		"completion_tokens": u.OutputTokens,
		"total_tokens":      u.TotalTokens,
		"reasoning_tokens":  u.ReasoningTokens,
	}
}

func extractGrokCLIUsage(payload []byte) GrokCLIUsage {
	trimmed := strings.TrimSpace(string(payload))
	data, _ := grokcliParseEvent([]byte(trimmed))
	root := gjson.ParseBytes(data)
	if r := root.Get("response"); r.Exists() {
		root = r
	}
	usage := root.Get("usage")
	if !usage.Exists() {
		return GrokCLIUsage{}
	}
	return GrokCLIUsage{
		InputTokens:     usage.Get("input_tokens").Int(),
		OutputTokens:    usage.Get("output_tokens").Int(),
		TotalTokens:     usage.Get("total_tokens").Int(),
		ReasoningTokens: usage.Get("output_tokens_details.reasoning_tokens").Int(),
	}
}

func grokcliParseEvent(line []byte) ([]byte, string) {
	data := strings.TrimSpace(string(line))
	if strings.HasPrefix(data, "data:") {
		data = strings.TrimSpace(data[5:])
	}
	if data == "" {
		return nil, ""
	}
	return []byte(data), gjson.Get(data, "type").String()
}

func grokcliPatchCompletedOutput(payload []byte, byIndex map[int64][]byte, fallback [][]byte) []byte {
	data, eventType := grokcliParseEvent(payload)
	if eventType != "response.completed" || len(data) == 0 {
		return payload
	}
	output := gjson.GetBytes(data, "response.output")
	needsPatch := (!output.Exists() || !output.IsArray() || len(output.Array()) == 0) &&
		(len(byIndex) > 0 || len(fallback) > 0)
	if !needsPatch {
		return payload
	}
	patched, err := sjson.SetRawBytes(data, "response.output", []byte("[]"))
	if err != nil {
		return payload
	}
	var indexes []int64
	for idx := range byIndex {
		indexes = append(indexes, idx)
	}
	for i := 0; i < len(indexes)-1; i++ {
		for j := i + 1; j < len(indexes); j++ {
			if indexes[i] > indexes[j] {
				indexes[i], indexes[j] = indexes[j], indexes[i]
			}
		}
	}
	for _, idx := range indexes {
		patched, _ = sjson.SetRawBytes(patched, "response.output.-1", byIndex[idx])
	}
	for _, item := range fallback {
		patched, _ = sjson.SetRawBytes(patched, "response.output.-1", item)
	}
	return append([]byte("data: "), patched...)
}

// Execute performs a Grok CLI Responses API call.
func (e *GrokCLIExecutor) Execute(ctx context.Context, req *Request) (*Response, error) {
	url := grokcliURL(req)
	body, err := grokcliRequestBody(req)
	if err != nil {
		return nil, fmt.Errorf("grok-cli request body: %w", err)
	}
	headers := grokcliHeaders(req)

	cfg := &StreamConfig{ScannerMaxTokenSize: 64 * 1024}
	streamResult, err := e.DoStreamRequestWithConfig(ctx, "POST", url, headers, body, cfg)
	if err != nil {
		if upErr, ok := err.(*UpstreamError); ok {
			upErr.TranslateErrorBody(req.Provider)
		}
		return nil, err
	}

	var statusCode int
	var usage GrokCLIUsage
	outputItemsByIndex := make(map[int64][]byte)
	var outputItemsFallback [][]byte
	var completedPayload []byte

	for chunk := range streamResult.Chunks {
		if chunk.Err != nil {
			return nil, fmt.Errorf("grok-cli stream error: %w", chunk.Err)
		}
		if chunk.Payload == nil {
			continue
		}
		payload := chunk.Payload
		data, eventType := grokcliParseEvent(payload)
		switch eventType {
		case "response.output_item.done":
			if item := gjson.GetBytes(data, "item"); item.Exists() && item.Type == gjson.JSON {
				idx := gjson.GetBytes(data, "output_index").Int()
				if gjson.GetBytes(data, "output_index").Exists() {
					outputItemsByIndex[idx] = []byte(item.Raw)
				} else {
					outputItemsFallback = append(outputItemsFallback, []byte(item.Raw))
				}
			}
		case "response.completed", "response.done":
			payload = grokcliPatchCompletedOutput(payload, outputItemsByIndex, outputItemsFallback)
			completedPayload = payload
			usage = extractGrokCLIUsage(payload)
		}
	}

	if len(completedPayload) == 0 {
		return nil, fmt.Errorf("grok-cli stream closed before response.completed")
	}
	if streamResult.StatusCode > 0 {
		statusCode = streamResult.StatusCode
	} else {
		statusCode = 200
	}

	responseBody, _ := grokcliParseEvent(completedPayload)
	return &Response{
		StatusCode: statusCode,
		Body:       responseBody,
		Headers:    streamResult.Headers,
		Usage:      usage.ToMap(),
	}, nil
}

// ExecuteStream performs a streaming Grok CLI Responses API call.
func (e *GrokCLIExecutor) ExecuteStream(ctx context.Context, req *Request) (*StreamResult, error) {
	url := grokcliURL(req)
	body, err := grokcliRequestBody(req)
	if err != nil {
		return nil, fmt.Errorf("grok-cli request body: %w", err)
	}
	headers := grokcliHeaders(req)

	cfg := &StreamConfig{ScannerMaxTokenSize: 64 * 1024}
	result, err := e.DoStreamRequestWithConfig(ctx, "POST", url, headers, body, cfg)
	if err != nil {
		if upErr, ok := err.(*UpstreamError); ok {
			upErr.TranslateErrorBody(req.Provider)
		}
		return nil, err
	}
	return result, nil
}
