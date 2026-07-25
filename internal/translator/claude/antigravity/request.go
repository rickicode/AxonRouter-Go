// Package antigravity provides Claude → Antigravity request translation.
// It ports the core logic from CLIProxyAPI so Kiro / Claude Code clients can
// talk to an Antigravity (Gemini Cloud Code Assist) backend directly.
package antigravity

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/rickicode/AxonRouter-Go/internal/cache"
	"github.com/rickicode/AxonRouter-Go/internal/signature"
	"github.com/rickicode/AxonRouter-Go/internal/translator/antigravity"
	"github.com/rickicode/AxonRouter-Go/internal/translator/antigravity/openai"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const antigravityFunctionThoughtSignature = "skip_thought_signature_validator"

var functionNameSanitizer = regexp.MustCompile(`[^a-zA-Z0-9_.:-]`)

// convertClaudeRequestToAntigravity transforms a Claude Messages API request into
// Antigravity's Gemini-compatible envelope format.
func convertClaudeRequestToAntigravity(modelName string, inputRawJSON []byte, _ bool) []byte {
	rawJSON := signature.StripInvalidClaudeThinkingBlocks(inputRawJSON)
	rawJSON = stripTrailingEmptyAssistant(rawJSON)

	out := []byte(`{"model":"","request":{"contents":[]}}`)
	out, _ = sjson.SetBytes(out, "model", modelName)

	functionNameMap := buildFunctionNameMap(rawJSON)

	if genConfig := gjson.GetBytes(rawJSON, "generationConfig"); genConfig.Exists() {
		out, _ = sjson.SetRawBytes(out, "request.generationConfig", []byte(genConfig.Raw))
	}
	if v := gjson.GetBytes(rawJSON, "temperature"); v.Exists() && v.Type == gjson.Number {
		out, _ = sjson.SetBytes(out, "request.generationConfig.temperature", v.Num)
	}
	if v := gjson.GetBytes(rawJSON, "top_p"); v.Exists() && v.Type == gjson.Number {
		out, _ = sjson.SetBytes(out, "request.generationConfig.topP", v.Num)
	}
	if v := gjson.GetBytes(rawJSON, "top_k"); v.Exists() && v.Type == gjson.Number {
		out, _ = sjson.SetBytes(out, "request.generationConfig.topK", v.Num)
	}
	if v := gjson.GetBytes(rawJSON, "max_tokens"); v.Exists() && v.Type == gjson.Number {
		out, _ = sjson.SetBytes(out, "request.generationConfig.maxOutputTokens", v.Num)
	}

	// System instruction
	if sys := gjson.GetBytes(rawJSON, "system"); sys.Exists() {
		var parts [][]byte
		if sys.IsArray() {
			for _, item := range sys.Array() {
				if item.Get("type").String() == "text" {
					text := strings.TrimSpace(item.Get("text").String())
					if text != "" {
						parts = append(parts, mustMarshalJSON(map[string]any{"text": text}))
					}
				}
			}
		} else if sys.Type == gjson.String {
			if text := strings.TrimSpace(sys.String()); text != "" {
				parts = append(parts, mustMarshalJSON(map[string]any{"text": text}))
			}
		}
		if len(parts) > 0 {
			content := antigravityContent("user", parts)
			out, _ = sjson.SetRawBytes(out, "request.systemInstruction", content)
		}
	}

	// Messages + tool_lookup
	toolNameByID := map[string]string{}
	messages := gjson.GetBytes(rawJSON, "messages")
	if messages.IsArray() {
		for i, msg := range messages.Array() {
			role := msg.Get("role").String()
			if role == "" {
				continue
			}
			agRole := role
			if role == "assistant" {
				agRole = "model"
			} else if role == "system" {
				agRole = "user"
			}

			content := msg.Get("content")
			if role == "system" && content.Type == gjson.String {
				if text := strings.TrimSpace(content.String()); text != "" {
					out, _ = sjson.SetRawBytes(out, "request.contents.-1", antigravityContent("user", [][]byte{
						mustMarshalJSON(map[string]any{"text": text}),
					}))
				}
				continue
			}

			var parts [][]byte
			if content.IsArray() {
				for _, block := range content.Array() {
					blockType := block.Get("type").String()
					switch blockType {
					case "thinking":
						// Invalid thinking blocks are already stripped earlier.
						thinkingText := block.Get("text").String()
						if thinkingText == "" {
							thinkingText = block.Get("thinking").String()
						}
						if thinkingText == "" {
							continue
						}
						part := map[string]any{"thought": true, "text": thinkingText}
						if sig := block.Get("signature").String(); sig != "" {
							if normalized, err := signature.NormalizeClaudeThinkingSignature(sig); err == nil {
								part["thoughtSignature"] = normalized
							}
						}
						parts = append(parts, mustMarshalJSON(part))
					case "text":
						text := strings.TrimSpace(block.Get("text").String())
						if text == "" {
							continue
						}
						parts = append(parts, mustMarshalJSON(map[string]any{"text": text}))
					case "tool_use":
						originalName := block.Get("name").String()
						functionName := mapFunctionName(functionNameMap, originalName)
						toolID := block.Get("id").String()
						if toolID != "" && originalName != "" {
							toolNameByID[toolID] = originalName
						}
						argsRaw := rawArgs(block.Get("input"))
						if argsRaw == "" {
							argsRaw = "{}"
						}
						part := map[string]any{
							"functionCall": map[string]any{
								"id":   toolID,
								"name": functionName,
							},
						}
						if toolID != "" {
							// Keep id on the functionCall for round-trip correlation.
						}
						part["functionCall"].(map[string]any)["args"] = json.RawMessage(argsRaw)
						sig := block.Get("signature").String()
						if normalized, err := signature.NormalizeClaudeThinkingSignature(sig); err == nil {
							part["thoughtSignature"] = normalized
						} else if sig != "" {
							part["thoughtSignature"] = antigravityFunctionThoughtSignature
						} else {
							part["thoughtSignature"] = antigravityFunctionThoughtSignature
						}
						parts = append(parts, mustMarshalJSON(part))
					case "tool_result":
						toolCallID := block.Get("tool_use_id").String()
						funcName, ok := toolNameByID[toolCallID]
						if !ok {
							funcName = deriveToolNameFromID(toolCallID)
						}
						fr := map[string]any{
							"id":   toolCallID,
							"name": mapFunctionName(functionNameMap, funcName),
						}
						contentVal := block.Get("content")
						if contentVal.Type == gjson.String {
							fr["response"] = map[string]any{"result": contentVal.String()}
						} else if contentVal.IsObject() || contentVal.IsArray() {
							fr["response"] = map[string]any{"result": json.RawMessage(contentVal.Raw)}
						} else {
							fr["response"] = map[string]any{"result": ""}
						}
						parts = append(parts, mustMarshalJSON(map[string]any{"functionResponse": fr}))
					case "image":
						if srcType := block.Get("source.type").String(); srcType == "base64" {
							inline := map[string]any{
								"mimeType": block.Get("source.media_type").String(),
								"data":     block.Get("source.data").String(),
							}
							parts = append(parts, mustMarshalJSON(map[string]any{"inlineData": inline}))
						}
					}
				}
			} else if content.Type == gjson.String {
				if text := strings.TrimSpace(content.String()); text != "" {
					parts = append(parts, mustMarshalJSON(map[string]any{"text": text}))
				}
			}

			// Model turns: ensure thinking -> text -> functionCall ordering.
			if agRole == "model" && len(parts) > 1 {
				parts = reorderModelParts(parts)
			}

			// Pre-populate tool id -> name mapping from assistant tool_use blocks
			// before we drop the message if it is empty.
			_ = i

			if len(parts) == 0 {
				continue
			}
			out, _ = sjson.SetRawBytes(out, "request.contents.-1", antigravityContent(agRole, parts))
		}
	}

	// Tools
	tools := gjson.GetBytes(rawJSON, "tools")
	if tools.IsArray() {
		var functionDecls [][]byte
		for _, tool := range tools.Array() {
			if tool.Get("type").String() != "function" {
				continue
			}
			inputSchema := tool.Get("input_schema")
			if !inputSchema.Exists() || !inputSchema.IsObject() {
				continue
			}
			originalName := tool.Get("name").String()
			mappedName := mapFunctionName(functionNameMap, originalName)

			var schemaMap map[string]any
			_ = json.Unmarshal([]byte(inputSchema.Raw), &schemaMap)
			schemaMap = antigravity.StripEnumDescriptions(schemaMap)
			if schemaMap == nil {
				schemaMap = map[string]any{"type": "object", "properties": map[string]any{}}
			}
			schemaJSON, _ := json.Marshal(schemaMap)

			decl := map[string]any{
				"name":                 mappedName,
				"description":          tool.Get("description").String(),
				"parametersJsonSchema": json.RawMessage(schemaJSON),
			}
			functionDecls = append(functionDecls, mustMarshalJSON(decl))
		}
		if len(functionDecls) > 0 {
			for _, decoy := range antigravity.AGDecoyToolNames {
				functionDecls = append(functionDecls, mustMarshalJSON(map[string]any{
					"name":                 decoy,
					"description":          "This tool is currently unavailable.",
					"parametersJsonSchema": map[string]any{"type": "object", "properties": map[string]any{}},
				}))
			}
			toolNode := []byte(`{"functionDeclarations":[]}`)
			toolNode, _ = sjson.SetRawBytes(toolNode, "functionDeclarations", joinRaw(functionDecls))
			out, _ = sjson.SetRawBytes(out, "request.tools", []byte(`[`+string(toolNode)+`]`))

			mode := "AUTO"
			if tc := gjson.GetBytes(rawJSON, "tool_choice"); tc.Exists() {
				switch tc.Get("type").String() {
				case "auto", "":
					mode = "AUTO"
				case "none":
					mode = "NONE"
				case "any":
					mode = "ANY"
				case "tool":
					mode = "ANY"
					if name := tc.Get("name").String(); name != "" {
						out, _ = sjson.SetBytes(out, "request.toolConfig.functionCallingConfig.allowedFunctionNames", []string{mapFunctionName(functionNameMap, name)})
					}
				}
				} else if tc.Type == gjson.String {
					switch tc.String() {
					case "auto", "":
						mode = "AUTO"
					case "none":
						mode = "NONE"
					case "any":
						mode = "ANY"
					}
				}
			out, _ = sjson.SetBytes(out, "request.toolConfig.functionCallingConfig.mode", mode)
		}
	}

	// Thinking config
	if t := gjson.GetBytes(rawJSON, "thinking"); t.Exists() && t.IsObject() {
		switch t.Get("type").String() {
		case "enabled":
			if budget := t.Get("budget_tokens"); budget.Exists() && budget.Type == gjson.Number {
				out, _ = sjson.SetBytes(out, "request.generationConfig.thinkingConfig.thinkingBudget", budget.Int())
			}
			out, _ = sjson.SetBytes(out, "request.generationConfig.thinkingConfig.includeThoughts", true)
		case "adaptive", "auto":
			out, _ = sjson.SetBytes(out, "request.generationConfig.thinkingConfig.thinkingLevel", "high")
			out, _ = sjson.SetBytes(out, "request.generationConfig.thinkingConfig.includeThoughts", true)
		}
	}

	return openai.AttachDefaultSafetySettings(out, "request.safetySettings")
}

func antigravityContent(role string, parts [][]byte) []byte {
	content := map[string]any{
		"role":  role,
		"parts": json.RawMessage(joinRaw(parts)),
	}
	b, _ := json.Marshal(content)
	return b
}

func joinRaw(parts [][]byte) []byte {
	var sb strings.Builder
	sb.WriteByte('[')
	for i, p := range parts {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.Write(p)
	}
	sb.WriteByte(']')
	return []byte(sb.String())
}

func mustMarshalJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func rawArgs(input gjson.Result) string {
	if input.IsObject() {
		return input.Raw
	}
	if input.Type == gjson.String {
		parsed := gjson.Parse(input.String())
		if parsed.IsObject() {
			return parsed.Raw
		}
	}
	return ""
}

func buildFunctionNameMap(rawJSON []byte) map[string]string {
	tools := gjson.GetBytes(rawJSON, "tools")
	if !tools.IsArray() {
		return nil
	}
	m := map[string]string{}
	for _, tool := range tools.Array() {
		if tool.Get("type").String() != "function" {
			continue
		}
		name := tool.Get("name").String()
		if name == "" {
			continue
		}
		m[name] = antigravity.CloakName(sanitizeFunctionName(name))
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

func mapFunctionName(nameMap map[string]string, name string) string {
	if nameMap == nil {
		return antigravity.CloakName(sanitizeFunctionName(name))
	}
	if mapped, ok := nameMap[name]; ok {
		return mapped
	}
	return antigravity.CloakName(sanitizeFunctionName(name))
}

func sanitizeFunctionName(name string) string {
	if name == "" {
		return ""
	}
	sanitized := functionNameSanitizer.ReplaceAllString(name, "_")
	if len(sanitized) > 0 {
		first := sanitized[0]
		if !((first >= 'a' && first <= 'z') || (first >= 'A' && first <= 'Z') || first == '_') {
			sanitized = "_" + sanitized
		}
	} else {
		sanitized = "_"
	}
	if len(sanitized) > 64 {
		sanitized = sanitized[:64]
	}
	return sanitized
}

func deriveToolNameFromID(toolCallID string) string {
	parts := strings.Split(toolCallID, "-")
	if len(parts) > 2 {
		candidate := strings.Join(parts[:len(parts)-2], "-")
		if candidate != "" {
			return candidate
		}
	}
	return toolCallID
}

func reorderModelParts(parts [][]byte) [][]byte {
	var thinking, text, functionCall [][]byte
	for _, p := range parts {
		root := gjson.ParseBytes(p)
		switch {
		case root.Get("thought").Exists():
			thinking = append(thinking, p)
		case root.Get("functionCall").Exists():
			functionCall = append(functionCall, p)
		default:
			text = append(text, p)
		}
	}
	out := append(thinking, text...)
	out = append(out, functionCall...)
	return out
}

func stripTrailingEmptyAssistant(rawJSON []byte) []byte {
	messages := gjson.GetBytes(rawJSON, "messages")
	if !messages.IsArray() || len(messages.Array()) == 0 {
		return rawJSON
	}
	last := messages.Array()[len(messages.Array())-1]
	if last.Get("role").String() != "assistant" {
		return rawJSON
	}
	content := last.Get("content")
	empty := false
	if content.Type == gjson.String {
		empty = strings.TrimSpace(content.String()) == ""
	} else if content.IsArray() {
		empty = len(content.Array()) == 0
	} else if !content.Exists() {
		empty = true
	}
	if !empty {
		return rawJSON
	}

	kept := make([]string, 0, len(messages.Array())-1)
	for _, msg := range messages.Array()[:len(messages.Array())-1] {
		kept = append(kept, msg.Raw)
	}
	out, _ := sjson.SetRawBytes(rawJSON, "messages", []byte("["+strings.Join(kept, ",")+"]"))
	return out
}

// RequireCachedThinkingSignatures is exported for callers that want to pre-validate
// cached signatures before forwarding. If signature caching is disabled it is a no-op.
func RequireCachedThinkingSignatures(_ string, rawJSON []byte) error {
	// The in-memory cache in this project is best-effort; we do not gate requests on it.
	return nil
}

// CacheSignatureBestEffort stores a signature for replay (wraps internal/cache).
func CacheSignatureBestEffort(modelName, text, signature string) bool {
	return cache.CacheSignature(modelName, text, signature)
}
