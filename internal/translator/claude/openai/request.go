package openai

import (
	"encoding/json"
	"strings"

	"github.com/tidwall/gjson"
)

// ConvertClaudeRequestToOpenAI converts an Anthropic Messages request to OpenAI Chat Completions format.
// It builds a single map[string]any and marshals once, avoiding ~12 sjson round-trips.
func ConvertClaudeRequestToOpenAI(modelName string, body []byte, stream bool) []byte {
	root := gjson.ParseBytes(body)

	out := map[string]any{
		"model":  modelName,
		"stream": stream,
	}
	messages := []any{}

	if maxTokens := root.Get("max_tokens"); maxTokens.Exists() {
		out["max_tokens"] = maxTokens.Int()
	}
	if temp := root.Get("temperature"); temp.Exists() {
		out["temperature"] = temp.Float()
	}
	if topP := root.Get("top_p"); topP.Exists() {
		out["top_p"] = topP.Float()
	}

	// Stop sequences
	if stop := root.Get("stop_sequences"); stop.Exists() && stop.IsArray() {
		var stops []string
		stop.ForEach(func(_, v gjson.Result) bool {
			stops = append(stops, v.String())
			return true
		})
		if len(stops) == 1 {
			out["stop"] = stops[0]
		} else if len(stops) > 1 {
			out["stop"] = stops
		}
	}

	// System message (string or array of {text,...})
	if sys := root.Get("system"); sys.Exists() {
		systemContent := ""
		if sys.Type == gjson.String {
			systemContent = sys.String()
		} else if sys.IsArray() {
			var parts []string
			sys.ForEach(func(_, part gjson.Result) bool {
				if t := part.Get("text"); t.Exists() {
					parts = append(parts, t.String())
				}
				return true
			})
			systemContent = strings.Join(parts, "\n")
		}
		if systemContent != "" {
			messages = append(messages, map[string]any{
				"role":    "system",
				"content": systemContent,
			})
		}
	}

	// tool_choice
	if tc := root.Get("tool_choice"); tc.Exists() {
		switch tc.Type {
		case gjson.String:
			switch tc.String() {
			case "auto":
				out["tool_choice"] = "auto"
			case "any":
				out["tool_choice"] = "required"
			case "none":
				out["tool_choice"] = "none"
			}
		case gjson.JSON:
			if tc.Get("type").String() == "tool" {
				out["tool_choice"] = map[string]any{
					"type":     "function",
					"function": map[string]any{"name": tc.Get("name").String()},
				}
			}
		}
	}

	// thinking (request-level) → reasoning_effort
	if thinking := root.Get("thinking"); thinking.Exists() && thinking.Type == gjson.JSON {
		switch thinking.Get("type").String() {
		case "enabled":
			out["reasoning_effort"] = "high"
		case "disabled":
			out["reasoning_effort"] = "none"
		case "adaptive", "auto":
			out["reasoning_effort"] = "medium"
		}
	}

	// Messages
	if msgs := root.Get("messages"); msgs.Exists() && msgs.IsArray() {
		msgs.ForEach(func(_, msg gjson.Result) bool {
			role := msg.Get("role").String()
			content := msg.Get("content")
			if !content.Exists() {
				return true
			}
			if content.Type == gjson.String {
				messages = append(messages, map[string]any{
					"role":    role,
					"content": content.String(),
				})
				return true
			}

			var contentParts []map[string]any
			var toolCalls []map[string]any
			type toolResult struct {
				id      string
				content any
			}
			var toolResults []toolResult

			content.ForEach(func(_, part gjson.Result) bool {
				switch part.Get("type").String() {
				case "text":
					contentParts = append(contentParts, map[string]any{
						"type": "text",
						"text": part.Get("text").String(),
					})
				case "image":
					if source := part.Get("source"); source.Exists() {
						url := ""
						if source.Get("type").String() == "base64" {
							url = "data:" + source.Get("media_type").String() + ";base64," + source.Get("data").String()
						} else {
							url = source.Get("url").String()
						}
						contentParts = append(contentParts, map[string]any{
							"type":      "image_url",
							"image_url": map[string]any{"url": url},
						})
					}
				case "tool_use":
					toolCalls = append(toolCalls, map[string]any{
						"id":   part.Get("id").String(),
						"type": "function",
						"function": map[string]any{
							"name":      part.Get("name").String(),
							"arguments": json.RawMessage(part.Get("input").Raw),
						},
					})
				case "tool_result":
					var c any
					if cRaw := part.Get("content"); cRaw.Exists() {
						if cRaw.Type == gjson.String {
							c = cRaw.String()
						} else {
							c = json.RawMessage(cRaw.Raw)
						}
					} else {
						c = ""
					}
					toolResults = append(toolResults, toolResult{id: part.Get("tool_use_id").String(), content: c})
				case "thinking", "redacted_thinking":
					// dropped in request translation (never mapped to OpenAI)
				}
				return true
			})

			// assistant tool_use → tool_calls (content may be null)
			if role == "assistant" && len(toolCalls) > 0 {
				m := map[string]any{
					"role":      role,
					"content":   nil,
					"tool_calls": toolCalls,
				}
				if len(contentParts) > 0 {
					m["content"] = contentParts
				}
				messages = append(messages, m)
				return true
			}

			// user message with tool_result parts → emit tool messages first (correct ordering), then text
			if role == "user" && len(toolResults) > 0 {
				for _, tr := range toolResults {
					messages = append(messages, map[string]any{
						"role":         "tool",
						"tool_call_id": tr.id,
						"content":      tr.content,
					})
				}
				if len(contentParts) > 0 {
					messages = append(messages, map[string]any{
						"role":    "user",
						"content": contentParts,
					})
				}
				return true
			}

			// default: plain content (array of parts, or empty string)
			m := map[string]any{"role": role}
			if len(contentParts) > 0 {
				m["content"] = contentParts
			} else {
				m["content"] = ""
			}
			messages = append(messages, m)
			return true
		})
	}

	out["messages"] = messages

	// Tools
	if tools := root.Get("tools"); tools.Exists() && tools.IsArray() {
		var oaiTools []map[string]any
		tools.ForEach(func(_, tool gjson.Result) bool {
			fn := map[string]any{"name": tool.Get("name").String()}
			if desc := tool.Get("description"); desc.Exists() {
				fn["description"] = desc.String()
			}
			if schema := tool.Get("input_schema"); schema.Exists() {
				fn["parameters"] = json.RawMessage(schema.Raw)
			}
			oaiTools = append(oaiTools, map[string]any{"type": "function", "function": fn})
			return true
		})
		if len(oaiTools) > 0 {
			out["tools"] = oaiTools
		}
	}

	b, _ := json.Marshal(out)
	return b
}
