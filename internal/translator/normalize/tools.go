package normalize

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const toolIDPattern = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-"

func sanitizeToolID(id string) string {
	var b strings.Builder
	for _, r := range id {
		if strings.ContainsRune(toolIDPattern, r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// EnsureToolCallIDs normalizes tool identifiers and arguments in OpenAI-style
// messages and Responses input items. Existing valid IDs are preserved.
func EnsureToolCallIDs(body []byte) []byte {
	root := gjson.ParseBytes(body)
	if root.Get("input").Exists() && !root.Get("messages").Exists() {
		body = ensureResponsesCallIDs(body)
	}
	messages := gjson.GetBytes(body, "messages")
	if !messages.Exists() || !messages.IsArray() {
		return body
	}
	messages.ForEach(func(mi, msg gjson.Result) bool {
		calls := msg.Get("tool_calls")
		if calls.Exists() && calls.IsArray() {
			calls.ForEach(func(ci, call gjson.Result) bool {
				id := sanitizeToolID(strings.TrimSpace(call.Get("id").String()))
				if id == "" {
					id = fmt.Sprintf("call_msg%d_tc%d", mi.Int(), ci.Int())
				}
				base := fmt.Sprintf("messages.%d.tool_calls.%d", mi.Int(), ci.Int())
				body, _ = sjson.SetBytes(body, base+".id", id)
				if call.Get("type").String() == "" {
					body, _ = sjson.SetBytes(body, base+".type", "function")
				}
				args := call.Get("function.arguments")
				if args.Exists() && args.Type != gjson.String {
					body, _ = sjson.SetBytes(body, base+".function.arguments", args.Raw)
				}
				return true
			})
		}
		content := msg.Get("content")
		if content.Exists() && content.IsArray() {
			content.ForEach(func(ci, part gjson.Result) bool {
				base := fmt.Sprintf("messages.%d.content.%d", mi.Int(), ci.Int())
				switch part.Get("type").String() {
				case "tool_use":
					id := sanitizeToolID(part.Get("id").String())
					if id == "" {
						id = fmt.Sprintf("call_msg%d_tc%d", mi.Int(), ci.Int())
					}
					body, _ = sjson.SetBytes(body, base+".id", id)
				case "tool_result":
					id := sanitizeToolID(part.Get("tool_use_id").String())
					if id != "" {
						body, _ = sjson.SetBytes(body, base+".tool_use_id", id)
					}
				}
				return true
			})
		}
		if msg.Get("role").String() == "tool" {
			id := sanitizeToolID(msg.Get("tool_call_id").String())
			if id != "" {
				body, _ = sjson.SetBytes(body, fmt.Sprintf("messages.%d.tool_call_id", mi.Int()), id)
			}
		}
		return true
	})
	return body
}

func ensureResponsesCallIDs(body []byte) []byte {
	input := gjson.GetBytes(body, "input")
	if !input.IsArray() {
		return body
	}
	input.ForEach(func(i, item gjson.Result) bool {
		if item.Get("type").String() == "function_call" {
			id := sanitizeToolID(item.Get("call_id").String())
			if id == "" {
				id = sanitizeToolID(item.Get("id").String())
			}
			if id == "" {
				id = fmt.Sprintf("call_%d", i.Int())
			}
			body, _ = sjson.SetBytes(body, fmt.Sprintf("input.%d.call_id", i.Int()), id)
		}
		return true
	})
	return body
}

// FixMissingToolResponses repairs incomplete OpenAI and Claude histories.
// It matches 9router: only a non-terminal assistant tool turn with a following
// message is repaired. A terminal tool call remains untouched so the client
// can execute it instead of receiving a fabricated result.
func FixMissingToolResponses(body []byte) []byte {
	var root map[string]any
	if json.Unmarshal(body, &root) != nil {
		return body
	}
	messages, ok := root["messages"].([]any)
	if !ok {
		return body
	}

	out := make([]any, 0, len(messages))
	for i, raw := range messages {
		out = append(out, raw)
		msg, ok := raw.(map[string]any)
		if !ok || msg["role"] != "assistant" {
			continue
		}
		ids := toolCallIDs(msg)
		if len(ids) == 0 {
			continue
		}
		if i+1 >= len(messages) {
			continue
		}
		if hasAnyToolResult(messages[i+1], ids) {
			continue
		}
		// Match 9router behavior for a non-terminal assistant tool turn.
		for _, id := range ids {
			out = append(out, map[string]any{"role": "tool", "tool_call_id": id, "content": ""})
		}
	}
	root["messages"] = out
	encoded, err := json.Marshal(root)
	if err != nil {
		return body
	}
	return encoded
}

func toolCallIDs(msg map[string]any) []string {
	var ids []string
	if calls, ok := msg["tool_calls"].([]any); ok {
		for _, raw := range calls {
			if call, ok := raw.(map[string]any); ok {
				if id, ok := call["id"].(string); ok && id != "" {
					ids = append(ids, id)
				}
			}
		}
	}
	if content, ok := msg["content"].([]any); ok {
		for _, raw := range content {
			if block, ok := raw.(map[string]any); ok && block["type"] == "tool_use" {
				if id, ok := block["id"].(string); ok && id != "" {
					ids = append(ids, id)
				}
			}
		}
	}
	return ids
}

func hasAnyToolResult(raw any, ids []string) bool {
	msg, ok := raw.(map[string]any)
	if !ok {
		return false
	}
	if id, ok := msg["tool_call_id"].(string); ok {
		for _, want := range ids {
			if id == want {
				return true
			}
		}
	}
	if content, ok := msg["content"].([]any); ok {
		for _, rawBlock := range content {
			block, ok := rawBlock.(map[string]any)
			if !ok || block["type"] != "tool_result" {
				continue
			}
			id, _ := block["tool_use_id"].(string)
			for _, want := range ids {
				if id == want {
					return true
				}
			}
		}
	}
	return false
}
