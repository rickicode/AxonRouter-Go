package normalize

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func ValidateClientSSE(frame []byte) ([]byte, bool) {
	trimmed := bytes.TrimSpace(frame)
	if len(trimmed) == 0 || bytes.HasPrefix(trimmed, []byte(":")) || bytes.Equal(trimmed, []byte("data: [DONE]")) {
		return frame, true
	}
	if !bytes.HasPrefix(trimmed, []byte("data:")) {
		return frame, true
	}
	payload := bytes.TrimSpace(trimmed[len("data:"):])
	if bytes.Equal(payload, []byte("[DONE]")) {
		return frame, true
	}
	if !gjson.ValidBytes(payload) {
		return nil, false
	}
	return append(append([]byte("data: "), normalizeSSEToolCalls(payload)...), '\n', '\n'), true
}

func normalizeSSEToolCalls(payload []byte) []byte {
	root := gjson.ParseBytes(payload)
	out := payload
	if calls := root.Get("choices.0.delta.tool_calls"); calls.Exists() && calls.IsArray() {
		calls.ForEach(func(i, call gjson.Result) bool {
			base := fmt.Sprintf("choices.0.delta.tool_calls.%d", i.Int())
			id := sanitizeToolID(call.Get("id").String())
			if id != "" {
				out, _ = sjson.SetBytes(out, base+".id", id)
			}
			if call.Get("type").String() == "" && call.Get("id").Exists() {
				out, _ = sjson.SetBytes(out, base+".type", "function")
			}
			return true
		})
	}
	if block := root.Get("content_block"); block.Exists() && block.IsObject() {
		if id := sanitizeToolID(block.Get("id").String()); id != "" {
			out, _ = sjson.SetBytes(out, "content_block.id", id)
		}
	}
	return out
}

func ValidateClientJSON(body []byte) ([]byte, bool) {
	if !json.Valid(body) {
		return nil, false
	}
	root := gjson.ParseBytes(body)
	out := body
	if calls := root.Get("choices.0.message.tool_calls"); calls.Exists() && calls.IsArray() {
		calls.ForEach(func(i, call gjson.Result) bool {
			base := fmt.Sprintf("choices.0.message.tool_calls.%d", i.Int())
			if id := sanitizeToolID(call.Get("id").String()); id != "" {
				out, _ = sjson.SetBytes(out, base+".id", id)
			}
			if call.Get("type").String() == "" && call.Get("id").Exists() {
				out, _ = sjson.SetBytes(out, base+".type", "function")
			}
			return true
		})
	}
	return out, true
}

func HasProviderNativeToolShape(body []byte) bool {
	if !json.Valid(body) {
		return true
	}
	text := strings.TrimSpace(string(body))
	return strings.Contains(text, `"functionCall"`) || strings.Contains(text, `"functionResponse"`)
}
