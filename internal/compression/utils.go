package compression

import (
	"encoding/json"
	"math"
	"strings"
)

// EstimateTokens returns a rough token count using the 4-char heuristic.
func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	return int(math.Ceil(float64(len(text)) / 4.0))
}

// HasTools detects whether a request body contains a non-empty tools array.
func HasTools(body []byte) bool {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return false
	}
	t, ok := m["tools"]
	if !ok || t == nil {
		return false
	}
	arr, ok := t.([]any)
	return ok && len(arr) > 0
}

// HasCacheControl detects whether a request body contains Anthropic-style
// cache_control markers. Compression should be skipped for such requests to
// avoid breaking provider-side prompt-cache prefixes.
func HasCacheControl(body []byte) bool {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return false
	}
	return valueHasCacheControl(v)
}

func valueHasCacheControl(v any) bool {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			if k == "cache_control" {
				return true
			}
			if valueHasCacheControl(val) {
				return true
			}
		}
	case []any:
		for _, val := range x {
			if valueHasCacheControl(val) {
				return true
			}
		}
	}
	return false
}

// CleanSpaces collapses multiple spaces/newlines and trims.
func CleanSpaces(s string) string {
	var b strings.Builder
	var lastSpace bool
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if !lastSpace {
				b.WriteByte(' ')
				lastSpace = true
			}
			continue
		}
		b.WriteRune(r)
		lastSpace = false
	}
	return strings.TrimSpace(b.String())
}
