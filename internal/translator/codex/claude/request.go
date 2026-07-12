package claude

import (
	"encoding/json"
	"strconv"
	"strings"
	"unicode"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// convertCodexRequestToClaude transforms a Codex Responses API request into
// Claude Messages format. It handles developer/system prompts, multimodal
// message content, top-level reasoning items, function calls/outputs, web
// search tools, and reasoning effort→thinking budget mapping.
func convertCodexRequestToClaude(modelName string, body []byte, stream bool) []byte {
	root := gjson.ParseBytes(body)

	out := []byte(`{"messages":[]}`)
	out, _ = sjson.SetBytes(out, "model", modelName)
	out, _ = sjson.SetBytes(out, "max_tokens", 4096)
	out, _ = sjson.SetBytes(out, "stream", stream)

	if maxTokens := root.Get("max_output_tokens"); maxTokens.Exists() {
		out, _ = sjson.SetBytes(out, "max_tokens", maxTokens.Int())
	} else if maxTokens := root.Get("max_tokens"); maxTokens.Exists() {
		out, _ = sjson.SetBytes(out, "max_tokens", maxTokens.Int())
	}
	if temp := root.Get("temperature"); temp.Exists() {
		out, _ = sjson.SetBytes(out, "temperature", temp.Float())
	}
	if topP := root.Get("top_p"); topP.Exists() {
		out, _ = sjson.SetBytes(out, "top_p", topP.Float())
	}

	// Collect all system/developer text into a single Claude system array.
	var systemParts []map[string]any
	appendSystemText := func(text string) {
		if text == "" || isClaudeCodeAttributionSystemText(text) {
			return
		}
		systemParts = append(systemParts, map[string]any{"type": "text", "text": text})
	}
	collectSystemResult := func(sys gjson.Result) {
		switch {
		case sys.Type == gjson.String:
			appendSystemText(sys.String())
		case sys.IsArray():
			sys.ForEach(func(_, item gjson.Result) bool {
				if item.Get("type").String() == "text" {
					appendSystemText(item.Get("text").String())
				}
				return true
			})
		}
	}
	if sys := root.Get("system"); sys.Exists() {
		collectSystemResult(sys)
	} else if inst := root.Get("instructions"); inst.Exists() {
		collectSystemResult(inst)
	}

	if input := root.Get("input"); input.Exists() && input.IsArray() {
		input.ForEach(func(_, item gjson.Result) bool {
			itemType := item.Get("type").String()
			switch itemType {
			case "message":
				role := item.Get("role").String()
				if role == "developer" || role == "system" {
					for _, p := range claudeSystemTextParts(item.Get("content")) {
						appendSystemText(p)
					}
				} else {
					out = appendClaudeMessageFromInput(out, item)
				}
			case "function_call":
				out = appendClaudeFunctionCall(out, item)
			case "function_call_output":
				out = appendClaudeFunctionCallOutput(out, item)
			case "reasoning":
				out = appendClaudeReasoning(out, item)
			}
			return true
		})
	}

	if len(systemParts) > 0 {
		out, _ = sjson.SetRawBytes(out, "system", mustMarshal(systemParts))
	}

	if tools := root.Get("tools"); tools.Exists() && tools.IsArray() {
		var claudeTools []map[string]any
		tools.ForEach(func(_, tool gjson.Result) bool {
			claudeTools = append(claudeTools, convertCodexToolToClaude(tool))
			return true
		})
		if len(claudeTools) > 0 {
			out, _ = sjson.SetRawBytes(out, "tools", mustMarshal(claudeTools))
		}
	}

	if tc := root.Get("tool_choice"); tc.Exists() {
		out, _ = sjson.SetRawBytes(out, "tool_choice", convertCodexToolChoiceToClaude(tc))
	}

	if effort := root.Get("reasoning.effort"); effort.Exists() {
		budget := effortToBudget(effort.String())
		if budget > 0 {
			out, _ = sjson.SetRawBytes(out, "thinking", mustMarshal(map[string]any{
				"type":          "enabled",
				"budget_tokens": budget,
			}))
		}
	}

	return out
}

func claudeSystemTextParts(content gjson.Result) []string {
	if !content.Exists() {
		return nil
	}
	if content.Type == gjson.String {
		text := content.String()
		if text == "" || isClaudeCodeAttributionSystemText(text) {
			return nil
		}
		return []string{text}
	}
	if !content.IsArray() {
		return nil
	}
	var parts []string
	content.ForEach(func(_, item gjson.Result) bool {
		if item.Get("type").String() != "input_text" && item.Get("type").String() != "text" {
			return true
		}
		text := item.Get("text").String()
		if text == "" || isClaudeCodeAttributionSystemText(text) {
			return true
		}
		parts = append(parts, text)
		return true
	})
	return parts
}

func appendClaudeMessageFromInput(out []byte, item gjson.Result) []byte {
	role := item.Get("role").String()
	switch role {
	case "assistant", "model":
		role = "assistant"
	case "":
		role = "user"
	default:
		role = "user"
	}

	msg := map[string]any{"role": role}
	content := item.Get("content")
	if content.Type == gjson.String {
		if s := content.String(); s != "" {
			msg["content"] = s
		}
	} else if content.IsArray() {
		var parts []map[string]any
		content.ForEach(func(_, part gjson.Result) bool {
			if p := convertCodexContentPartToClaude(part, role == "assistant"); p != nil {
				parts = append(parts, p)
			}
			return true
		})
		if len(parts) > 0 {
			msg["content"] = parts
		}
	}
	if _, ok := msg["content"]; !ok {
		return out
	}
	out, _ = sjson.SetRawBytes(out, "messages.-1", mustMarshal(msg))
	return out
}

func convertCodexContentPartToClaude(part gjson.Result, isAssistant bool) map[string]any {
	pType := part.Get("type").String()
	switch pType {
	case "input_text", "output_text", "text":
		if text := part.Get("text").String(); text != "" {
			return map[string]any{"type": "text", "text": text}
		}
	case "input_image", "image":
		return parseImagePart(part.Get("image_url").String())
	case "thinking", "reasoning":
		block := map[string]any{"type": "thinking", "thinking": ""}
		if sig := part.Get("encrypted_content").String(); sig != "" {
			block["signature"] = sig
		} else if sig := part.Get("signature").String(); sig != "" {
			block["signature"] = sig
		}
		if summary := part.Get("summary"); summary.Exists() && summary.IsArray() {
			var sb strings.Builder
			summary.ForEach(func(_, s gjson.Result) bool {
				if t := s.Get("text").String(); t != "" {
					sb.WriteString(t)
				} else {
					sb.WriteString(s.String())
				}
				return true
			})
			block["thinking"] = sb.String()
		}
		return block
	}
	return nil
}

func parseImagePart(imageURL string) map[string]any {
	if imageURL == "" {
		return nil
	}
	data, mime, ok := parseDataURL(imageURL)
	if ok {
		return map[string]any{
			"type":   "image",
			"source": map[string]any{"type": "base64", "media_type": mime, "data": data},
		}
	}
	return map[string]any{
		"type":   "image",
		"source": map[string]any{"type": "url", "url": imageURL},
	}
}

func parseDataURL(s string) (data, mime string, ok bool) {
	const prefix = "data:"
	if !strings.HasPrefix(s, prefix) {
		return "", "", false
	}
	rest := s[len(prefix):]
	semi := strings.Index(rest, ";")
	comma := strings.Index(rest, ",")
	if semi <= 0 || comma <= semi || !strings.EqualFold(rest[semi+1:comma], "base64") {
		return "", "", false
	}
	return rest[comma+1:], rest[:semi], true
}

func appendClaudeFunctionCall(out []byte, item gjson.Result) []byte {
	name := item.Get("name").String()
	callID := item.Get("call_id").String()
	if callID == "" {
		callID = item.Get("id").String()
	}
	args := map[string]any{}
	if raw := item.Get("arguments").String(); raw != "" {
		if gjson.Valid(raw) {
			_ = json.Unmarshal([]byte(raw), &args)
		}
	}
	if args == nil {
		args = map[string]any{}
	}
	msg := map[string]any{
		"role": "assistant",
		"content": []map[string]any{{
			"type":  "tool_use",
			"id":    callID,
			"name":  name,
			"input": args,
		}},
	}
	out, _ = sjson.SetRawBytes(out, "messages.-1", mustMarshal(msg))
	return out
}

func appendClaudeFunctionCallOutput(out []byte, item gjson.Result) []byte {
	callID := item.Get("call_id").String()
	if callID == "" {
		callID = item.Get("tool_call_id").String()
	}
	output := item.Get("output")
	var content any
	switch {
	case output.Type == gjson.String:
		content = output.String()
	case output.IsArray():
		var parts []map[string]any
		output.ForEach(func(_, part gjson.Result) bool {
			switch part.Get("type").String() {
			case "input_text", "text":
				parts = append(parts, map[string]any{"type": "text", "text": part.Get("text").String()})
			case "input_image", "image":
				if p := parseImagePart(part.Get("image_url").String()); p != nil {
					parts = append(parts, p)
				}
			}
			return true
		})
		content = parts
	default:
		content = output.String()
	}
	msg := map[string]any{
		"role": "user",
		"content": []map[string]any{{
			"type":        "tool_result",
			"tool_use_id": callID,
			"content":     content,
		}},
	}
	out, _ = sjson.SetRawBytes(out, "messages.-1", mustMarshal(msg))
	return out
}

func appendClaudeReasoning(out []byte, item gjson.Result) []byte {
	block := map[string]any{"type": "thinking", "thinking": ""}
	if sig := item.Get("encrypted_content").String(); sig != "" {
		block["signature"] = sig
	}
	if summary := item.Get("summary"); summary.Exists() && summary.IsArray() {
		var sb strings.Builder
		summary.ForEach(func(_, s gjson.Result) bool {
			if t := s.Get("text").String(); t != "" {
				sb.WriteString(t)
			} else {
				sb.WriteString(s.String())
			}
			return true
		})
		block["thinking"] = sb.String()
	}
	msg := map[string]any{
		"role":    "assistant",
		"content": []map[string]any{block},
	}
	out, _ = sjson.SetRawBytes(out, "messages.-1", mustMarshal(msg))
	return out
}

func convertCodexToolToClaude(tool gjson.Result) map[string]any {
	tType := tool.Get("type").String()
	switch tType {
	case "web_search":
		name := tool.Get("name").String()
		if name == "" {
			name = "web_search"
		}
		out := map[string]any{"type": "web_search_20250305", "name": name}
		if allowed := tool.Get("filters.allowed_domains"); allowed.Exists() && allowed.IsArray() {
			out["allowed_domains"] = jsonRaw(allowed.Raw)
		}
		if loc := tool.Get("user_location"); loc.Exists() && loc.IsObject() {
			out["user_location"] = jsonRaw(loc.Raw)
		}
		return out
	case "function":
		ct := map[string]any{
			"name":        tool.Get("name").String(),
			"description": tool.Get("description").String(),
		}
		if params := tool.Get("parameters"); params.Exists() {
			ct["input_schema"] = jsonRaw(params.Raw)
		} else {
			ct["input_schema"] = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		return ct
	default:
		var raw any
		_ = json.Unmarshal([]byte(tool.Raw), &raw)
		if raw == nil {
			raw = map[string]any{"type": tType}
		}
		return raw.(map[string]any)
	}
}

func convertCodexToolChoiceToClaude(tc gjson.Result) []byte {
	if tc.Type == gjson.String {
		switch tc.String() {
		case "auto":
			return []byte(`"auto"`)
		case "none":
			return []byte(`"none"`)
		case "required":
			return []byte(`"any"`)
		default:
			return []byte(`"auto"`)
		}
	}
	if tc.IsObject() {
		t := tc.Get("type").String()
		switch t {
		case "function":
			name := tc.Get("name").String()
			return mustMarshal(map[string]any{"type": "tool", "name": name})
		case "web_search":
			return mustMarshal(map[string]any{"type": "tool", "name": "web_search"})
		case "auto", "none", "any":
			return []byte(strconv.Quote(t))
		}
	}
	return []byte(`"auto"`)
}

func effortToBudget(effort string) int {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "none":
		return 0
	case "auto":
		return 8192
	case "minimal":
		return 512
	case "low":
		return 1024
	case "medium":
		return 8192
	case "high":
		return 24576
	case "xhigh", "x-high":
		return 32768
	case "max":
		return 128000
	default:
		return 8192
	}
}

func isClaudeCodeAttributionSystemText(text string) bool {
	text = strings.TrimLeftFunc(text, unicode.IsSpace)
	return strings.HasPrefix(text, "x-anthropic-billing-header:")
}

func mustMarshal(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func jsonRaw(raw string) any {
	var v any
	if raw != "" && gjson.Valid(raw) {
		_ = json.Unmarshal([]byte(raw), &v)
	}
	return v
}
