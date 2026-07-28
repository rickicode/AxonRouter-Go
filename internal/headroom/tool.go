package headroom

import (
	"bytes"
	"encoding/json"
)

// MinToolContentBytes is the smallest tool-output payload we will try to
// compress. Very small strings rarely save tokens and the overhead is not worth
// the latency.
const MinToolContentBytes = 256

// CompressibleToolText returns the raw byte payload that should be compressed,
// or nil if this block should not be compressed. It accepts the content value
// of a tool_result block or a "tool" role message, which may be a JSON string,
// an array of content parts, or null.
func CompressibleToolText(content json.RawMessage) []byte {
	if len(content) == 0 || bytes.Equal(content, []byte("null")) {
		return nil
	}
	if content[0] == '"' {
		var s string
		if err := json.Unmarshal(content, &s); err != nil {
			return nil
		}
		return []byte(s)
	}
	if content[0] == '[' {
		var parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(content, &parts); err != nil {
			return nil
		}
		var texts []byte
		for _, p := range parts {
			if p.Type == "text" && p.Text != "" {
				if len(texts) > 0 {
					texts = append(texts, '\n')
				}
				texts = append(texts, []byte(p.Text)...)
			}
		}
		if len(texts) == 0 {
			return nil
		}
		return texts
	}
	if content[0] == '{' {
		var obj struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(content, &obj); err != nil {
			return nil
		}
		if obj.Type == "text" && obj.Text != "" {
			return []byte(obj.Text)
		}
	}
	return content
}

// SetToolText rewrites the original content value to hold a compressed string
// or array-of-text-parts. If the original was a string it stays a string; if it
// was an array or object it rebuilds a single text part to keep the schema.
func SetToolText(content json.RawMessage, compressed []byte) json.RawMessage {
	if len(content) == 0 || bytes.Equal(content, []byte("null")) {
		return content
	}
	if content[0] == '"' {
		b, _ := json.Marshal(string(compressed))
		return b
	}
	out := map[string]interface{}{
		"type": "text",
		"text": string(compressed),
	}
	b, _ := json.Marshal(out)
	return b
}

// IsToolBlock returns true if the content object looks like a tool-related block.
func IsToolBlock(content json.RawMessage) bool {
	if len(content) == 0 {
		return false
	}
	if content[0] != '{' {
		return false
	}
	var obj struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(content, &obj); err != nil {
		return false
	}
	return obj.Type == "tool_result" || obj.Type == "function_call_output" || obj.Type == "tool"
}
