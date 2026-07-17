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
		"Content-Type": "application/json",
		"Accept":       "text/event-stream",
		"Authorization": "Bearer " + req.AccessToken,
		"User-Agent":   ua,
		"Originator":   codexOriginator,
		"Connection":   "Keep-Alive",
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

func codexRequestBody(body []byte) []byte {
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
		ScannerMaxTokenSize:       codexScannerMax,
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
