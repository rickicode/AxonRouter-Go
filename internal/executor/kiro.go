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
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/provider/kiro"
)

// KiroExecutor handles AWS CodeWhisperer / Kiro streaming (AWS EventStream)
// using the generateAssistantResponse endpoint.
type KiroExecutor struct {
	*BaseExecutor
}

// NewKiroExecutor creates a new Kiro executor.
func NewKiroExecutor(base *BaseExecutor) *KiroExecutor {
	return &KiroExecutor{BaseExecutor: base}
}

func genUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func kiroHeaders(req *Request) map[string]string {
	ua := req.ProviderSpecificData["userAgent"]
	if ua == "" {
		ua = "AWS-SDK-JS/3.0.0 kiro-ide/1.0.0"
	}
	headers := map[string]string{
		"Content-Type":    "application/json",
		"Accept":          "application/vnd.amazon.eventstream",
		"User-Agent":      ua,
		"X-Amz-User-Agent": "aws-sdk-js/3.0.0 KiroIDE",
		"X-Amz-Target":    "AmazonCodeWhispererStreamingService.GenerateAssistantResponse",
	}
	if req.AccessToken != "" {
		headers["Authorization"] = "Bearer " + req.AccessToken
	}
	if req.APIKey != "" && req.AccessToken == "" {
		headers["Authorization"] = "Bearer " + req.APIKey
	}
	for k, v := range req.Headers {
		headers[k] = v
	}

	// Apply auth-method-specific headers after upstream overlays so they cannot be
	// accidentally overridden by client-provided headers.
	authMethod := normalizeRegion(req.ProviderSpecificData["authMethod"])
	switch authMethod {
	case "api_key":
		headers["tokentype"] = "API_KEY"
	case "external_idp":
		headers["TokenType"] = "EXTERNAL_IDP"
	}
	return headers
}

// injectKiroProfileArn ensures a profileArn is sent upstream:
//  1. If the connection already has a real profileArn in PSD, use it.
//  2. If the translated request already includes a non-empty profileArn, leave it.
//  3. For OAuth/social/import auth methods, fall back to the shared default placeholder
//     (this is required for builder-id/social tokens because CodeWhisperer rejects
//     requests without a profileArn).
// Account-bound methods (api_key, idc, external_idp) intentionally skip the shared
// placeholder because it belongs to a different account.
func injectKiroProfileArn(body []byte, psd map[string]string) ([]byte, error) {
	psdProfileArn := strings.TrimSpace(psd["profileArn"])
	authMethod := normalizeRegion(psd["authMethod"])
	if authMethod == "api_key" || authMethod == "idc" || authMethod == "external_idp" {
		if psdProfileArn == "" {
			return body, nil
		}
	}

	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return body, err
	}
	if bodyProfileArn, ok := raw["profileArn"].(string); ok && strings.TrimSpace(bodyProfileArn) != "" {
		return body, nil
	}

	if psdProfileArn != "" {
		raw["profileArn"] = psdProfileArn
	} else if authMethod != "api_key" && authMethod != "idc" && authMethod != "external_idp" {
		raw["profileArn"] = resolveDefaultKiroProfileArn(authMethod)
	}

	if raw["profileArn"] == nil || raw["profileArn"] == "" {
		return body, nil
	}
	return json.Marshal(raw)
}

// buildKiroUpstreamBody strips non-upstream fields from the translated body
// and keeps only the fields Kiro accepts.
func buildKiroUpstreamBody(body []byte) ([]byte, map[string]string, error) {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, nil, err
	}
	nameMap := map[string]string{}
	if m, ok := raw["_toolNameMap"].(map[string]any); ok {
		for k, v := range m {
			if s, ok := v.(string); ok {
				nameMap[k] = s
			}
		}
	}
	out := map[string]any{}
	for _, k := range []string{"conversationState", "profileArn", "inferenceConfig", "additionalModelRequestFields", "agentMode", "systemPrompt"} {
		if v, ok := raw[k]; ok && v != nil {
			out[k] = v
		}
	}
	b, err := json.Marshal(out)
	return b, nameMap, err
}

type kiroStreamState struct {
	chunkIndex       int
	responseID       string
	created          int64
	content          strings.Builder
	textLen          int64
	reasoning        strings.Builder
	sawToolUse       bool
	seenToolIDs      map[string]int
	toolIndex        int
	toolArgsBuf      map[string]string
	toolArgsEmitted  map[string]string
	estInputTokens   int64
	estOutputTokens  int64
	hasContextUsage  bool
	contextUsagePct  int64
	hasMeteringEvent bool
	// Inline-thinking splitter state.
	thinkingExpected bool
	thinkingMode     bool
	pendingTag       string
}

func (s *kiroStreamState) toolName(raw string, nameMap map[string]string) string {
	if orig, ok := nameMap[raw]; ok {
		return orig
	}
	return raw
}

// splitInlineThinking walks one slice of upstream content at a time and routes
// characters to either the content channel or the reasoning channel based on the
// current <thinking> state. State is mutated on s so a tag split between frames
// (e.g. "</think" followed by "ing>foo") is still recognised.
func (s *kiroStreamState) splitInlineThinking(content string, model string) [][]byte {
	text := s.pendingTag + content
	s.pendingTag = ""

	// Longest unfinished tag we might still complete on the next frame:
	// "</thinking>" is 11 characters.
	const partialMax = 11

	var out [][]byte
	for text != "" {
		target := "</thinking>"
		if !s.thinkingMode {
			target = "<thinking>"
		}
		idx := strings.Index(text, target)

		if idx == -1 {
			// No full target tag in text. Look for a possible partial at the end
			// so we can complete it on the next frame.
			holdFrom := len(text)
			start := len(text) - partialMax
			if start < 0 {
				start = 0
			}
			for i := start; i < len(text); i++ {
				tail := text[i:]
				if tail != "" && strings.HasPrefix(target, tail) {
					holdFrom = i
					break
				}
			}
			flushable := text[:holdFrom]
			if flushable != "" {
				if s.thinkingMode {
					s.reasoning.WriteString(flushable)
					out = append(out, s.emitChunk(map[string]any{"reasoning_content": flushable}, model))
				} else {
					s.textLen += int64(len(flushable))
					out = append(out, s.emitChunk(map[string]any{"content": flushable}, model))
				}
			}
			s.pendingTag = text[holdFrom:]
			return out
		}

		// Found a complete target tag. Flush everything before it in the
		// current mode, flip the mode, and keep walking the remainder.
		before := text[:idx]
		if before != "" {
			if s.thinkingMode {
				s.reasoning.WriteString(before)
				out = append(out, s.emitChunk(map[string]any{"reasoning_content": before}, model))
			} else {
				s.textLen += int64(len(before))
				out = append(out, s.emitChunk(map[string]any{"content": before}, model))
			}
		}
		s.thinkingMode = !s.thinkingMode
		text = text[idx+len(target):]
	}
	return out
}

// flushPendingThinking drains whatever is left in s.pendingTag at end-of-stream.
func (s *kiroStreamState) flushPendingThinking(model string) [][]byte {
	if s.pendingTag == "" {
		return nil
	}
	text := s.pendingTag
	s.pendingTag = ""
	if s.thinkingMode {
		s.reasoning.WriteString(text)
		return [][]byte{s.emitChunk(map[string]any{"reasoning_content": text}, model)}
	}
	s.textLen += int64(len(text))
	return [][]byte{s.emitChunk(map[string]any{"content": text}, model)}
}

func (s *kiroStreamState) emitChunk(delta map[string]any, model string) []byte {
	if s.chunkIndex == 0 && delta != nil {
		delta["role"] = "assistant"
	}
	chunk := map[string]any{
		"id":      s.responseID,
		"object":  "chat.completion.chunk",
		"created": s.created,
		"model":   model,
		"choices": []any{
			map[string]any{
				"index":         0,
				"delta":         delta,
				"finish_reason": nil,
			},
		},
	}
	s.chunkIndex++
	b, _ := json.Marshal(chunk)
	return []byte("data: " + string(b))
}

func (s *kiroStreamState) emitStartChunk(model string) []byte {
	if s.started() {
		return nil
	}
	return s.emitChunk(map[string]any{}, model)
}

func (s *kiroStreamState) started() bool { return s.responseID != "" }

func (s *kiroStreamState) ensureStarted(model string) []byte {
	if s.started() {
		return nil
	}
	s.responseID = "chatcmpl-" + hex.EncodeToString([]byte(fmt.Sprintf("%d-%s", s.created, genUUID())))[:16]
	s.created = time.Now().Unix()
	return s.emitChunk(map[string]any{}, model)
}

func (s *kiroStreamState) maybeFlushToolArgs(nameMap map[string]string, model string) [][]byte {
	var out [][]byte
	for id, buf := range s.toolArgsBuf {
		toolIdx, ok := s.seenToolIDs[id]
		if !ok {
			continue
		}
		last := s.toolArgsEmitted[id]
		if buf != last {
			delta := map[string]any{
				"tool_calls": []any{
					map[string]any{
						"index": toolIdx,
						"function": map[string]any{
							"arguments": buf,
						},
					},
				},
			}
			out = append(out, s.emitChunk(delta, model))
			s.toolArgsEmitted[id] = buf
		}
	}
	return out
}

func (s *kiroStreamState) handleEvent(frame *EventFrame, nameMap map[string]string, model string) [][]byte {
	if !s.started() {
		s.responseID = genUUID()
		s.created = time.Now().Unix()
	}

	eventType := frame.Headers[":event-type"]
	if eventType == "" && frame.Payload != nil {
		if _, ok := frame.Payload["assistantResponseEvent"]; ok {
			eventType = "assistantResponseEvent"
		} else if _, ok := frame.Payload["reasoningContentEvent"]; ok {
			eventType = "reasoningContentEvent"
		} else if _, ok := frame.Payload["toolUseEvent"]; ok {
			eventType = "toolUseEvent"
		} else if _, ok := frame.Payload["codeEvent"]; ok {
			eventType = "codeEvent"
		} else if _, ok := frame.Payload["messageStopEvent"]; ok {
			eventType = "messageStopEvent"
		} else if _, ok := frame.Payload["usageEvent"]; ok {
			eventType = "usageEvent"
		} else if _, ok := frame.Payload["contextUsageEvent"]; ok {
			eventType = "contextUsageEvent"
		} else if _, ok := frame.Payload["meteringEvent"]; ok {
			eventType = "meteringEvent"
		}
	}

	switch eventType {
	case "assistantResponseEvent", "codeEvent":
		payload := frame.Payload
		if eventType == "assistantResponseEvent" {
			if p, ok := frame.Payload["assistantResponseEvent"].(map[string]any); ok {
				payload = p
			}
		} else if eventType == "codeEvent" {
			if p, ok := frame.Payload["codeEvent"].(map[string]any); ok {
				payload = p
			}
		}
		content, _ := payload["content"].(string)
		if content == "" {
			return nil
		}

		// Kiro may inline thinking tags inside assistantResponseEvent content when the
		// reasoningContentEvent frame is not emitted. Split them stream-safely.
		if s.thinkingExpected {
			return s.splitInlineThinking(content, model)
		}
		s.textLen += int64(len(content))
		return [][]byte{s.emitChunk(map[string]any{"content": content}, model)}

	case "reasoningContentEvent":
		payload := frame.Payload
		if p, ok := frame.Payload["reasoningContentEvent"].(map[string]any); ok {
			payload = p
		}
		text := ""
		if rt, ok := payload["reasoningText"].(map[string]any); ok {
			if v, ok := rt["text"].(string); ok {
				text = v
			} else if v, ok := rt["Text"].(string); ok {
				text = v
			}
		} else if v, ok := payload["text"].(string); ok {
			text = v
		}
		if text == "" {
			return nil
		}
		return [][]byte{s.emitChunk(map[string]any{"reasoning_content": text}, model)}

	case "toolUseEvent":
		toolUse := frame.Payload
		if p, ok := frame.Payload["toolUseEvent"].(map[string]any); ok {
			toolUse = p
		}
		toolUseID, _ := toolUse["toolUseId"].(string)
		if toolUseID == "" {
			toolUseID, _ = toolUse["toolUseId"].(string)
		}
		if toolUseID == "" {
			toolUseID = fmt.Sprintf("call_%d", time.Now().UnixMilli())
		}
		name := ""
		if n, ok := toolUse["name"].(string); ok {
			name = s.toolName(n, nameMap)
		}
		var out [][]byte
		if _, seen := s.seenToolIDs[toolUseID]; !seen {
			s.sawToolUse = true
			s.seenToolIDs[toolUseID] = s.toolIndex
			delta := map[string]any{
				"tool_calls": []any{
					map[string]any{
						"index": s.toolIndex,
						"id":    toolUseID,
						"type":  "function",
						"function": map[string]any{
							"name":      name,
							"arguments": "",
						},
					},
				},
			}
			out = append(out, s.emitChunk(delta, model))
			s.toolIndex++
		}
		if input, ok := toolUse["input"]; ok {
			var args string
			switch v := input.(type) {
			case string:
				args = v
			default:
				b, _ := json.Marshal(v)
				args = string(b)
			}
			if args != "" {
				if s.toolArgsBuf == nil {
					s.toolArgsBuf = make(map[string]string)
				}
				s.toolArgsBuf[toolUseID] = args
				// Emit incrementally if arguments are string deltas, otherwise buffer until flush.
				if _, isStr := input.(string); isStr {
					out = append(out, s.maybeFlushToolArgs(nameMap, model)...)
				}
			}
		}
		return out

	case "messageStopEvent":
		finish := "stop"
		if s.sawToolUse {
			finish = "tool_calls"
		}
		s.estimateUsage()
		usage := map[string]any{}
		if s.estInputTokens > 0 || s.estOutputTokens > 0 {
			usage["prompt_tokens"] = s.estInputTokens
			usage["completion_tokens"] = s.estOutputTokens
			usage["total_tokens"] = s.estInputTokens + s.estOutputTokens
		}
		chunk := map[string]any{
			"id":      s.responseID,
			"object":  "chat.completion.chunk",
			"created": s.created,
			"model":   model,
			"choices": []any{
				map[string]any{
					"index":         0,
					"delta":         map[string]any{},
					"finish_reason": finish,
				},
			},
		}
		if len(usage) > 0 {
			chunk["usage"] = usage
		}
		b, _ := json.Marshal(chunk)
		return [][]byte{[]byte("data: " + string(b))}

	case "contextUsageEvent":
		payload := frame.Payload
		if p, ok := frame.Payload["contextUsageEvent"].(map[string]any); ok {
			payload = p
		}
		if pct, ok := toFloat64(payload["contextUsagePercentage"]); ok && pct > 0 {
			s.hasContextUsage = true
			s.contextUsagePct = int64(pct)
		}
		return nil

	case "meteringEvent":
		s.hasMeteringEvent = true
		return nil

	case "usageEvent":
		payload := frame.Payload
		if p, ok := frame.Payload["usageEvent"].(map[string]any); ok {
			payload = p
		}
		if inTok, ok := toInt64(payload["inputTokens"]); ok {
			s.estInputTokens = inTok
		}
		if outTok, ok := toInt64(payload["outputTokens"]); ok {
			s.estOutputTokens = outTok
		}
		return nil
	}

	return nil
}

func (s *kiroStreamState) estimateUsage() {
	if s.estOutputTokens <= 0 && s.textLen > 0 {
		s.estOutputTokens = int64(s.textLen / 4)
		if s.estOutputTokens < 1 {
			s.estOutputTokens = 1
		}
	}
	if s.estInputTokens <= 0 && s.hasContextUsage && s.contextUsagePct > 0 {
		s.estInputTokens = (s.contextUsagePct * 200000) / 100
	}
}

func toInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case float64:
		return int64(n), true
	case int:
		return int64(n), true
	}
	return 0, false
}

func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int64:
		return float64(n), true
	case int:
		return float64(n), true
	case float32:
		return float64(n), true
	}
	return 0, false
}

// Execute performs a non-streaming Kiro request by collecting the stream.
func (e *KiroExecutor) Execute(ctx context.Context, req *Request) (*Response, error) {
	result, err := e.ExecuteStream(ctx, req)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			return nil, chunk.Err
		}
		if chunk.Payload != nil {
			buf.Write(chunk.Payload)
		}
	}
	// Build a non-streaming chat.completion from the SSE chunks.
	body, err := assembleKiroNonStream(buf.Bytes())
	if err != nil {
		return nil, err
	}
	return &Response{
		StatusCode: result.StatusCode,
		Body:       body,
		Headers:    result.Headers,
	}, nil
}

func assembleKiroNonStream(sse []byte) ([]byte, error) {
	var content, reasoning strings.Builder
	var toolCalls []map[string]any
	var usage map[string]any
	var finishReason string

	for _, line := range bytes.Split(sse, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		data := bytes.TrimSpace(line[5:])
		if len(data) == 0 || bytes.Equal(data, []byte("[DONE]")) {
			continue
		}
		var chunk map[string]any
		if err := json.Unmarshal(data, &chunk); err != nil {
			continue
		}
		choices, _ := chunk["choices"].([]any)
		if len(choices) == 0 {
			if u, ok := chunk["usage"].(map[string]any); ok {
				usage = u
			}
			if fr, ok := chunk["finish_reason"].(string); ok {
				finishReason = fr
			}
			continue
		}
		choice, _ := choices[0].(map[string]any)
		delta, _ := choice["delta"].(map[string]any)
		if v, ok := delta["content"].(string); ok {
			content.WriteString(v)
		}
		if v, ok := delta["reasoning_content"].(string); ok {
			reasoning.WriteString(v)
		}
		if tcs, ok := delta["tool_calls"].([]any); ok && len(tcs) > 0 {
			mergeKiroToolCalls(&toolCalls, tcs)
		}
		if fr, ok := choice["finish_reason"].(string); ok {
			finishReason = fr
		}
		if u, ok := chunk["usage"].(map[string]any); ok {
			usage = u
		}
	}

	out := map[string]any{
		"id":      genUUID(),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   "kiro",
		"choices": []any{},
		"usage":   usage,
	}
	if finishReason == "" && len(toolCalls) > 0 {
		finishReason = "tool_calls"
	}
	msg := map[string]any{
		"role":    "assistant",
		"content": content.String(),
	}
	if reasoning.Len() > 0 {
		msg["content"] = content.String()
		msg["reasoning_content"] = reasoning.String()
	}
	if len(toolCalls) > 0 {
		msg["tool_calls"] = toolCalls
	}
	out["choices"] = []any{
		map[string]any{
			"index":         0,
			"message":       msg,
			"finish_reason": finishReason,
		},
	}
	return json.Marshal(out)
}

func mergeKiroToolCalls(out *[]map[string]any, deltas []any) {
	for _, raw := range deltas {
		d, _ := raw.(map[string]any)
		if d == nil {
			continue
		}
		idxF, _ := d["index"].(float64)
		idx := int(idxF)
		id, _ := d["id"].(string)
		fnDelta, _ := d["function"].(map[string]any)
		name, _ := fnDelta["name"].(string)
		args, _ := fnDelta["arguments"].(string)

		for len(*out) <= idx {
			*out = append(*out, map[string]any{
				"id":       id,
				"type":     "function",
				"function": map[string]any{"name": "", "arguments": ""},
			})
		}
		tc := (*out)[idx]
		if id != "" {
			tc["id"] = id
		}
		if fn, ok := tc["function"].(map[string]any); ok {
			if name != "" {
				fn["name"] = name
			}
			if args != "" {
				fn["arguments"] = fn["arguments"].(string) + args
			}
		}
	}
}

// Models returns the live Kiro model catalog. Falls back to the static catalog
// when the account is offline or unauthenticated, so model listing never breaks.
func (e *KiroExecutor) Models(ctx context.Context, req *Request) (*Response, error) {
	psd := make(map[string]any, len(req.ProviderSpecificData))
	for k, v := range req.ProviderSpecificData {
		psd[k] = v
	}
	result, err := kiro.FetchLiveModels(req.AccessToken, psd)
	if err != nil {
		return nil, err
	}
	type item struct {
		ID string `json:"id"`
	}
	resp := struct {
		Object string `json:"object"`
		Data   []item `json:"data"`
	}{Object: "list", Data: make([]item, 0, len(result.Models))}
	for _, m := range result.Models {
		resp.Data = append(resp.Data, item{ID: m.ID})
	}
	body, _ := json.Marshal(resp)
	return &Response{StatusCode: http.StatusOK, Body: body}, nil
}

// ExecuteStream performs a streaming Kiro request.
func (e *KiroExecutor) ExecuteStream(ctx context.Context, req *Request) (*StreamResult, error) {
	urls := kiroEndpointURLs(req.ProviderSpecificData, req.BaseURL)

	body, err := injectKiroProfileArn(req.Body, req.ProviderSpecificData)
	if err != nil {
		return nil, fmt.Errorf("kiro profile arn injection: %w", err)
	}

	body, nameMap, err := buildKiroUpstreamBody(body)
	if err != nil {
		return nil, fmt.Errorf("kiro upstream body: %w", err)
	}
	headers := kiroHeaders(req)

	var (
		resp    *http.Response
		lastErr error
	)
	for _, url := range urls {
		client, targetURL, err := e.clientForContext(ctx, url, headers)
		if err != nil {
			return nil, err
		}

		httpReq, err := http.NewRequestWithContext(ctx, "POST", targetURL, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("create kiro request: %w", err)
		}
		for k, v := range headers {
			httpReq.Header.Set(k, v)
		}

		// Preserve exact-casing for auth-method-specific headers; net/http.Header.Set
		// canonicalizes keys, but the Kiro/CodeWhisperer surface expects these spellings.
		authMethod := normalizeRegion(req.ProviderSpecificData["authMethod"])
		switch authMethod {
		case "api_key":
			httpReq.Header["tokentype"] = []string{"API_KEY"}
		case "external_idp":
			httpReq.Header["TokenType"] = []string{"EXTERNAL_IDP"}
		}

		resp, err = client.Do(httpReq)
		if err != nil {
			lastErr = err
			resp = nil
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			respBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			lastErr = &UpstreamError{
				StatusCode: resp.StatusCode,
				Body:       respBody,
				RawBody:    respBody,
				Headers:    resp.Header,
			}
			lastErr.(*UpstreamError).TranslateErrorBody(req.Provider)
			resp = nil
			continue
		}
		break
	}
	if resp == nil {
		return nil, fmt.Errorf("kiro request failed: %w", lastErr)
	}

	result := &StreamResult{
		Chunks:     make(chan StreamChunk),
		Headers:    resp.Header,
		StatusCode: resp.StatusCode,
	}

	modelName := req.Model
	if modelName == "" {
		modelName = "kiro"
	}

	thinkingExpected := strings.Contains(string(req.Body), "<thinking_mode>enabled</thinking_mode>")

	go func() {
		defer resp.Body.Close()
		defer close(result.Chunks)

		queue := newByteQueue()
		state := &kiroStreamState{
			seenToolIDs:      make(map[string]int),
			toolArgsBuf:      make(map[string]string),
			toolArgsEmitted:  make(map[string]string),
			thinkingExpected: thinkingExpected,
		}

		buf := make([]byte, 32*1024)
		idleTimeout := e.StreamIdleTimeout
		if idleTimeout <= 0 {
			idleTimeout = 10 * time.Minute
		}

		for {
			_, cancel := context.WithTimeout(ctx, idleTimeout)
			n, readErr := resp.Body.Read(buf)
			cancel()
			if n > 0 {
				queue.push(buf[:n])
				for {
					totalLength, ok := queue.peekUint32BE(0)
					if !ok || totalLength < 16 || queue.len() < int(totalLength) {
						break
					}
					frameData := queue.read(int(totalLength))
					frame, parseErr := parseEventFrame(frameData)
					if parseErr != nil {
						continue
					}
					if start := state.ensureStarted(modelName); start != nil {
						result.Chunks <- StreamChunk{Payload: start}
					}
					chunks := state.handleEvent(frame, nameMap, modelName)
					for _, c := range chunks {
						result.Chunks <- StreamChunk{Payload: c}
					}
				}
			}
			if readErr != nil {
				if readErr != io.EOF {
					result.Chunks <- StreamChunk{Err: readErr}
				}
				break
			}
		}
		// flush remaining buffered tool args and emit final chunk if not emitted
		if state.started() {
			for _, c := range state.flushPendingThinking(modelName) {
				result.Chunks <- StreamChunk{Payload: c}
			}
			for _, c := range state.maybeFlushToolArgs(nameMap, modelName) {
				result.Chunks <- StreamChunk{Payload: c}
			}
			if state.sawToolUse {
				chunks := state.handleEvent(&EventFrame{Headers: map[string]string{":event-type": "messageStopEvent"}}, nameMap, modelName)
				for _, c := range chunks {
					result.Chunks <- StreamChunk{Payload: c}
				}
			}
		}
	}()

	return result, nil
}
