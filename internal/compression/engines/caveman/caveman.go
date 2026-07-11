package caveman

import (
	"encoding/json"

	"github.com/rickicode/AxonRouter-Go/internal/compression"
)

func init() {
	compression.Register(&Engine{})
}

// Engine is the rule-based prose compressor.
type Engine struct{}

// ID returns the engine identifier.
func (e *Engine) ID() string { return "caveman" }

// Apply compresses message content in an OpenAI-style request body.
func (e *Engine) Apply(body []byte, config compression.EngineConfig) ([]byte, compression.EngineStats, error) {
	original := string(body)
	beforeTokens := compression.EstimateTokens(original)

	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return body, compression.EngineStats{OriginalTokens: beforeTokens, CompressedTokens: beforeTokens}, nil
	}

	messages, _ := m["messages"].([]any)
	if messages == nil {
		return body, compression.EngineStats{OriginalTokens: beforeTokens, CompressedTokens: beforeTokens}, nil
	}

	techniques := []string{}
	changed := false

	for _, raw := range messages {
		msg, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		content := msg["content"]
		switch v := content.(type) {
		case string:
			out, t := compressText(v)
			if out != v {
				changed = true
				techniques = appendUnique(techniques, t)
			}
			msg["content"] = out
		case []any:
			for _, partRaw := range v {
				part, ok := partRaw.(map[string]any)
				if !ok || part["type"] != "text" {
					continue
				}
				text, _ := part["text"].(string)
				out, t := compressText(text)
				if out != text {
					changed = true
					techniques = appendUnique(techniques, t)
				}
				part["text"] = out
			}
		}
	}

	if !changed {
		return body, compression.EngineStats{OriginalTokens: beforeTokens, CompressedTokens: beforeTokens}, nil
	}

	m["messages"] = messages
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
