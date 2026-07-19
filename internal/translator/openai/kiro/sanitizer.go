package kiro

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// ToolNameMaxLength is the maximum tool name length accepted by Kiro.
const ToolNameMaxLength = 64

var disallowedSchemaKeys = map[string]struct{}{
	"additionalProperties": {},
	"anyOf":                {},
	"oneOf":                {},
	"allOf":                {},
	"not":                  {},
	"$schema":              {},
	"$id":                  {},
	"$ref":                 {},
	"$defs":                {},
	"definitions":          {},
	"if":                   {},
	"then":                 {},
	"else":                 {},
	"unevaluatedProperties": {},
	"unevaluatedItems":     {},
	"contentEncoding":      {},
	"contentMediaType":     {},
}

// SanitizeTools sanitizes every tool schema and normalizes long tool names.
// It returns the sanitized tools and a name map from normalized -> original.
func SanitizeTools(tools []any) ([]any, map[string]string, error) {
	nameMap := make(map[string]string)
	out := make([]any, 0, len(tools))
	for _, t := range tools {
		tool, ok := t.(map[string]any)
		if !ok {
			out = append(out, t)
			continue
		}
		sanitized := cloneMap(tool)
		if fn, ok := sanitized["function"].(map[string]any); ok {
			if name, ok := fn["name"].(string); ok {
				normalized := NormalizeToolName(name, nameMap)
				fn["name"] = normalized
			}
			if params, ok := fn["parameters"].(map[string]any); ok {
				fn["parameters"] = sanitizeSchema(params)
			}
		}
		out = append(out, sanitized)
	}
	return out, nameMap, nil
}

// sanitizeSchema recursively removes unsupported JSON Schema keywords.
func sanitizeSchema(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			if _, disallowed := disallowedSchemaKeys[k]; disallowed {
				continue
			}
			if k == "required" {
				if arr, ok := val.([]any); ok && len(arr) == 0 {
					continue
				}
			}
			out[k] = sanitizeSchema(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = sanitizeSchema(item)
		}
		return out
	default:
		return x
	}
}

// NormalizeToolName truncates names that exceed ToolNameMaxLength using a deterministic hash suffix.
// It populates nameMap with normalized -> original mapping.
func NormalizeToolName(name string, nameMap map[string]string) string {
	if len(name) <= ToolNameMaxLength {
		nameMap[name] = name
		return name
	}
	sum := sha256.Sum256([]byte(name))
	hash := hex.EncodeToString(sum[:])[:8]
	prefixLen := ToolNameMaxLength - len(hash) - 1
	if prefixLen < 8 {
		prefixLen = 8
	}
	normalized := fmt.Sprintf("%s_%s", name[:prefixLen], hash)
	nameMap[normalized] = name
	return normalized
}

func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = deepClone(v)
	}
	return out
}

func deepClone(v any) any {
	switch x := v.(type) {
	case map[string]any:
		return cloneMap(x)
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = deepClone(item)
		}
		return out
	default:
		return x
	}
}
