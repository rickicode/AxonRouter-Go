package headroom

import (
	"context"
	"encoding/json"
)

// ApplyToRequestBody compresses tool_result / tool output content inside an
// Anthropic Messages or OpenAI Chat Completions request. It is fail-open.
func ApplyToRequestBody(ctx context.Context, client *Client, body []byte) []byte {
	if client == nil {
		return body
	}
	cfg := client.Config()
	if !cfg.Enabled || client.Status() != "running" {
		return body
	}

	return compressViaMap(ctx, client, body)
}

// ApplyToMessageContent compresses a single outside-json text block if it
// matches a known tool output kind. Returns the possibly-compressed text.
func ApplyToMessageContent(ctx context.Context, client *Client, text string) string {
	if client == nil || text == "" {
		return text
	}
	cfg := client.Config()
	if !cfg.Enabled || client.Status() != "running" {
		return text
	}
	kind := DetectPayloadKind(text)
	if kind == KindGeneric {
		return text
	}
	compressed, err := client.Compress(ctx, kind, text)
	if err != nil || len(compressed) >= len(text) {
		return text
	}
	return compressed
}

func compressViaMap(ctx context.Context, client *Client, body []byte) []byte {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return body
	}
	messages, ok := m["messages"].([]any)
	if !ok {
		return body
	}

	modified := false
	for _, raw := range messages {
		msg, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		content, ok := msg["content"].([]any)
		if !ok {
			continue
		}
		for _, rawPart := range content {
			part, ok := rawPart.(map[string]any)
			if !ok {
				continue
			}
			typ, _ := part["type"].(string)
			switch typ {
			case "tool_result":
				if original := toolResultTextMap(part); original != "" {
					compressed, err := client.Compress(ctx, KindToolResult, original)
					if err == nil && len(compressed) < len(original) {
						setToolResultTextMap(part, compressed)
						modified = true
					}
				}
			case "text":
				text, _ := part["text"].(string)
				if text == "" {
					continue
				}
				kind := DetectPayloadKind(text)
				if kind == KindGeneric {
					continue
				}
				compressed, err := client.Compress(ctx, kind, text)
				if err == nil && len(compressed) < len(text) {
					part["text"] = compressed
					modified = true
				}
			}
		}
	}

	if !modified {
		return body
	}
	out, err := json.Marshal(m)
	if err != nil {
		return body
	}
	return out
}

func toolResultTextMap(part map[string]any) string {
	content := part["content"]
	switch v := content.(type) {
	case string:
		return v
	case []any:
		for _, raw := range v {
			if m, ok := raw.(map[string]any); ok {
				if m["type"] == "text" {
					if s, ok := m["text"].(string); ok {
						return s
					}
				}
			}
		}
	}
	return ""
}

func setToolResultTextMap(part map[string]any, text string) {
	content := part["content"]
	switch v := content.(type) {
	case string:
		part["content"] = text
	case []any:
		for _, raw := range v {
			if m, ok := raw.(map[string]any); ok && m["type"] == "text" {
				m["text"] = text
				return
			}
		}
	}
}
