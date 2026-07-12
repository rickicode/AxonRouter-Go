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

// System message (string or array of {text,...}).
	// We preserve array form so cache_control blocks can be dropped while still
	// keeping multi-part system prompts intact; pure strings stay strings.
	if sys := root.Get("system"); sys.Exists() {
		if sys.Type == gjson.String {
			messages = append(messages, map[string]any{
				"role":    "system",
				"content": sys.String(),
			})
		} else if sys.IsArray() {
			var sysParts []map[string]any
			sys.ForEach(func(_, part gjson.Result) bool {
				if t := part.Get("text"); t.Exists() {
					sysParts = append(sysParts, map[string]any{
						"type": "text",
						"text": t.String(),
					})
				}
				return true
			})
			if len(sysParts) > 0 {
				messages = append(messages, map[string]any{
					"role":    "system",
					"content": sysParts,
				})
			}
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
					args := part.Get("input").String()
					if args == "" {
						args = "{}"
					}
					toolCalls = append(toolCalls, map[string]any{
						"id": part.Get("id").String(),
						"type": "function",
						"function": map[string]any{
							"name":      part.Get("name").String(),
							"arguments": args,
						},
					})
				case "tool_result":
					c := normalizeToolResultContent(part.Get("content"))
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

// normalizeToolResultContent converts an Anthropic tool_result.content value
// (which can be a string or an array of content blocks) into a plain string for
// OpenAI's tool role message. Upstreams such as Cloudflare Workers AI reject
// non-string tool message content with "Tool use input must be a string or object".
func normalizeToolResultContent(c gjson.Result) string {
	if !c.Exists() || c.Type == gjson.Null {
		return ""
	}
	if c.Type == gjson.String {
		return c.String()
	}
	if c.IsArray() {
		var parts []string
		hasNonText := false
		c.ForEach(func(_, item gjson.Result) bool {
			switch {
			case item.Type == gjson.String:
				parts = append(parts, item.String())
			case item.Get("type").String() == "text":
				parts = append(parts, item.Get("text").String())
			default:
				hasNonText = true
			}
			return true
		})
		if len(parts) > 0 && !hasNonText {
			return strings.Join(parts, "\n\n")
		}
	}
	if c.IsObject() {
		if text := c.Get("text"); text.Exists() && text.Type == gjson.String {
			return text.String()
		}
	}
	b, _ := json.Marshal(c.Value())
	return string(b)
}
