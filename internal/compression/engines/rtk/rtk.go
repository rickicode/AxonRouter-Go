package rtk

import (
	"encoding/json"

	"github.com/rickicode/AxonRouter-Go/internal/compression"
)

func init() {
	compression.Register(defaultEngine)
}

var defaultEngine = &rtkEngine{}

// Engine returns the RTK engine instance.
func Engine() compression.Engine {
	return defaultEngine
}

// rtkEngine is the tool-output token saver.
type rtkEngine struct{}

// ID returns the engine identifier.
func (e *rtkEngine) ID() string { return "rtk" }

// Apply compresses tool outputs inside OpenAI / Claude / Responses request bodies.
// It is fail-open: on any error the original body is returned unchanged.
func (e *rtkEngine) Apply(body []byte, config compression.EngineConfig) ([]byte, compression.EngineStats, error) {
	original := string(body)
	beforeTokens := compression.EstimateTokens(original)

	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return body, compression.EngineStats{OriginalTokens: beforeTokens, CompressedTokens: beforeTokens}, nil
	}

	techniques := []string{}
	changed := false

	if msgs, ok := m["messages"].([]any); ok {
		for _, raw := range msgs {
			msg, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			c, t := compressMessageContent(msg)
			if c {
				changed = true
				techniques = appendUnique(techniques, t)
			}
		}
	}

	if input, ok := m["input"].([]any); ok {
		for _, raw := range input {
			item, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			c, t := compressInputItem(item)
			if c {
				changed = true
				techniques = appendUnique(techniques, t)
			}
		}
	}

	if !changed {
		return body, compression.EngineStats{OriginalTokens: beforeTokens, CompressedTokens: beforeTokens}, nil
	}

	outBody, err := json.Marshal(m)
	if err != nil {
		return body, compression.EngineStats{OriginalTokens: beforeTokens, CompressedTokens: beforeTokens}, nil
	}

	afterTokens := compression.EstimateTokens(string(outBody))
	savings := 0.0
	if beforeTokens > 0 {
		savings = (1.0 - float64(afterTokens)/float64(beforeTokens)) * 100
	}

	return outBody, compression.EngineStats{
		OriginalTokens:   beforeTokens,
		CompressedTokens: afterTokens,
		SavingsPercent:   savings,
		TechniquesUsed:   techniques,
	}, nil
}

func compressMessageContent(msg map[string]any) (bool, []string) {
	role, _ := msg["role"].(string)
	typ, _ := msg["type"].(string)

	if typ == "function_call_output" {
		return compressAnyText(msg, "output")
	}

	switch role {
	case "tool":
		return compressAnyText(msg, "content")
	case "user":
		content, ok := msg["content"].([]any)
		if !ok {
			return false, nil
		}
		changed := false
		var allTechs []string
		for _, partRaw := range content {
			part, ok := partRaw.(map[string]any)
			if !ok {
				continue
			}
			if part["type"] != "tool_result" {
				continue
			}
			c, t := compressAnyText(part, "content")
			if c {
				changed = true
				allTechs = appendUnique(allTechs, t)
			}
		}
		return changed, allTechs
	}
	return false, nil
}

func compressInputItem(item map[string]any) (bool, []string) {
	typ, _ := item["type"].(string)
	if typ != "function_call_output" {
		return false, nil
	}
	return compressAnyText(item, "output")
}

func compressAnyText(parent map[string]any, key string) (bool, []string) {
	v := parent[key]
	switch x := v.(type) {
	case string:
		out, techs := compressString(x)
		if out != x {
			parent[key] = out
			return true, techs
		}
	case []any:
		changed := false
		var allTechs []string
		for _, partRaw := range x {
			part, ok := partRaw.(map[string]any)
			if !ok {
				continue
			}
			text, _ := part["text"].(string)
			if text == "" {
				continue
			}
			out, techs := compressString(text)
			if out != text {
				part["text"] = out
				changed = true
				allTechs = appendUnique(allTechs, techs)
			}
		}
		return changed, allTechs
	}
	return false, nil
}

func compressString(text string) (string, []string) {
	if len(text) < minCompressSize {
		return text, nil
	}
	return compressToolOutput(text)
}

func compressToolOutput(text string) (string, []string) {
	name, fn := autoDetectFilter(text)
	if fn == nil {
		return text, nil
	}
	out := safeApply(fn, text)
	if out == text {
		return text, nil
	}
	return out, []string{name}
}

func appendUnique(slice, add []string) []string {
	set := make(map[string]bool, len(slice))
	for _, s := range slice {
		set[s] = true
	}
	for _, s := range add {
		if !set[s] {
			set[s] = true
			slice = append(slice, s)
		}
	}
	return slice
}
