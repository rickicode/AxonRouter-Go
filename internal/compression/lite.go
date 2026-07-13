package compression

import (
	"encoding/json"
	"regexp"
	"strings"
)

var imageDataURLRE, imageDataURLREErr = regexp.Compile(strings.ReplaceAll("data:IMAGE/[^;]+;base64,[A-Za-z0-9+/=]+", "IMAGE", "image"))

// LiteConfig controls the always-on baseline compressor.
type LiteConfig struct {
	CollapseWhitespace     bool `json:"collapse_whitespace"`
	ReplaceImageUrls       bool `json:"replace_image_urls"`
	RemoveRedundantContent bool `json:"remove_redundant_content"`
	DedupSystemPrompt      bool `json:"dedup_system_prompt"`
}

// ApplyLite runs the always-on whitespace / image-url compressor.
// It is fail-open: on any error the original body is returned with zero stats.
func ApplyLite(body []byte, cfg LiteConfig) ([]byte, EngineStats, error) {
	original := string(body)
	beforeTokens := EstimateTokens(original)
	techniques := []string{}

	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return body, EngineStats{OriginalTokens: beforeTokens, CompressedTokens: beforeTokens}, nil
	}

	messages, _ := m["messages"].([]any)
	if messages == nil {
		return body, EngineStats{OriginalTokens: beforeTokens, CompressedTokens: beforeTokens}, nil
	}

	// Helpers
	isSystem := func(msg map[string]any) bool {
		r, _ := msg["role"].(string)
		return r == "system"
	}
	equalContent := func(a, b map[string]any) bool {
		ac, _ := json.Marshal(a["content"])
		bc, _ := json.Marshal(b["content"])
		return string(ac) == string(bc)
	}

	var deduped []any
	var lastSys map[string]any
	for i, raw := range messages {
		msg, ok := raw.(map[string]any)
		if !ok {
			deduped = append(deduped, raw)
			continue
		}
		if cfg.DedupSystemPrompt && isSystem(msg) {
			if lastSys != nil && equalContent(msg, lastSys) {
				techniques = append(techniques, "dedup_system_prompt")
				continue
			}
			lastSys = msg
		}
		if cfg.RemoveRedundantContent && i > 0 {
			prev, ok := messages[i-1].(map[string]any)
			if ok {
				pr, _ := prev["role"].(string)
				cr, _ := msg["role"].(string)
				if pr == "assistant" && cr == "assistant" && equalContent(prev, msg) {
					techniques = append(techniques, "remove_redundant_content")
					continue
				}
			}
		}
		deduped = append(deduped, raw)
	}

	for _, raw := range deduped {
		msg, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		content := msg["content"]
		switch v := content.(type) {
		case string:
			out := v
			if cfg.CollapseWhitespace {
				out = CleanSpaces(out)
				if out != v {
					techniques = appendUnique(techniques, "collapse_whitespace")
				}
			}
			if cfg.ReplaceImageUrls {
				out = replaceImageDataURLs(out)
				if out != v && !contains(techniques, "replace_image_urls") {
					techniques = append(techniques, "replace_image_urls")
				}
			}
			msg["content"] = out
		case []any:
			for _, partRaw := range v {
				part, ok := partRaw.(map[string]any)
				if !ok {
					continue
				}
				if part["type"] != "text" {
					continue
				}
				text, _ := part["text"].(string)
				out := text
				if cfg.CollapseWhitespace {
					out = CleanSpaces(out)
					if out != text {
						techniques = appendUnique(techniques, "collapse_whitespace")
					}
				}
				if cfg.ReplaceImageUrls {
					out = replaceImageDataURLs(out)
					if out != text && !contains(techniques, "replace_image_urls") {
						techniques = append(techniques, "replace_image_urls")
					}
				}
				part["text"] = out
			}
		}
	}

	m["messages"] = deduped
	outBody, err := json.Marshal(m)
	if err != nil {
		return body, EngineStats{OriginalTokens: beforeTokens, CompressedTokens: beforeTokens}, nil
	}
	afterTokens := EstimateTokens(string(outBody))

	savings := 0.0
	if beforeTokens > 0 {
		savings = (1.0 - float64(afterTokens)/float64(beforeTokens)) * 100
	}

	return outBody, EngineStats{
		OriginalTokens:   beforeTokens,
		CompressedTokens: afterTokens,
		SavingsPercent:   savings,
		TechniquesUsed:   techniques,
	}, nil
}

// replaceImageDataURLs substitutes base64 image data URLs with [image].
func replaceImageDataURLs(text string) string {
	if imageDataURLREErr != nil || !strings.Contains(text, "data:") {
		return text
	}
	return imageDataURLRE.ReplaceAllString(text, "[image]")
}

func appendUnique(slice []string, s string) []string {
	if contains(slice, s) {
		return slice
	}
	return append(slice, s)
}

func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
