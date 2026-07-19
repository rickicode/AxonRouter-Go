package executor

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	defaultGrokCLIBaseURL = "https://cli-chat-proxy.grok.com/v1/responses"
	defaultGrokCLIClientVersion = "0.2.99"
	defaultGrokCLIUserAgent = "grok-shell/" + defaultGrokCLIClientVersion + " (linux; x86_64)"
)

// GrokCLIExecutor handles xAI Grok CLI's Responses API over OAuth tokens.
// It streams to upstream and collects the final response for non-streaming calls.
type GrokCLIExecutor struct {
	*BaseExecutor
	// sessionLocks serializes per-connection state mutation so turn index and
	// stable session IDs are updated atomically relative to other requests on
	// the same connection. The actual state lives in provider_specific_data.
	sessionLocks sync.Map // connectionID -> *sync.Mutex
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

func grokcliHeaders(req *Request, sessionID, convID, agentID, reqID string, turnIdx int) map[string]string {
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

	// Keep headers aligned with Grok CLI's current identity for the
	// cli-chat-proxy endpoint while avoiding older/xai-grok-workspace headers
	// that can trigger Cloudflare/404-style rejections.
	headers := map[string]string{
		"Content-Type": "application/json",
		"Accept": "text/event-stream",
		"Authorization": "Bearer " + token,
		"X-XAI-Token-Auth": "xai-grok-cli",
		"x-grok-client-version": defaultGrokCLIClientVersion,
		"x-grok-client-identifier": "grok-shell",
		"x-grok-client-mode": "headless",
		"x-grok-session-id": sessionID,
		"x-grok-conv-id": convID,
		"x-grok-req-id": reqID,
		"x-grok-turn-idx": strconv.Itoa(turnIdx),
		"x-grok-agent-id": agentID,
		"x-grok-model-override": ExtractModel(req.Model),
		"User-Agent": ua,
		"Connection": "Keep-Alive",
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

func (e *GrokCLIExecutor) connMu(connID string) *sync.Mutex {
	if connID == "" {
		return &sync.Mutex{}
	}
	if v, ok := e.sessionLocks.Load(connID); ok {
		return v.(*sync.Mutex)
	}
	mu := &sync.Mutex{}
	if v, loaded := e.sessionLocks.LoadOrStore(connID, mu); loaded {
		return v.(*sync.Mutex)
	}
	return mu
}

const (
	grokCLISessionIDKey = "grokSessionId"
	grokCLIConvIDKey    = "grokConvId"
	grokCLITurnIdxKey   = "grokTurnIdx"
	grokCLIAgentIDKey   = "grokAgentId"
)

func grokcliCountUserMessages(body []byte) int {
	if input := gjson.GetBytes(body, "input"); input.IsArray() {
		count := 0
		input.ForEach(func(_, item gjson.Result) bool {
			if item.Get("role").String() == "user" {
				count++
			}
			return true
		})
		return count
	}
	if messages := gjson.GetBytes(body, "messages"); messages.IsArray() {
		count := 0
		messages.ForEach(func(_, msg gjson.Result) bool {
			if msg.Get("role").String() == "user" {
				count++
			}
			return true
		})
		return count
	}
	return 0
}

func grokcliStableAgentID(psd map[string]string) string {
	if v := psd[grokCLIAgentIDKey]; v != "" {
		return v
	}
	seed := psd["deviceId"]
	if seed == "" {
		seed = psd["sub"]
	}
	if seed == "" {
		seed = psd["email"]
	}
	if seed == "" {
		seed = psd[grokCLISessionIDKey]
	}
	if seed == "" {
		seed = uuid.NewString()
	}
	agentID := uuid.NewSHA1(uuid.NameSpaceOID, []byte("grok-cli-agent:"+seed)).String()
	psd[grokCLIAgentIDKey] = agentID
	return agentID
}

func grokcliGetTurnIdx(psd map[string]string) int {
	if v := psd[grokCLITurnIdxKey]; v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return 0
}

func grokcliEnsureID(psd map[string]string, key string) string {
	if v := psd[key]; v != "" {
		return v
	}
	v := uuid.NewString()
	psd[key] = v
	return v
}

// allocateGrokCLISession reads the connection's persisted session state, assigns
// a fresh request UUID, computes the current turn index from the number of user
// messages in the request, increments the stored turn index, and persists the
// updated state. It returns the values to send as upstream headers.
func (e *GrokCLIExecutor) allocateGrokCLISession(req *Request) (sessionID, convID, agentID, reqID string, turnIdx int, err error) {
	mu := e.connMu(req.ConnectionID)
	mu.Lock()
	defer mu.Unlock()

	if req.ProviderSpecificData == nil {
		req.ProviderSpecificData = map[string]string{}
	}

	n := grokcliCountUserMessages(req.Body)
	sessionID = grokcliEnsureID(req.ProviderSpecificData, grokCLISessionIDKey)
	convID = grokcliEnsureID(req.ProviderSpecificData, grokCLIConvIDKey)
	agentID = grokcliStableAgentID(req.ProviderSpecificData)
	currentTurn := grokcliGetTurnIdx(req.ProviderSpecificData)
	turnIdx = currentTurn
	req.ProviderSpecificData[grokCLITurnIdxKey] = strconv.Itoa(currentTurn + n)
	reqID = uuid.NewString()

	if req.PersistProviderSpecificData != nil {
		if err = req.PersistProviderSpecificData(req.ProviderSpecificData); err != nil {
			return "", "", "", "", 0, err
		}
	}
	return sessionID, convID, agentID, reqID, turnIdx, nil
}

const grokCLIMaxTools = 200

var grokCLIAllowedTopLevel = map[string]bool{
	"model":               true,
	"input":               true,
	"instructions":        true,
	"tools":               true,
	"tool_choice":         true,
	"parallel_tool_calls": true,
	"reasoning":           true,
	"metadata":            true,
	"text":                true,
	"max_output_tokens":   true,
	"temperature":         true,
	"top_p":               true,
	"service_tier":        true,
	"include":             true,
	"stream":              true,
	"store":               true,
	"prompt_cache_key":    true,
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

// grokCLINormalizeCtx holds request-scoped namespace and client-tool metadata
// used during Grok CLI request normalization. It must not be persisted to
// connection state because it is rebuilt from each request payload.
type grokCLINormalizeCtx struct {
	namespaceRefs      map[string]grokCLINamespaceToolRef
	clientDeclaredKeys map[string]bool
}

// grokCLINamespaceToolRef records the original namespace and short name for a
// tool whose upstream name has been qualified.
type grokCLINamespaceToolRef struct {
	namespace string
	name      string
}

func grokcliRequestBody(req *Request) ([]byte, error) {
	// Build request-scoped normalization context from the raw payload and
	// flatten namespace tools before unmarshalling into the executor's body map.
	normCtx := grokCLINormalizeCtx{
		namespaceRefs:      collectGrokCLINamespaceToolRefs(req.Body),
		clientDeclaredKeys: collectGrokCLIClientDeclaredKeys(req.Body),
	}
	rawBody := grokcliFlattenNamespaceTools(req.Body, normCtx.namespaceRefs)

	var body map[string]any
	if err := json.Unmarshal(rawBody, &body); err != nil || body == nil {
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

	// Fallback: if the request reached the executor without `input` items
	// (e.g. /v1/responses clients that pass `messages[]`), convert the simple
	// Chat Completions shape to Responses input items. Tools and other top-level
	// fields are left for the executor's own flattening/normalization below.
	if rawInput, hasInput := body["input"].([]any); !hasInput || len(rawInput) == 0 {
		if rawMessages := gjson.GetBytes(req.Body, "messages"); rawMessages.IsArray() && len(rawMessages.Array()) > 0 {
			inputItems := grokcliConvertMessagesToInput(rawMessages)
			if len(inputItems) > 0 {
				body["input"] = inputItems
			}
		}
	}

	reasoning := map[string]any{}
	if existing, ok := body["reasoning"].(map[string]any); ok {
		reasoning = existing
	}
	if re, ok := body["reasoning_effort"].(string); ok {
		re = strings.ToLower(strings.TrimSpace(re))
		delete(body, "reasoning_effort")
		if re == "max" {
			re = "xhigh"
		}
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
	supportsReasoning := strings.Contains(baseModel, "grok-4.5") || strings.Contains(model, "grok-4.5")
	if len(reasoning) > 0 && supportsReasoning {
		reasoning["summary"] = "concise"
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
	} else {
		delete(body, "reasoning")
	}

	if rawTools, ok := body["tools"].([]any); ok {
		if len(rawTools) > grokCLIMaxTools {
			logging.Logger.Warn("grok-cli tool list truncated", "original", len(rawTools), "max", grokCLIMaxTools)
			rawTools = rawTools[:grokCLIMaxTools]
		}
		body["tools"] = rawTools
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

func grokcliConvertMessagesToInput(messages gjson.Result) []any {
	var input []any
	messages.ForEach(func(_, msg gjson.Result) bool {
		role := msg.Get("role").String()
		switch role {
		case "tool":
			item := map[string]any{
				"type":    "function_call_output",
				"call_id": msg.Get("tool_call_id").String(),
				"output":  msg.Get("content").String(),
			}
			input = append(input, item)
		case "assistant":
			if content := msg.Get("content").String(); content != "" {
				input = append(input, map[string]any{
					"type":    "message",
					"role":    "assistant",
					"content": content,
				})
			}
			toolCalls := msg.Get("tool_calls")
			if toolCalls.IsArray() {
				toolCalls.ForEach(func(_, tc gjson.Result) bool {
					if tc.Get("type").String() != "function" {
						return true
					}
					input = append(input, map[string]any{
						"type":      "function_call",
						"call_id":   tc.Get("id").String(),
						"name":      tc.Get("function.name").String(),
						"arguments": tc.Get("function.arguments").String(),
					})
					return true
				})
			}
		default:
			// user, system, developer
			content := msg.Get("content").String()
			if content == "" && msg.Get("content").Exists() {
				content = msg.Get("content").Raw
			}
			input = append(input, map[string]any{
				"type":    "message",
				"role":    role,
				"content": content,
			})
		}
		return true
	})
	return input
}

func grokcliFilterInput(input []any) []any {
	var out []any
	for _, raw := range input {
		// Keep plain strings (official CLI accepts string input).
		if s, ok := raw.(string); ok {
			out = append(out, s)
			continue
		}
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		typ, _ := m["type"].(string)
		switch typ {
		case "item_reference":
			// Stale references cannot resolve with store=false.
			continue
		case "custom_tool_call":
			m["type"] = "function_call"
		case "custom_tool_call_output":
			m["type"] = "function_call_output"
		case "internal_chat_message_metadata_passthrough":
			continue
		}
		if typ, _ = m["type"].(string); !grokCLIAllowedInputTypes[typ] && m["role"] == nil {
			continue
		}
		// Drop server-generated item IDs that cannot resolve with store=false.
		if id, ok := m["id"].(string); ok {
			for _, prefix := range []string{"rs_", "fc_", "resp_", "msg_"} {
				if strings.HasPrefix(id, prefix) {
					delete(m, "id")
					break
				}
			}
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

// grokcliRetryConfig returns the retry policy for a given upstream status code.
// 429 is retried up to 2 times with a 2s base exponential backoff.
// 502/503 are retried up to 2 times with a 1.5s base exponential backoff.
func grokcliRetryConfig(statusCode int) (maxAttempts int, baseDelay time.Duration, retryable bool) {
	switch statusCode {
	case http.StatusTooManyRequests:
		return 2, 2 * time.Second, true
	case http.StatusBadGateway, http.StatusServiceUnavailable:
		return 2, 1500 * time.Millisecond, true
	}
	return 0, 0, false
}

// doStreamWithRetry sends the request and retries transient upstream status
// codes with exponential backoff. It preserves the original request body so it
// can be re-sent on each attempt.
func (e *GrokCLIExecutor) doStreamWithRetry(ctx context.Context, url string, headers map[string]string, body []byte, cfg *StreamConfig) (*StreamResult, error) {
	for attempt := 1; ; attempt++ {
		result, err := e.DoStreamRequestWithConfig(ctx, "POST", url, headers, body, cfg)
		if err == nil {
			return result, nil
		}
		var upErr *UpstreamError
		if !errors.As(err, &upErr) {
			return nil, err
		}
		maxAttempts, baseDelay, retryable := grokcliRetryConfig(upErr.StatusCode)
		if !retryable || attempt >= maxAttempts {
			upErr.TranslateErrorBody("grok-cli")
			return nil, upErr
		}
		delay := baseDelay * time.Duration(1<<(attempt-1))
		if delay > 30*time.Second {
			delay = 30 * time.Second
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
}

// Execute performs a Grok CLI Responses API call.
func (e *GrokCLIExecutor) Execute(ctx context.Context, req *Request) (*Response, error) {
	url := grokcliURL(req)
	body, err := grokcliRequestBody(req)
	if err != nil {
		return nil, fmt.Errorf("grok-cli request body: %w", err)
	}
	sessionID, convID, agentID, reqID, turnIdx, err := e.allocateGrokCLISession(req)
	if err != nil {
		return nil, fmt.Errorf("grok-cli session: %w", err)
	}
	headers := grokcliHeaders(req, sessionID, convID, agentID, reqID, turnIdx)

	cfg := &StreamConfig{ScannerMaxTokenSize: 64 * 1024}
	streamResult, err := e.doStreamWithRetry(ctx, url, headers, body, cfg)
	if err != nil {
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
	sessionID, convID, agentID, reqID, turnIdx, err := e.allocateGrokCLISession(req)
	if err != nil {
		return nil, fmt.Errorf("grok-cli session: %w", err)
	}
	headers := grokcliHeaders(req, sessionID, convID, agentID, reqID, turnIdx)

	cfg := &StreamConfig{ScannerMaxTokenSize: 64 * 1024}
	result, err := e.doStreamWithRetry(ctx, url, headers, body, cfg)
	if err != nil {
		return nil, err
	}
	return result, nil
}
