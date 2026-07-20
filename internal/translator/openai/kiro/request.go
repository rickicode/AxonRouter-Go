package kiro

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
)

func init() {
	registry.Register(
		types.FormatOpenAI,
		types.FormatKiro,
		ConvertOpenAIRequestToKiro,
		types.ResponseTransform{},
	)
}

var (
	kiroUnsupportedSuffix = "[1m]"
	kiroModelNormalizeRe  = regexp.MustCompile(`^(claude-(?:opus|sonnet|haiku|3-\d+)-\d+)-(\d{1,2})$`)

	// KiroAdaptiveThinkingModels is the strict allowlist of models that support
	// Kiro's adaptive thinking. Sending it to other models causes 400 errors.
	// Legacy 4.5 and Haiku models are excluded based on live smoke tests.
	kiroAdaptiveThinkingModels = map[string]struct{}{
		"claude-sonnet-4.5": {},
		"claude-sonnet-4":   {},
	}

	// Agentic system prompt (chunked-write assistant) injected for synthetic -agentic variants.
	agenticSystemPrompt = `<system-reminder>
You are an agentic coding assistant. When the user asks you to write, edit, or refactor code, you MUST use the chunked-write protocol:
1. Call the write_file tool for every file you create or modify.
2. Apply edits incrementally — one logical change per tool call.
3. After each edit, verify the result by reading the affected region.
4. Never emit the final code inside the chat message unless explicitly asked.
5. Always prefer deterministic, idiomatic, and production-ready code.
Failure to use the chunked-write protocol will result in rejection.
</system-reminder>`
)

// ConvertOpenAIRequestToKiro translates an OpenAI Chat Completions request
// into the AWS CodeWhisperer / Kiro generateAssistantResponse payload shape.
func ConvertOpenAIRequestToKiro(model string, body []byte, stream bool) []byte {
	if strings.Contains(strings.ToLower(model), kiroUnsupportedSuffix) {
		return mustMarshal(map[string]any{
			"error": map[string]any{
				"message": "Kiro does not support the Anthropic [1m] context-1m beta; use a direct Anthropic provider for 1M context.",
				"type":    "invalid_request_error",
			},
		})
	}

	// Strip provider prefix if present.
	model = strings.TrimPrefix(model, "kiro/")

	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		return body
	}

	normalizedModel := normalizeKiroModel(model)
	messages, _ := req["messages"].([]any)
	tools, _ := req["tools"].([]any)

	// Synthesize tool schemas from history if tools omitted but tool_calls exist.
	if len(tools) == 0 {
		tools = synthesizeToolsFromHistory(messages)
	}

	// Sanitize tool schemas and normalize long tool names. The nameMap is used
	// to restore original names when streaming Kiro responses back to clients.
	sanitizedTools, toolNameMap, err := SanitizeTools(tools)
	if err == nil && len(sanitizedTools) > 0 {
		tools = sanitizedTools
	}

	history, currentMessage := convertMessages(messages, tools, normalizedModel, isAgenticVariant(normalizedModel))
	if currentMessage == nil {
		currentMessage = map[string]any{
			"userInputMessage": map[string]any{
				"content": "(empty)",
				"modelId": normalizedModel,
				"origin":  "AI_EDITOR",
			},
		}
	}

	profileArn, _ := req["profileArn"].(string)

	maxTokens := pickInt(req, "max_tokens", "max_completion_tokens")
	if maxTokens <= 0 {
		maxTokens = 32000
	}
	temperature := pickFloat(req, "temperature")
	topP := pickFloat(req, "top_p")

	currentUserInput, _ := currentMessage["userInputMessage"].(map[string]any)
	if currentUserInput == nil {
		currentUserInput = map[string]any{
			"modelId": normalizedModel,
			"origin":  "AI_EDITOR",
		}
		currentMessage["userInputMessage"] = currentUserInput
	}
	content, _ := currentUserInput["content"].(string)

	// Extract original system messages for the top-level systemPrompt field.
	systemTexts := extractSystemTexts(messages)

	// Build deterministic conversationId from first real user content.
	firstUser := firstRealUserContent(messages, history)
	if firstUser == "" {
		firstUser = content
	}
	conversationID := uuidv5(firstUser[:maxLen(firstUser, 4000)], kiroNamespaceUUID())

	// Freeze and replay the first user message to keep the upstream cache key stable.
	// The translator registry does not expose a connection ID, so the replay cache is
	// keyed by the deterministic conversation ID.
	history = applySessionReplay(conversationID, history, currentMessage)

	effort := ""
	if supportsReasoning(normalizedModel) {
		effort = resolveKiroEffort(req)
	}

	// Assemble the top-level system prompt: thinking directive + agentic prompt + original system texts.
	var systemPromptParts []string
	if effort != "" {
		thinkingLength := capThinkingBudget(normalizedModel, thinkingLengthForEffort(effort))
		systemPromptParts = append(systemPromptParts, fmt.Sprintf("<thinking_mode>enabled</thinking_mode><max_thinking_length>%d</max_thinking_length>", thinkingLength))
	}
	if isAgenticVariant(normalizedModel) {
		systemPromptParts = append(systemPromptParts, agenticSystemPrompt)
	}
	systemPromptParts = append(systemPromptParts, systemTexts...)
	systemPrompt := strings.Join(systemPromptParts, "\n\n")

	// Attach context timestamp and system prompt to current user message.
	content = fmt.Sprintf("[Context: Current time is %s]\n\n%s", time.Now().UTC().Format(time.RFC3339), content)
	if systemPrompt != "" {
		content = systemPrompt + "\n\n" + content
	}
	currentUserInput["content"] = content

	payload := map[string]any{
		"conversationState": map[string]any{
			"chatTriggerType":      "MANUAL",
			"conversationId":       conversationID,
			"agentContinuationId":  conversationID,
			"agentTaskType":        "vibe",
			"currentMessage":       currentMessage,
			"history":              history,
		},
		"agentMode":    "vibe",
		"_toolNameMap": toolNameMap,
	}
	if profileArn != "" {
		payload["profileArn"] = profileArn
	}
	if systemPrompt != "" {
		payload["systemPrompt"] = systemPrompt
	}
	if maxTokens > 0 || temperature != nil || topP != nil {
		inference := map[string]any{}
		if maxTokens > 0 {
			inference["maxTokens"] = maxTokens
		}
		if temperature != nil {
			inference["temperature"] = *temperature
		}
		if topP != nil {
			inference["topP"] = *topP
		}
		payload["inferenceConfig"] = inference
	}
	if effort != "" {
		fields := map[string]any{
			"output_config": map[string]any{"effort": effort},
			"thinking":      map[string]any{"type": "adaptive", "display": "summarized"},
		}
		// Heuristic floor to avoid rejected small max_tokens while thinking.
		if capped := capMaxOutputTokens(normalizedModel, maxTokens); capped > 0 {
			if capped < 1024 {
				capped = 1024
			}
			fields["max_tokens"] = capped
		}
		payload["additionalModelRequestFields"] = fields
		// Strip temperature/topP from inferenceConfig for adaptive-only Claude models.
		if inf, ok := payload["inferenceConfig"].(map[string]any); ok {
			delete(inf, "temperature")
			delete(inf, "topP")
			if len(inf) == 0 {
				delete(payload, "inferenceConfig")
			}
		}
	}

	if stream {
		payload["_stream"] = true
	}

	return mustMarshal(payload)
}

func convertMessages(messages, tools []any, model string, agentic bool) ([]map[string]any, map[string]any) {
	supportsImages := strings.Contains(strings.ToLower(model), "claude")

	var history []map[string]any
	var pendingUser []string
	var pendingAssistant []string
	var pendingToolResults []map[string]any
	var pendingImages []map[string]any
	var currentRole string
	toolsAttached := false

	flush := func() {
		switch currentRole {
		case "user":
			text := strings.TrimSpace(strings.Join(pendingUser, "\n\n"))
			hasContext := len(pendingToolResults) > 0 || len(pendingImages) > 0
			if text == "" && !hasContext {
				text = "(empty)"
			}
			msg := map[string]any{
				"userInputMessage": map[string]any{
					"content": text,
					"modelId": model,
					"origin":  "AI_EDITOR",
				},
			}
			if len(pendingToolResults) > 0 {
				ctx := map[string]any{"toolResults": pendingToolResults}
				msg["userInputMessage"].(map[string]any)["userInputMessageContext"] = ctx
			}
			if len(pendingImages) > 0 {
				msg["userInputMessage"].(map[string]any)["images"] = pendingImages
			}
			if tools != nil && len(tools) > 0 && !toolsAttached {
				xCtx, _ := msg["userInputMessage"].(map[string]any)["userInputMessageContext"].(map[string]any)
				if xCtx == nil {
					xCtx = map[string]any{}
					msg["userInputMessage"].(map[string]any)["userInputMessageContext"] = xCtx
				}
				xCtx["tools"] = buildKiroTools(tools)
				toolsAttached = true
			}
			history = append(history, msg)
			pendingUser = nil
			pendingToolResults = nil
			pendingImages = nil
		case "assistant":
			text := strings.TrimSpace(strings.Join(pendingAssistant, "\n\n"))
			if text == "" {
				text = "(empty)"
			}
			history = append(history, map[string]any{
				"assistantResponseMessage": map[string]any{"content": text},
			})
			pendingAssistant = nil
		}
	}

	if agentic {
		messages = injectAgenticSystemPrompt(messages)
	}

	for _, raw := range messages {
		msg, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		originalRole := role
		if role == "system" || role == "tool" {
			role = "user"
		}
		if currentRole != "" && role != currentRole {
			flush()
		}
		currentRole = role

		if originalRole == "tool" {
			text := serializeToolResultContent(msg["content"])
			pendingToolResults = append(pendingToolResults, map[string]any{
				"toolUseId": msg["tool_call_id"],
				"status":    "success",
				"content":   []map[string]any{{"text": text}},
			})
			continue
		}

		content := msg["content"]
		var text string
		switch v := content.(type) {
		case string:
			text = v
		case []any:
			text = extractTextFromBlocks(v)
				if supportsImages {
					for _, raw := range v {
						if img, ok := raw.(map[string]any); ok {
							format, bytes, url := extractImage(img)
							if bytes != "" {
								pendingImages = append(pendingImages, map[string]any{
									"format": format,
									"source": map[string]any{"bytes": bytes},
								})
							} else if url != "" {
								pendingUser = append(pendingUser, fmt.Sprintf("[Image: %s]", url))
							}
						}
					}
				}
			// Inline tool_result blocks inside content array.
			for _, raw := range v {
				if block, ok := raw.(map[string]any); ok && block["type"] == "tool_result" {
					textResult := serializeToolResultContent(block["content"])
					pendingToolResults = append(pendingToolResults, map[string]any{
						"toolUseId": block["tool_use_id"],
						"status":    "success",
						"content":   []map[string]any{{"text": textResult}},
					})
				}
			}
		}

		if originalRole == "system" && text != "" {
			text = wrapKiroInstructions(text)
		}

		if currentRole == "user" {
			if text != "" {
				pendingUser = append(pendingUser, text)
			}
			if toolCalls, ok := msg["tool_calls"].([]any); ok && len(toolCalls) > 0 {
				flush()
				last := history[len(history)-1]
				arm, _ := last["assistantResponseMessage"].(map[string]any)
				if arm != nil {
					arm["toolUses"] = buildToolUses(toolCalls)
				}
				currentRole = ""
			}
		} else if currentRole == "assistant" {
			if text != "" {
				pendingAssistant = append(pendingAssistant, text)
			}
			if toolCalls, ok := msg["tool_calls"].([]any); ok && len(toolCalls) > 0 {
				flush()
				last := history[len(history)-1]
				arm, _ := last["assistantResponseMessage"].(map[string]any)
				if arm != nil {
					arm["toolUses"] = buildToolUses(toolCalls)
				}
				currentRole = ""
			}
		}
	}
	if currentRole != "" {
		flush()
	}

	// Promote last user turn to currentMessage.
	var currentMessage map[string]any
	if len(history) > 0 {
		last := history[len(history)-1]
		if _, ok := last["userInputMessage"]; ok {
			currentMessage = last
			history = history[:len(history)-1]
		}
	}
	if currentMessage == nil {
		currentMessage = map[string]any{
			"userInputMessage": map[string]any{
				"content": "...",
				"modelId": model,
				"origin":  "AI_EDITOR",
			},
		}
	}

	// Move tools schema to currentMessage if not there.
	if tools != nil && len(tools) > 0 {
		uim, _ := currentMessage["userInputMessage"].(map[string]any)
		ctx, _ := uim["userInputMessageContext"].(map[string]any)
		if ctx == nil || ctx["tools"] == nil {
			if ctx == nil {
				ctx = map[string]any{}
				uim["userInputMessageContext"] = ctx
			}
			ctx["tools"] = buildKiroTools(tools)
		}
	}

	// Strip tool schemas from history; finalize message metadata.
	for _, item := range history {
		if uim, ok := item["userInputMessage"].(map[string]any); ok {
			if ctx, ok := uim["userInputMessageContext"].(map[string]any); ok {
				delete(ctx, "tools")
				if len(ctx) == 0 {
					delete(uim, "userInputMessageContext")
				}
			}
			if _, ok := uim["modelId"]; !ok || uim["modelId"] == "" {
				uim["modelId"] = model
			}
			if _, ok := uim["origin"]; !ok || uim["origin"] == "" {
				uim["origin"] = "AI_EDITOR"
			}
		}
	}

	// Merge consecutive same-role messages.
	history = mergeConsecutive(history)

	// Ensure conversation starts with a user turn.
	if len(history) > 0 {
		if _, ok := history[0]["assistantResponseMessage"]; ok {
			history = append([]map[string]any{{
				"userInputMessage": map[string]any{"content": "(empty)", "modelId": model, "origin": "AI_EDITOR"},
			}}, history...)
		}
	}

	// Ensure toolResults have a preceding assistant toolUses; otherwise convert to text.
	history = reconcileToolResults(history)

	// Ensure alternating roles (insert synthetic assistant between consecutive users).
	history = alternateRoles(history)

	return history, currentMessage
}

func wrapKiroInstructions(text string) string {
	return "<instructions>\n" + text + "\n</instructions>"
}

// extractSystemTexts pulls role=system messages out of the OpenAI request so
// they can be sent as Kiro's top-level systemPrompt in addition to being folded
// into the user content prefix.
func extractSystemTexts(messages []any) []string {
	var out []string
	for _, raw := range messages {
		msg, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		if role != "system" {
			continue
		}
		content := msg["content"]
		switch v := content.(type) {
		case string:
			if v != "" {
				out = append(out, v)
			}
		case []any:
			text := extractTextFromBlocks(v)
			if text != "" {
				out = append(out, text)
			}
		}
	}
	return out
}

func injectAgenticSystemPrompt(messages []any) []any {
	if agenticSystemPrompt == "" {
		return messages
	}
	out := make([]any, 0, len(messages)+1)
	injected := false
	for _, raw := range messages {
		msg, ok := raw.(map[string]any)
		if !ok {
			out = append(out, raw)
			continue
		}
		if !injected && msg["role"] == "system" {
			content, _ := msg["content"].(string)
			msg["content"] = content + "\n\n" + agenticSystemPrompt
			injected = true
		} else if !injected && msg["role"] == "user" {
			out = append(out, map[string]any{"role": "system", "content": agenticSystemPrompt})
			injected = true
		}
		out = append(out, msg)
	}
	if !injected && len(out) == 0 {
		out = append(out, map[string]any{"role": "system", "content": agenticSystemPrompt})
	}
	return out
}

func extractTextFromBlocks(blocks []any) string {
	var parts []string
	for _, raw := range blocks {
		block, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if block["type"] == "text" {
			if t, ok := block["text"].(string); ok {
				parts = append(parts, t)
			}
		}
	}
	return strings.Join(parts, "\n")
}

func extractImage(block map[string]any) (format, bytes, url string) {
	btype, _ := block["type"].(string)
	switch btype {
	case "image_url":
		iu, _ := block["image_url"].(map[string]any)
		url, _ = iu["url"].(string)
		format, bytes = parseDataURL(url)
		return format, bytes, url
	case "image":
		src, _ := block["source"].(map[string]any)
		if src["type"] == "base64" {
			mediaType, _ := src["media_type"].(string)
			return extFromMime(mediaType), asString(src["data"]), ""
		}
		if img, ok := block["image"].(string); ok {
			format, bytes = parseDataURL(img)
			return format, bytes, img
		}
	}
	return "", "", ""
}

func parseDataURL(url string) (format, bytes string) {
	if !strings.HasPrefix(url, "data:") {
		return "", ""
	}
	parts := strings.SplitN(url, ",", 2)
	if len(parts) != 2 {
		return "", ""
	}
	mimePart := strings.TrimPrefix(parts[0], "data:")
	mimeType := strings.Split(mimePart, ";")[0]
	return extFromMime(mimeType), parts[1]
}

func extFromMime(mime string) string {
	parts := strings.Split(mime, "/")
	if len(parts) == 2 {
		return parts[1]
	}
	return "jpeg"
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func serializeToolResultContent(content any) string {
	if s, ok := content.(string); ok {
		if s == "" {
			return "(no output)"
		}
		return s
	}
	if arr, ok := content.([]any); ok {
		var parts []string
		for _, raw := range arr {
			block, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if block["type"] == "text" {
				if t, ok := block["text"].(string); ok && t != "" {
					parts = append(parts, t)
				}
			} else if block["type"] == "image" || block["type"] == "image_url" {
				parts = append(parts, "[image]")
			} else {
				b, _ := json.Marshal(block)
				if len(b) > 0 && string(b) != "{}" {
					parts = append(parts, string(b))
				}
			}
		}
		if len(parts) == 0 {
			return "(no output)"
		}
		return strings.Join(parts, "\n")
	}
	if content == nil {
		return "(no output)"
	}
	b, _ := json.Marshal(content)
	if len(b) > 0 && string(b) != "{}" {
		return string(b)
	}
	return "(no output)"
}

func buildToolUses(toolCalls []any) []map[string]any {
	var out []map[string]any
	for _, raw := range toolCalls {
		tc, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		toolUseID, _ := tc["id"].(string)
		fn, _ := tc["function"].(map[string]any)
		name := ""
		if fn != nil {
			name, _ = fn["name"].(string)
		} else {
			name, _ = tc["name"].(string)
		}
		args := ""
		if fn != nil {
			args, _ = fn["arguments"].(string)
		} else {
			if a, ok := tc["input"].(string); ok {
				args = a
			}
		}
		if toolUseID == "" {
			toolUseID = fmt.Sprintf("call_%d", time.Now().UnixMilli())
		}
		var input map[string]any
		if args != "" {
			_ = json.Unmarshal([]byte(args), &input)
		}
		if input == nil {
			input = map[string]any{}
		}
		out = append(out, map[string]any{
			"toolUseId": toolUseID,
			"name":      name,
			"input":     input,
		})
	}
	return out
}

func buildKiroTools(tools []any) []map[string]any {
	const kiroMaxToolDescriptionLen = 10000
	var out []map[string]any
	for _, raw := range tools {
		tool, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		fn := tool
		if f, ok := tool["function"].(map[string]any); ok {
			fn = f
		}
		name, _ := fn["name"].(string)
		desc, _ := fn["description"].(string)
		if desc == "" {
			desc = fmt.Sprintf("Tool: %s", name)
		} else if len(desc) > kiroMaxToolDescriptionLen {
			desc = desc[:kiroMaxToolDescriptionLen-len(" …")] + " …"
		}
		params, _ := fn["parameters"].(map[string]any)
		if params == nil {
			params = tool["parameters"].(map[string]any)
		}
		out = append(out, map[string]any{
			"toolSpecification": map[string]any{
				"name":        name,
				"description": desc,
				"inputSchema": map[string]any{"json": normalizeKiroToolSchema(params)},
			},
		})
	}
	return out
}

func normalizeKiroToolSchema(schema map[string]any) map[string]any {
	if schema == nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	out := map[string]any{}
	for k, v := range schema {
		switch {
		case k == "required" && isEmptyArray(v):
			continue
		case k == "additionalProperties":
			continue
		case k == "properties" && isObject(v):
			props := map[string]any{}
			for propName, propValue := range v.(map[string]any) {
				props[propName] = normalizeKiroToolSchema(asObject(propValue))
			}
			out[k] = props
		case isObject(v):
			out[k] = normalizeKiroToolSchema(v.(map[string]any))
		case isArray(v):
			arr := v.([]any)
			next := make([]any, len(arr))
			for i, item := range arr {
				next[i] = normalizeKiroToolSchema(asObject(item))
			}
			out[k] = next
		default:
			out[k] = v
		}
	}
	return out
}

func synthesizeToolsFromHistory(messages []any) []any {
	seen := map[string]bool{}
	var tools []any
	push := func(name string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		tools = append(tools, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        name,
				"description": fmt.Sprintf("Tool: %s", name),
				"parameters": map[string]any{
					"type":       "object",
					"properties": map[string]any{},
					"required":   []any{},
				},
			},
		})
	}
	for _, raw := range messages {
		msg, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if msg["role"] != "assistant" {
			continue
		}
		if tcs, ok := msg["tool_calls"].([]any); ok {
			for _, raw := range tcs {
				tc, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				fn, _ := tc["function"].(map[string]any)
				if fn != nil {
					push(fn["name"].(string))
				} else {
					push(tc["name"].(string))
				}
			}
		}
		if blocks, ok := msg["content"].([]any); ok {
			for _, raw := range blocks {
				block, ok := raw.(map[string]any)
				if !ok || block["type"] != "tool_use" {
					continue
				}
				push(block["name"].(string))
			}
		}
	}
	return tools
}

func mergeConsecutive(history []map[string]any) []map[string]any {
	var merged []map[string]any
	for _, item := range history {
		if len(merged) == 0 {
			merged = append(merged, item)
			continue
		}
		prev := merged[len(merged)-1]
		if uim, ok := item["userInputMessage"].(map[string]any); ok {
			if puim, ok := prev["userInputMessage"].(map[string]any); ok {
				puim["content"] = joinContent(asString(puim["content"]), asString(uim["content"]))
				if ctx1, ok1 := puim["userInputMessageContext"].(map[string]any); ok1 {
					if ctx2, ok2 := uim["userInputMessageContext"].(map[string]any); ok2 {
						for k, v := range ctx2 {
							if existing, ok := ctx1[k].([]any); ok {
								if arr, ok := v.([]any); ok {
									ctx1[k] = append(existing, arr...)
									continue
								}
							}
							ctx1[k] = v
						}
					}
				}
				continue
			}
		}
		if arm, ok := item["assistantResponseMessage"].(map[string]any); ok {
			if parm, ok := prev["assistantResponseMessage"].(map[string]any); ok {
				parm["content"] = joinContent(asString(parm["content"]), asString(arm["content"]))
				if uses1, ok1 := parm["toolUses"].([]map[string]any); ok1 {
					if uses2, ok2 := arm["toolUses"].([]map[string]any); ok2 {
						parm["toolUses"] = append(uses1, uses2...)
					}
				}
				continue
			}
		}
		merged = append(merged, item)
	}
	return merged
}

func joinContent(a, b string) string {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	return a + "\n\n" + b
}

func reconcileToolResults(history []map[string]any) []map[string]any {
	for i, item := range history {
		uim, ok := item["userInputMessage"].(map[string]any)
		if !ok {
			continue
		}
		ctx, ok := uim["userInputMessageContext"].(map[string]any)
		if !ok {
			continue
		}
		trs, ok := ctx["toolResults"].([]any)
		if !ok || len(trs) == 0 {
			continue
		}
		preceding := false
		if i > 0 {
			if prevArm, ok := history[i-1]["assistantResponseMessage"].(map[string]any); ok {
				if uses, ok := prevArm["toolUses"].([]map[string]any); ok && len(uses) > 0 {
					preceding = true
				}
			}
		}
		if preceding {
			continue
		}
		var texts []string
		for _, raw := range trs {
			tr, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			id, _ := tr["toolUseId"].(string)
			var text string
			if contents, ok := tr["content"].([]any); ok {
				text = extractTextFromBlocks(contents)
			}
			if id != "" {
				texts = append(texts, fmt.Sprintf("[Tool Result (%s)]\n%s", id, text))
			} else {
				texts = append(texts, fmt.Sprintf("[Tool Result]\n%s", text))
			}
		}
		content := strings.Join(texts, "\n\n")
		if uim["content"] != "" {
			content = asString(uim["content"]) + "\n\n" + content
		}
		uim["content"] = content
		delete(ctx, "toolResults")
		if len(ctx) == 0 {
			delete(uim, "userInputMessageContext")
		}
	}
	return history
}

func alternateRoles(history []map[string]any) []map[string]any {
	var out []map[string]any
	for _, item := range history {
		lastUser := false
		if len(out) > 0 {
			if _, ok := out[len(out)-1]["userInputMessage"]; ok {
				lastUser = true
			}
		}
		if _, isUser := item["userInputMessage"]; isUser && lastUser {
			out = append(out, map[string]any{
				"assistantResponseMessage": map[string]any{"content": "(empty)"},
			})
		}
		out = append(out, item)
	}
	return out
}

func firstRealUserContent(messages []any, history []map[string]any) string {
	for _, raw := range messages {
		msg, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if msg["role"] != "user" {
			continue
		}
		switch v := msg["content"].(type) {
		case string:
			return v
		case []any:
			return extractTextFromBlocks(v)
		}
	}
	for _, raw := range history {
		item := raw
		if uim, ok := item["userInputMessage"].(map[string]any); ok {
			if c := asString(uim["content"]); c != "" && c != "(empty)" && c != "..." {
				return c
			}
		}
	}
	return ""
}

func normalizeKiroModel(model string) string {
	model = strings.TrimSpace(model)
	return kiroModelNormalizeRe.ReplaceAllString(model, "${1}.${2}")
}

func resolveKiroEffort(req map[string]any) string {
	effort := ""
	if r, ok := req["reasoning_effort"].(string); ok {
		effort = strings.ToLower(r)
	}
	if effort == "" {
		if oc, ok := req["output_config"].(map[string]any); ok {
			if e, ok := oc["effort"].(string); ok {
				effort = strings.ToLower(e)
			}
		}
	}
	if effort == "" {
		if thinking, ok := req["thinking"].(map[string]any); ok {
			switch thinking["type"] {
			case "enabled":
				if budget, ok := thinking["budget_tokens"].(float64); ok {
					effort = effortFromBudget(int(budget))
				}
			case "adaptive":
				effort = "high"
			}
		}
	}
	if effort == "minimal" {
		effort = "low"
	}
	for _, l := range []string{"low", "medium", "high", "xhigh", "max"} {
		if l == effort {
			return effort
		}
	}
	return ""
}

func effortFromBudget(budget int) string {
	switch {
	case budget >= 32000:
		return "high"
	case budget >= 16000:
		return "medium"
	case budget > 0:
		return "low"
	default:
		return ""
	}
}

func thinkingLengthForEffort(effort string) int {
	switch effort {
	case "max":
		return 120000
	case "xhigh":
		return 64000
	case "high":
		return 32000
	case "medium":
		return 16000
	default:
		return 8000
	}
}

func supportsReasoning(model string) bool {
	m := strings.ToLower(model)
	_, ok := kiroAdaptiveThinkingModels[m]
	return ok
}

func isAgenticVariant(model string) bool {
	return strings.HasSuffix(strings.ToLower(model), "-agentic")
}

func capThinkingBudget(model string, budget int) int {
	// Simplified caps: 128k for newer Claude, 64k for Opus, 32k for Sonnet, 8k for Haiku.
	m := strings.ToLower(model)
	cap := 128000
	if strings.Contains(m, "haiku") {
		cap = 8000
	} else if strings.Contains(m, "sonnet") {
		cap = 32000
	} else if strings.Contains(m, "opus") {
		cap = 64000
	}
	if budget > cap {
		return cap
	}
	if budget < 0 {
		return 0
	}
	return budget
}

func capMaxOutputTokens(model string, maxTokens int) int {
	m := strings.ToLower(model)
	cap := 64000
	if strings.Contains(m, "haiku") {
		cap = 32000
	}
	if maxTokens > cap {
		return cap
	}
	if maxTokens < 1024 {
		return 1024
	}
	return maxTokens
}

func pickInt(req map[string]any, keys ...string) int {
	for _, k := range keys {
		switch v := req[k].(type) {
		case float64:
			return int(v)
		case int:
			return v
		case int64:
			return int(v)
		}
	}
	return 0
}

func pickFloat(req map[string]any, key string) *float64 {
	switch v := req[key].(type) {
	case float64:
		return &v
	case int:
		f := float64(v)
		return &f
	}
	return nil
}

func isEmptyArray(v any) bool {
	arr, ok := v.([]any)
	return ok && len(arr) == 0
}

func isObject(v any) bool {
	_, ok := v.(map[string]any)
	return ok
}

func isArray(v any) bool {
	_, ok := v.([]any)
	return ok
}

func asObject(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func maxLen(s string, n int) int {
	if len(s) > n {
		return n
	}
	return len(s)
}

func mustMarshal(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func kiroNamespaceUUID() string {
	// deterministic namespace UUID string
	return "34f7193f-561d-4050-bc84-9547d953d6bf"
}

func uuidv5(name, namespace string) string {
	uuidBytes := namespaceUUID(namespace)
	h := sha1.New()
	h.Write(uuidBytes)
	h.Write([]byte(name))
	sum := h.Sum(nil)
	// variant and version per UUIDv5
	sum[6] = (sum[6] & 0x0f) | 0x50
	sum[8] = (sum[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}

func namespaceUUID(s string) []byte {
	uuid, err := hex.DecodeString(strings.ReplaceAll(s, "-", ""))
	if err != nil {
		return []byte{}
	}
	return uuid
}
