package executor

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// defaultCodexUserAgent is the current Codex CLI default used for upstream requests.
const defaultCodexUserAgent = "codex_cli_rs/0.142.0 (Debian 12.9; x86_64)"

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
	ua := defaultCodexUserAgent
	if req.ProviderSpecificData != nil {
		if v := req.ProviderSpecificData["userAgent"]; v != "" {
			ua = v
		}
	}

	headers := map[string]string{
		"Content-Type":              "application/json",
		"Accept":                    "text/event-stream",
		"Cache-Control":             "no-cache",
		"Authorization":             "Bearer " + req.AccessToken,
		"User-Agent":                ua,
		"Openai-Beta":               "responses=experimental",
		"Originator":                "codex_cli_rs",
		"Codex-Cli-Simplified-Flow": "true",
	}
	if req.APIKey != "" && req.AccessToken == "" {
		headers["Authorization"] = "Bearer " + req.APIKey
	}
	if req.ProviderSpecificData != nil {
		if v := req.ProviderSpecificData["workspaceId"]; v != "" {
			headers["chatgpt-account-id"] = v
		}
	}
	for k, v := range req.Headers {
		headers[k] = v
	}
	return headers
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

	var buf bytes.Buffer
	var statusCode int
	var usage CodexUsage
	// Codex Responses streams `response.output_item.done` events before the final
	// `response.completed`, but the completed event sometimes has an empty
	// `response.output` array. Collect output_item.done items and patch them into
	// the completed event, matching CLIProxyAPI Codex behavior.
	outputItemsByIndex := make(map[int64][]byte)
	var outputItemsFallback [][]byte
	for chunk := range streamResult.Chunks {
		if chunk.Err != nil {
			return nil, fmt.Errorf("codex stream error: %w", chunk.Err)
		}
		if chunk.Payload == nil || isNonstandardCodexSSELine(chunk.Payload) {
			continue
		}
		payload := chunk.Payload
		// Patch the completed event in-place if needed.
		payload = patchCodexCompletedOutput(payload, outputItemsByIndex, outputItemsFallback)
		buf.Write(payload)
		buf.WriteByte('\n')
		if u := extractCodexUsage(payload); u.TotalTokens > 0 || u.InputTokens > 0 || u.OutputTokens > 0 {
			usage = u
		}
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
		}
	}

	if streamResult.StatusCode > 0 {
		statusCode = streamResult.StatusCode
	} else {
		statusCode = 200
	}
	return &Response{
		StatusCode: statusCode,
		Body:       buf.Bytes(),
		Headers:    streamResult.Headers,
		Usage:      usage.ToMap(),
	}, nil
}

// ExecuteStream performs a streaming Codex Responses API call.
func (e *CodexExecutor) ExecuteStream(ctx context.Context, req *Request) (*StreamResult, error) {
	url := codexURL(req)
	body := codexRequestBody(req.Body)
	headers := codexHeaders(req)

	cfg := &StreamConfig{ScannerMaxTokenSize: codexScannerMax}
	result, err := e.DoStreamRequestWithConfig(ctx, "POST", url, headers, body, cfg)
	if err != nil {
		if upErr, ok := err.(*UpstreamError); ok {
			upErr.TranslateErrorBody(req.Provider)
		}
		return nil, err
	}

	if !shouldDropNonstandardCodexSSE() {
		return result, nil
	}

	filtered := &StreamResult{
		Chunks:     make(chan StreamChunk),
		Headers:    result.Headers,
		StatusCode: result.StatusCode,
	}
	go func() {
		defer close(filtered.Chunks)
		for chunk := range result.Chunks {
			if chunk.Err != nil || chunk.Payload == nil {
				filtered.Chunks <- chunk
				continue
			}
			if isNonstandardCodexSSELine(chunk.Payload) {
				continue
			}
			filtered.Chunks <- chunk
		}
	}()
	return filtered, nil
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
	if eventType != "response.completed" || len(data) == 0 {
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
