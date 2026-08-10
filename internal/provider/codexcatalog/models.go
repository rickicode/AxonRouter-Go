// Package codexcatalog builds dynamic Codex client model catalogs from
// gateway-side model definitions while leaving the static models.json catalog
// unchanged.
package codexcatalog

import (
	"strings"
	"sync"
)

// Capabilities mirrors the capability flags used by the Codex client when
// deciding how a model can be invoked.
type Capabilities struct {
	Thinking bool
	Vision   bool
	Search   bool
	Agentic  bool
}

// Model is a gateway-facing representation of a model entry used to build a
// Codex client catalog payload. It intentionally mirrors the shape used by
// CLIProxyAPI internal/client/codex/models so the two can converge over time.
type Model struct {
	ID              string
	DisplayName     string
	Description     string
	OwnedBy         string
	ContextLength   int
	MaxOutputTokens int
	Capabilities    Capabilities
	InputModalities []string
	ThinkingLevels  []string
}

// Entry is one constructed Codex client model entry in the final response.
// The map shape is defined by Codex client expectations rather than our typed
// structs, so the builder works with map[string]any.
type Entry = map[string]any

// providerCatalog stores a set of Codex client model templates keyed by slug.
// It is used to enrich entries for known models and to provide a default
// template for unknown upstream models.
type providerCatalog struct {
	mu              sync.RWMutex
	templates       map[string]Entry
	defaultTemplate Entry
	loaded          bool
}

var (
	globalCatalog          providerCatalog
	allowedReasoningLevels = map[string]struct{}{
		"none":    {},
		"minimal": {},
		"low":     {},
		"medium":  {},
		"high":    {},
		"xhigh":   {},
		"max":     {},
		"ultra":   {},
	}
)

// String returns a cleaned string value from a model map, or the empty string.
func stringValue(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok {
		return ""
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

// intValue returns a coerced integer value from a model map.
func intValue(m map[string]any, key string) int {
	if m == nil {
		return 0
	}
	switch v := m[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

// cloneMap returns a deep copy of a map[string]any value.
func cloneMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	cloned := make(map[string]any, len(m))
	for k, v := range m {
		cloned[k] = cloneValue(v)
	}
	return cloned
}

// cloneValue returns a deep copy of a value that may contain nested maps/slices.
func cloneValue(v any) any {
	switch typed := v.(type) {
	case map[string]any:
		return cloneMap(typed)
	case []any:
		out := make([]any, len(typed))
		for i, e := range typed {
			out[i] = cloneValue(e)
		}
		return out
	case []string:
		return append([]string(nil), typed...)
	default:
		return v
	}
}
