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

func joinSlices(items [][]byte) []byte {
	if len(items) == 0 {
		return nil
	}
	return joinRaw(items)
}

func jsonString(v gjson.Result) string {
	b, _ := json.Marshal(v.String())
	return string(b)
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

func setRawJSON(out []byte, path string, value gjson.Result, fallback []byte) []byte {
	if !value.Exists() {
		out, _ = jsonSetRaw(out, path, fallback)
		return out
	}
	raw := strings.TrimSpace(value.String())
	if value.Type == gjson.String && gjson.Valid(raw) {
		out, _ = jsonSetRaw(out, path, []byte(raw))
		return out
	}
	if value.Type == gjson.String {
		out, _ = jsonSetString(out, path, value.String())
		return out
	}
	out, _ = jsonSetRaw(out, path, []byte(value.Raw))
	return out
}

func jsonSetRaw(out []byte, path string, value []byte) ([]byte, error) {
	return sjson.SetRawBytes(out, path, value)
}

func jsonSetString(out []byte, path string, value string) ([]byte, error) {
	return sjson.SetBytes(out, path, value)
}

func openAITextMessage(text, role string) []byte {
	msg := map[string]interface{}{
		"role":    role,
		"content": text,
	}
	return mustMarshal(msg)
}
