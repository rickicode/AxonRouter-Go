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
