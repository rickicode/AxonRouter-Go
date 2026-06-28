package claude

import (
	"encoding/json"

	"github.com/tidwall/gjson"
)

// convertOpenAIRequestToClaude converts an OpenAI Chat Completions request to Anthropic Messages format.
func convertOpenAIRequestToClaude(modelName string, body []byte, stream bool) []byte {
	root := gjson.ParseBytes(body)

	out := make(map[string]interface{})
	out["model"] = modelName

	if maxTokens := root.Get("max_tokens"); maxTokens.Exists() {
		out["max_tokens"] = maxTokens.Int()
	}
	if temp := root.Get("temperature"); temp.Exists() {
		out["temperature"] = temp.Float()
	}
	if topP := root.Get("top_p"); topP.Exists() {
		out["top_p"] = topP.Float()
	}
	out["stream"] = stream

	// Stop sequences
	if stop := root.Get("stop"); stop.Exists() && stop.IsArray() {
		var stops []string
		stop.ForEach(func(_, v gjson.Result) bool {
			stops = append(stops, v.String())
			return true
		})
		if len(stops) > 0 {
			out["stop_sequences"] = stops
		}
	}

	// System message extraction
	var systemParts []string
	if sys := root.Get("messages"); sys.Exists() && sys.IsArray() {
		sys.ForEach(func(_, msg gjson.Result) bool {
			if msg.Get("role").String() == "system" {
				if c := msg.Get("content"); c.Exists() {
					if c.Type == gjson.String {
						systemParts = append(systemParts, c.String())
					} else if c.IsArray() {
						c.ForEach(func(_, part gjson.Result) bool {
							if part.Get("type").String() == "text" {
								systemParts = append(systemParts, part.Get("text").String())
							}
							return true
						})
					}
				}
			}
			return true
		})
	}
	if len(systemParts) > 0 {
		out["system"] = joinStrings(systemParts)
	}

	// Messages: filter out system, convert content blocks
	var messages []map[string]interface{}
	if msgs := root.Get("messages"); msgs.Exists() && msgs.IsArray() {
		msgs.ForEach(func(_, msg gjson.Result) bool {
			role := msg.Get("role").String()
			if role == "system" {
				return true // skip, already extracted
			}

			claudeMsg := map[string]interface{}{
				"role": role,
			}

			if content := msg.Get("content"); content.Exists() {
				if content.Type == gjson.String {
					claudeMsg["content"] = content.String()
				} else if content.IsArray() {
					var parts []map[string]interface{}
					content.ForEach(func(_, part gjson.Result) bool {
						pType := part.Get("type").String()
						switch pType {
						case "text":
							parts = append(parts, map[string]interface{}{
								"type": "text",
								"text": part.Get("text").String(),
							})
						case "image_url":
							if url := part.Get("image_url.url"); url.Exists() {
								parts = append(parts, map[string]interface{}{
									"type": "image",
									"source": map[string]interface{}{
										"type": "url",
										"url":  url.String(),
									},
								})
							}
						case "tool_use":
							toolUse := map[string]interface{}{
								"type":  "tool_use",
								"id":    part.Get("id").String(),
								"name":  part.Get("name").String(),
								"input": json.RawMessage(part.Get("input").Raw),
							}
							parts = append(parts, toolUse)
						case "tool_result":
							toolResult := map[string]interface{}{
								"type":        "tool_result",
								"tool_use_id": part.Get("tool_use_id").String(),
							}
							if c := part.Get("content"); c.Exists() {
								toolResult["content"] = c.String()
							}
							parts = append(parts, toolResult)
						}
						return true
					})
					if len(parts) > 0 {
						claudeMsg["content"] = parts
					}
				}
			}

			// Tool calls (assistant message)
			if toolCalls := msg.Get("tool_calls"); toolCalls.Exists() && toolCalls.IsArray() {
				var contentParts []map[string]interface{}
				toolCalls.ForEach(func(_, tc gjson.Result) bool {
					contentParts = append(contentParts, map[string]interface{}{
						"type":  "tool_use",
						"id":    tc.Get("id").String(),
						"name":  tc.Get("function.name").String(),
						"input": json.RawMessage(tc.Get("function.arguments").String()),
					})
					return true
				})
				if existing, ok := claudeMsg["content"].([]map[string]interface{}); ok {
					claudeMsg["content"] = append(existing, contentParts...)
				} else {
					claudeMsg["content"] = contentParts
				}
			}

			messages = append(messages, claudeMsg)
			return true
		})
	}

	out["messages"] = messages

	// Tools
	if tools := root.Get("tools"); tools.Exists() && tools.IsArray() {
		var claudeTools []map[string]interface{}
		tools.ForEach(func(_, tool gjson.Result) bool {
			claudeTool := map[string]interface{}{
				"name": tool.Get("function.name").String(),
			}
			if desc := tool.Get("function.description"); desc.Exists() {
				claudeTool["description"] = desc.String()
			}
			if params := tool.Get("function.parameters"); params.Exists() {
				claudeTool["input_schema"] = json.RawMessage(params.Raw)
			}
			claudeTools = append(claudeTools, claudeTool)
			return true
		})
		if len(claudeTools) > 0 {
			out["tools"] = claudeTools
		}
	}

	result, _ := json.Marshal(out)
	return result
}

func joinStrings(parts []string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += "\n\n"
		}
		result += p
	}
	return result
}
