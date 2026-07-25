package output

import (
	"encoding/json"

	"github.com/rickicode/AxonRouter-Go/internal/compression"
)

func init() {
	compression.Register(&Engine{})
}

const promptCaveman = "Be extremely terse. Use the fewest words possible. Prefer code, data, and bullet points over prose. Drop articles, hedging, filler words, and explanations unless asked. Never apologize or restate the question."

const promptPonytail = "Output only the minimal code or changes needed. Do not add comments, examples, explanations, docstrings, tests, or imports unless explicitly requested. Follow YAGNI: if something is not required, omit it."

// Engine injects an output-side system prompt into OpenAI-style request bodies.
// It is fail-open: any parse error returns the unmodified body.
type Engine struct{}

// ID returns the engine identifier.
func (e *Engine) ID() string { return "output" }

// Apply injects a terse/YAGNI system prompt into the first available system
// message, or prepends a new system message when none exists. Unparseable or
// unsupported bodies are returned unchanged.
func (e *Engine) Apply(body []byte, config compression.EngineConfig) ([]byte, compression.EngineStats, error) {
	original := string(body)
	beforeTokens := compression.EstimateTokens(original)

	level, _ := config["level"].(string)
	prompt, ok := promptForLevel(level)
	if !ok {
		return body, compression.EngineStats{OriginalTokens: beforeTokens, CompressedTokens: beforeTokens}, nil
	}

	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return body, compression.EngineStats{OriginalTokens: beforeTokens, CompressedTokens: beforeTokens}, nil
	}

	messages, _ := m["messages"].([]any)
	if messages == nil {
		return body, compression.EngineStats{OriginalTokens: beforeTokens, CompressedTokens: beforeTokens}, nil
	}

	changed := false
	for _, raw := range messages {
		msg, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		if role != "system" {
			continue
		}
		changed = injectPrompt(msg, prompt)
		break // only touch the first system message
	}

	if !changed {
		// No existing system message — prepend one.
		messages = append([]any{map[string]any{"role": "system", "content": prompt}}, messages...)
		changed = true
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
	technique := "output_" + level

	return outBody, compression.EngineStats{
		OriginalTokens:   beforeTokens,
		CompressedTokens: afterTokens,
		SavingsPercent:   savingsPercent(beforeTokens, afterTokens),
		TechniquesUsed:   []string{technique},
	}, nil
}

func promptForLevel(level string) (string, bool) {
	switch level {
	case "caveman":
		return promptCaveman, true
	case "ponytail":
		return promptPonytail, true
	default:
		return "", false
	}
}

func injectPrompt(msg map[string]any, prompt string) bool {
	content := msg["content"]
	switch v := content.(type) {
	case string:
		if v == "" {
			msg["content"] = prompt
		} else {
			msg["content"] = v + "\n\n" + prompt
		}
		return true
	case []any:
		for _, partRaw := range v {
			part, ok := partRaw.(map[string]any)
			if !ok || part["type"] != "text" {
				continue
			}
			text, _ := part["text"].(string)
			if text == "" {
				part["text"] = prompt
			} else {
				part["text"] = text + "\n\n" + prompt
			}
			return true
		}
		// No text part found — append a text part with the prompt.
		msg["content"] = append(v, map[string]any{"type": "text", "text": prompt})
		return true
	}
	return false
}

func savingsPercent(before, after int) float64 {
	if before <= 0 {
		return 0.0
	}
	return (1.0 - float64(after)/float64(before)) * 100
}
