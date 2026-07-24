package executor

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/rickicode/AxonRouter-Go/internal/translator/codex/responses"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// CLIProxyAPI Codex defaults (internal/runtime/executor/codex_executor.go:37-38).
const (
	defaultCodexUserAgent = "codex-tui/0.135.0 (Mac OS 26.5.0; arm64) iTerm.app/3.6.10 (codex-tui; 0.135.0)"
	codexOriginator       = "codex-tui"
)

// codexScannerMax is the per-line buffer size for Codex Responses SSE streams.
// Codex can emit single data: lines containing full outputs/images (>64 KB).

// codexScannerMax is the per-line buffer size for Codex Responses SSE streams.
// Codex can emit single data: lines containing full outputs/images (>64 KB).
const codexScannerMax = 52_428_800 // 50 MB, matching CLIProxyAPI Codex

var (
	codexDropNonstandardMu sync.RWMutex
	codexDropNonstandard   = true
)

// SetDropNonstandardCodexSSE controls whether SSE event lines whose name starts
// with "codex." are filtered out before being returned to clients. The default
// is true because Codex CLI emits event-only lines that break OpenAI SDKs.
func SetDropNonstandardCodexSSE(v bool) {
	codexDropNonstandardMu.Lock()
	codexDropNonstandard = v
	codexDropNonstandardMu.Unlock()
}

func shouldDropNonstandardCodexSSE() bool {
	codexDropNonstandardMu.RLock()
	defer codexDropNonstandardMu.RUnlock()
	return codexDropNonstandard
}

// CodexExecutor handles OpenAI Codex (Responses API) with OAuth tokens.
// The Codex API is streaming-only: it rejects stream:false.
// Execute() sends stream:true to upstream and collects the SSE response internally.
type CodexExecutor struct {
	*BaseExecutor
}

// NewCodexExecutor creates a new Codex executor.
func NewCodexExecutor(base *BaseExecutor) *CodexExecutor {
	return &CodexExecutor{BaseExecutor: base}
}

func codexHeaders(req *Request) map[string]string {
	// CLIProxyAPI forwards the client User-Agent when present, otherwise falls
	// back to a real Codex-CLI default. This reduces Cloudflare 1010 blocks and
	// matches the headers the upstream OAuth credential was issued for.
	ua := defaultCodexUserAgent
	if req.Headers != nil && req.Headers["User-Agent"] != "" {
		ua = req.Headers["User-Agent"]
	} else if req.ProviderSpecificData != nil && req.ProviderSpecificData["userAgent"] != "" {
		ua = req.ProviderSpecificData["userAgent"]
	}

	headers := map[string]string{
		"Content-Type":  "application/json",
		"Accept":        "text/event-stream",
		"Authorization": "Bearer " + req.AccessToken,
		"User-Agent":    ua,
		"Originator":    codexOriginator,
		"Connection":    "Keep-Alive",
	}
	if req.APIKey != "" && req.AccessToken == "" {
		headers["Authorization"] = "Bearer " + req.APIKey
	}

	// chatgpt-account-id is required for some Codex OAuth sessions. Prefer the
	// value stored during import, but fall back to parsing the access token.
	accountID := ""
	if req.ProviderSpecificData != nil {
		accountID = req.ProviderSpecificData["accountId"]
		// Legacy key used by older imports.
		if accountID == "" {
			accountID = req.ProviderSpecificData["workspaceId"]
		}
	}
	if accountID == "" {
		accountID = codexAccountIDFromToken(req.AccessToken)
	}
	if accountID != "" {
		headers["Chatgpt-Account-Id"] = accountID
	}

	// Codex's session-level prompt cache header. CLIProxyAPI adds it when the
	// User-Agent contains "Mac OS".
	if strings.Contains(ua, "Mac OS") {
		headers["Session_id"] = uuid.NewString()
	}

	// Forward any remaining client headers that are not already set.
	for k, v := range req.Headers {
		if _, ok := headers[k]; !ok && v != "" {
			headers[k] = v
		}
	}
	return headers
}

// codexAccountIDFromToken extracts chatgpt_account_id from a Codex access-token
// JWT payload. This matches the claim path used by OpenAI's Auth0 tokens.
func codexAccountIDFromToken(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	root := gjson.ParseBytes(payload)
	auth := root.Get(`https://api.openai.com/auth`)
	if auth.Exists() {
		return auth.Get("chatgpt_account_id").String()
	}
	return ""
}

func codexURL(req *Request) string {
	if req.BaseURL != "" {
		return req.BaseURL
	}
	return "https://chatgpt.com/backend-api/codex/responses"
}

// normalizeCodexResponsesRequest transforms a native OpenAI Responses request
// into a Codex-compatible Responses request. It is applied only when the inbound
// body already uses the Responses API shape (has an "input" field).
func normalizeCodexResponsesRequest(body []byte) []byte {
	root := gjson.ParseBytes(body)
	out := []byte(`{}`)

	// input: coerce a string to a single user message array, otherwise normalize
	// each item and convert system role to developer.
	if input := root.Get("input"); input.Exists() {
		if input.Type == gjson.String {
			out, _ = sjson.SetRawBytes(out, "input", buildCodexInputFromString(input.String()))
		} else if input.IsArray() {
			out, _ = sjson.SetRawBytes(out, "input", normalizeCodexInputArray(input))
		}
	}
	if !gjson.GetBytes(out, "input").Exists() {
		out, _ = sjson.SetRawBytes(out, "input", []byte(`[]`))
	}

	// model
	if v := root.Get("model"); v.Exists() {
		out, _ = sjson.SetBytes(out, "model", v.Value())
	}

	// instructions: preserve client value, otherwise inject Codex defaults.
	if v := root.Get("instructions"); v.Exists() && v.Type == gjson.String && v.String() != "" {
		out, _ = sjson.SetBytes(out, "instructions", v.String())
	}
	if !gjson.GetBytes(out, "instructions").Exists() {
		out, _ = sjson.SetBytes(out, "instructions", responses.DefaultInstructions())
	}

	// tools: normalize legacy web_search_preview aliases to web_search.
	if tools := root.Get("tools"); tools.Exists() && tools.IsArray() {
		out, _ = sjson.SetRawBytes(out, "tools", normalizeCodexTools(tools))
	}

	// tool_choice: normalize legacy web_search_preview aliases.
	if tc := root.Get("tool_choice"); tc.Exists() {
		if normalized, ok := normalizeCodexToolChoice(tc); ok {
			out, _ = sjson.SetRawBytes(out, "tool_choice", normalized)
		}
	}

	// reasoning: keep client value, otherwise provide a minimal default.
	if v := root.Get("reasoning"); v.Exists() {
		out, _ = sjson.SetRawBytes(out, "reasoning", []byte(v.Raw))
	} else {
		out, _ = sjson.SetBytes(out, "reasoning.effort", "medium")
		out, _ = sjson.SetBytes(out, "reasoning.summary", "auto")
	}

	// text settings (responses-format output format) and metadata Codex supports.
	if v := root.Get("text"); v.Exists() {
		out, _ = sjson.SetRawBytes(out, "text", []byte(v.Raw))
	}
	if v := root.Get("prompt_cache_key"); v.Exists() {
		out, _ = sjson.SetBytes(out, "prompt_cache_key", v.Value())
	}
	if v := root.Get("client_metadata"); v.Exists() {
		out, _ = sjson.SetRawBytes(out, "client_metadata", []byte(v.Raw))
	}

	// service_tier is restricted to "priority"; anything else is dropped.
	if st := root.Get("service_tier"); st.Exists() && st.String() == "priority" {
		out, _ = sjson.SetBytes(out, "service_tier", "priority")
	}

	// Required Codex defaults.
	out, _ = sjson.SetBytes(out, "stream", true)
	out, _ = sjson.SetBytes(out, "store", false)
	out, _ = sjson.SetBytes(out, "parallel_tool_calls", true)
	out, _ = sjson.SetRawBytes(out, "include", []byte(`["reasoning.encrypted_content"]`))

	// Final allowlist ensures unknown top-level fields do not leak upstream.
	allowed := map[string]struct{}{
		"model": {}, "input": {}, "instructions": {}, "tools": {},
		"tool_choice": {}, "stream": {}, "store": {}, "reasoning": {},
		"parallel_tool_calls": {}, "service_tier": {}, "include": {},
		"prompt_cache_key": {}, "client_metadata": {}, "text": {},
	}
	if parsed := gjson.ParseBytes(out); parsed.IsObject() {
		parsed.ForEach(func(key, _ gjson.Result) bool {
			if _, ok := allowed[key.String()]; !ok {
				out, _ = sjson.DeleteBytes(out, key.String())
			}
			return true
		})
	}

	return out
}

func buildCodexInputFromString(s string) []byte {
	item := []byte(`{}`)
	item, _ = sjson.SetBytes(item, "type", "message")
	item, _ = sjson.SetBytes(item, "role", "user")
	part := []byte(`{}`)
	part, _ = sjson.SetBytes(part, "type", "input_text")
	part, _ = sjson.SetBytes(part, "text", s)
	item, _ = sjson.SetRawBytes(item, "content", []byte(`[`+string(part)+`]`))
	return []byte(`[` + string(item) + `]`)
}

func normalizeCodexInputArray(input gjson.Result) []byte {
	out := []byte(`[]`)
	for _, it := range input.Array() {
		item := []byte(it.Raw)
		if role := it.Get("role"); role.Exists() && role.String() == "system" {
			item, _ = sjson.SetBytes(item, "role", "developer")
		}
		if content := it.Get("content"); content.Exists() && content.Type == gjson.String {
			partType := "input_text"
			if r := it.Get("role").String(); r == "assistant" {
				partType = "output_text"
			}
			part := []byte(`{}`)
			part, _ = sjson.SetBytes(part, "type", partType)
			part, _ = sjson.SetBytes(part, "text", content.String())
			item, _ = sjson.SetRawBytes(item, "content", []byte(`[`+string(part)+`]`))
		}
		out, _ = sjson.SetRawBytes(out, "-1", item)
	}
	return out
}

func normalizeCodexTools(tools gjson.Result) []byte {
	out := []byte(`[]`)
	for _, t := range tools.Array() {
		item := []byte(t.Raw)
		if typ := t.Get("type"); typ.Exists() {
			name := typ.String()
			if name == "web_search_preview" || name == "web_search_preview_2025_03_11" {
				item, _ = sjson.SetBytes(item, "type", "web_search")
			}
		}
		out, _ = sjson.SetRawBytes(out, "-1", item)
	}
	return out
}

func normalizeCodexToolChoice(tc gjson.Result) ([]byte, bool) {
	switch {
	case tc.Type == gjson.String:
		return []byte(tc.Raw), true
	case tc.IsObject():
		item := []byte(tc.Raw)
		if typ := tc.Get("type"); typ.Exists() {
			name := typ.String()
			if name == "web_search_preview" || name == "web_search_preview_2025_03_11" {
				item, _ = sjson.SetBytes(item, "type", "web_search")
			}
		}
		return item, true
	default:
		return nil, false
	}
}

func codexRequestBody(body []byte) []byte {
	// Only apply Responses-shape normalization when the inbound body uses the
	// Responses API (has an "input" field). Legacy chat-completions bodies are
	// left untouched apart from forcing stream/store.
	if gjson.GetBytes(body, "input").Exists() {
		body = normalizeCodexResponsesRequest(body)
	}
	body = JSONSet(body, "stream", true)
	body = JSONSet(body, "store", false)
	return body
}

// CodexUsage holds token counts parsed from a Codex response.usage object.
type CodexUsage struct {
	InputTokens         int64
	OutputTokens        int64
	TotalTokens         int64
	CachedTokens        int64
	CacheCreationTokens int64
	ReasoningTokens     int64
}

func extractCodexUsage(raw []byte) CodexUsage {
	trimmed := bytes.TrimSpace(raw)
	if bytes.HasPrefix(trimmed, []byte("data:")) {
		trimmed = bytes.TrimSpace(trimmed[5:])
	}
	root := gjson.ParseBytes(trimmed)
	if r := root.Get("response"); r.Exists() {
		root = r
	}
	usage := root.Get("usage")
	if !usage.Exists() {
		return CodexUsage{}
	}
	return CodexUsage{
		InputTokens:         usage.Get("input_tokens").Int(),
		OutputTokens:        usage.Get("output_tokens").Int(),
		TotalTokens:         usage.Get("total_tokens").Int(),
		CachedTokens:        usage.Get("input_tokens_details.cached_tokens").Int(),
		CacheCreationTokens: usage.Get("input_tokens_details.cache_creation_tokens").Int(),
		ReasoningTokens:     usage.Get("output_tokens_details.reasoning_tokens").Int(),
	}
}

// Execute performs a Codex Responses API call. The upstream always receives
// stream:true; the SSE response is collected and returned as a single
// non-streaming Response.
func (e *CodexExecutor) Execute(ctx context.Context, req *Request) (*Response, error) {
	url := codexURL(req)
	body := codexRequestBody(req.Body)
	headers := codexHeaders(req)

	cfg := &StreamConfig{ScannerMaxTokenSize: codexScannerMax}
	streamResult, err := e.DoStreamRequestWithConfig(ctx, "POST", url, headers, body, cfg)
	if err != nil {
		if upErr, ok := err.(*UpstreamError); ok {
			upErr.TranslateErrorBody(req.Provider)
		}
		return nil, err
	}

	var statusCode int
	var usage CodexUsage
	// Codex Responses streams `response.output_item.done` events before the final
	// `response.completed`, but the completed event sometimes has an empty
	// `response.output` array. Collect output_item.done items and patch them into
	// the completed event, matching CLIProxyAPI Codex behavior.
	outputItemsByIndex := make(map[int64][]byte)
	var outputItemsFallback [][]byte
	var completedPayload []byte
	for chunk := range streamResult.Chunks {
		if chunk.Err != nil {
			return nil, fmt.Errorf("codex stream error: %w", chunk.Err)
		}
		if chunk.Payload == nil || isNonstandardCodexSSELine(chunk.Payload) {
			continue
		}
		payload := chunk.Payload
		// Collect output_item.done for patching a later response.completed.
		eventData, eventType := parseCodexEvent(payload)
		if eventType == "response.output_item.done" && len(eventData) > 0 {
			if item := gjson.GetBytes(eventData, "item"); item.Exists() && item.Type == gjson.JSON {
				idx := gjson.GetBytes(eventData, "output_index").Int()
				if gjson.GetBytes(eventData, "output_index").Exists() {
					outputItemsByIndex[idx] = []byte(item.Raw)
				} else {
					outputItemsFallback = append(outputItemsFallback, []byte(item.Raw))
				}
			}
			continue
		}
		// Patch and keep only the final response.completed / response.done event.
		// The other events are only useful for assembling/patching it; downstream
		// translators expect a single Codex Responses completed object, not a
		// multi-line SSE dump starting with response.created.
		if eventType == "response.completed" || eventType == "response.done" {
			payload = patchCodexCompletedOutput(payload, outputItemsByIndex, outputItemsFallback)
			completedPayload = payload
			if u := extractCodexUsage(payload); u.TotalTokens > 0 || u.InputTokens > 0 || u.OutputTokens > 0 {
				usage = u
			}
		}
	}

	if len(completedPayload) == 0 {
		return nil, fmt.Errorf("codex stream closed before response.completed")
	}
	if streamResult.StatusCode > 0 {
		statusCode = streamResult.StatusCode
	} else {
		statusCode = 200
	}

	// Strip the optional "data: " SSE framing so the downstream non-stream
	// translator sees a plain JSON object.
	responseBody, _ := parseCodexEvent(completedPayload)
	return &Response{
		StatusCode: statusCode,
		Body:       responseBody,
		Headers:    streamResult.Headers,
		Usage:      usage.ToMap(),
	}, nil
}

// ExecuteStream performs a streaming Codex Responses API call.
func (e *CodexExecutor) ExecuteStream(ctx context.Context, req *Request) (*StreamResult, error) {
	url := codexURL(req)
	body := codexRequestBody(req.Body)
	headers := codexHeaders(req)

	cfg := &StreamConfig{
		ScannerMaxTokenSize:     codexScannerMax,
		DropNonstandardCodexSSE: shouldDropNonstandardCodexSSE(),
	}
	result, err := e.DoStreamRequestWithConfig(ctx, "POST", url, headers, body, cfg)
	if err != nil {
		if upErr, ok := err.(*UpstreamError); ok {
			upErr.TranslateErrorBody(req.Provider)
		}
		return nil, err
	}
	return result, nil
}

// ToMap returns the usage as a map suitable for the Response.Usage field.
func (u CodexUsage) ToMap() map[string]int64 {
	return map[string]int64{
		"prompt_tokens":         u.InputTokens,
		"completion_tokens":     u.OutputTokens,
		"total_tokens":          u.TotalTokens,
		"cached_tokens":         u.CachedTokens,
		"cache_creation_tokens": u.CacheCreationTokens,
		"reasoning_tokens":      u.ReasoningTokens,
	}
}

// isNonstandardCodexSSELine reports whether an individual SSE line is an
// "event: codex.*" line that should be dropped before being sent to clients.
func isNonstandardCodexSSELine(line []byte) bool {
	if !shouldDropNonstandardCodexSSE() {
		return false
	}
	trimmed := bytes.TrimSpace(line)
	if !bytes.HasPrefix(trimmed, []byte("event:")) {
		return false
	}
	name := strings.TrimSpace(string(trimmed[6:]))
	return strings.HasPrefix(name, "codex.")
}

// parseCodexEvent extracts the JSON payload and event type from a single SSE
// line. It strips the optional "data:" prefix if present.
func parseCodexEvent(line []byte) ([]byte, string) {
	data := bytes.TrimSpace(line)
	if bytes.HasPrefix(data, []byte("data:")) {
		data = bytes.TrimSpace(data[5:])
	}
	if len(data) == 0 {
		return nil, ""
	}
	return data, gjson.GetBytes(data, "type").String()
}

// patchCodexCompletedOutput reconstructs response.output from collected
// output_item.done events when the upstream response.completed event arrives
// with an empty output array. This mirrors CLIProxyAPI Codex behavior.
func patchCodexCompletedOutput(payload []byte, byIndex map[int64][]byte, fallback [][]byte) []byte {
	data, eventType := parseCodexEvent(payload)
	if (eventType != "response.completed" && eventType != "response.done") || len(data) == 0 {
		return payload
	}
	output := gjson.GetBytes(data, "response.output")
	needsPatch := (!output.Exists() || !output.IsArray() || len(output.Array()) == 0) &&
		(len(byIndex) > 0 || len(fallback) > 0)
	if !needsPatch {
		return payload
	}
	patched, err := sjson.SetRawBytes(data, "response.output", []byte(`[]`))
	if err != nil {
		return payload
	}
	indexes := make([]int64, 0, len(byIndex))
	for idx := range byIndex {
		indexes = append(indexes, idx)
	}
	sort.Slice(indexes, func(i, j int) bool { return indexes[i] < indexes[j] })
	for _, idx := range indexes {
		patched, _ = sjson.SetRawBytes(patched, "response.output.-1", byIndex[idx])
	}
	for _, item := range fallback {
		patched, _ = sjson.SetRawBytes(patched, "response.output.-1", item)
	}
	// Re-encode as an SSE data: line so downstream translators see the same
	// shape as before.
	return append([]byte("data: "), patched...)
}
