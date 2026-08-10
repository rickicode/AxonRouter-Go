package interactions

import (
	"encoding/json"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func firstExisting(values ...gjson.Result) gjson.Result {
	for _, v := range values {
		if v.Exists() {
			return v
		}
	}
	return gjson.Result{}
}

func mustMarshal(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}

func joinRaw(items [][]byte) []byte {
	b := []byte("[")
	for i, item := range items {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, item...)
	}
	b = append(b, ']')
	return b
}

func jsonStringValue(v gjson.Result, fallback string) string {
	if !v.Exists() {
		return fallback
	}
	if v.Type == gjson.String {
		return v.String()
	}
	return v.Raw
}

func parseDataURL(value string) (string, string, bool) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "data:") {
		return "", "", false
	}
	header, data, ok := strings.Cut(strings.TrimPrefix(value, "data:"), ",")
	if !ok {
		return "", "", false
	}
	mimeType, _, _ := strings.Cut(header, ";")
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	return mimeType, data, true
}

func setRawJSON(out []byte, path string, value gjson.Result, fallback []byte) []byte {
	if !value.Exists() {
		out, _ = sjson.SetRawBytes(out, path, fallback)
		return out
	}
	raw := strings.TrimSpace(value.String())
	if value.Type == gjson.String && gjson.Valid(raw) {
		out, _ = sjson.SetRawBytes(out, path, []byte(raw))
		return out
	}
	if value.Type == gjson.String {
		out, _ = sjson.SetBytes(out, path, value.String())
		return out
	}
	out, _ = sjson.SetRawBytes(out, path, []byte(value.Raw))
	return out
}

func responsesTextItem(text string) []byte {
	item := []byte(`{"type":"message","role":"assistant","content":[{"type":"output_text","text":""}]}`)
	item, _ = sjson.SetBytes(item, "content.0.text", text)
	return item
}

func responsesReasoningItem(text string) []byte {
	item := []byte(`{"type":"reasoning","summary":[{"type":"summary_text","text":""}]}`)
	item, _ = sjson.SetBytes(item, "summary.0.text", text)
	return item
}

func responsesFunctionCallItem(callID, name, arguments string) []byte {
	item := []byte(`{"type":"function_call","call_id":"","name":"","arguments":""}`)
	item, _ = sjson.SetBytes(item, "call_id", callID)
	item, _ = sjson.SetBytes(item, "name", name)
	item, _ = sjson.SetBytes(item, "arguments", arguments)
	return item
}
