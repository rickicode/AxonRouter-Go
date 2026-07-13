package usage

import (
	"encoding/json"
	"unicode/utf8"
)

// EstimateTokensFromString estimates token count from a string using rune count / 4 heuristic.
func EstimateTokensFromString(s string) int64 {
	return int64(utf8.RuneCountInString(s) / 4)
}

// EstimateTokensFromRequest estimates input token count from a request body
// by summing "messages[].content" and top-level "system" field lengths, divided by 4.
func EstimateTokensFromRequest(body []byte) int64 {
	var doc map[string]interface{}
	if err := json.Unmarshal(body, &doc); err != nil {
		return int64(len(body) / 4)
	}

	var totalChars int64

	// Collect system field if present at top level
	if sys, ok := doc["system"]; ok {
		if s, ok := sys.(string); ok {
			totalChars += int64(len(s))
		}
	}

	// Collect messages[].content
	if msgs, ok := doc["messages"]; ok {
		if arr, ok := msgs.([]interface{}); ok {
			for _, msg := range arr {
				if m, ok := msg.(map[string]interface{}); ok {
					if c, ok := m["content"]; ok {
						switch v := c.(type) {
						case string:
							totalChars += int64(len(v))
						case []interface{}:
							// Anthropic multi-part content
							for _, part := range v {
								if p, ok := part.(map[string]interface{}); ok {
									if t, ok := p["text"]; ok {
										if s, ok := t.(string); ok {
											totalChars += int64(len(s))
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}

	return totalChars / 4
}

// EstimateTokensFromResponse estimates output token count from a response body
// by trying known content paths and falling back to body length / 4.
func EstimateTokensFromResponse(body []byte) int64 {
	var doc map[string]interface{}
	if err := json.Unmarshal(body, &doc); err != nil {
		return int64(len(body) / 4)
	}

	content := ""

	// 1. choices[0].message.content (OpenAI chat)
	if choices, ok := doc["choices"].([]interface{}); ok && len(choices) > 0 {
		if first, ok := choices[0].(map[string]interface{}); ok {
			if msg, ok := first["message"].(map[string]interface{}); ok {
				if c, ok := msg["content"].(string); ok {
					content = c
				}
			}
			if content == "" {
				if c, ok := first["text"].(string); ok {
					content = c
				}
			}
		}
	}

	// 2. output_text (Claude / some models)
	if content == "" {
		if c, ok := doc["output_text"].(string); ok {
			content = c
		}
	}

	// 3. response.output[0].content[0].text (Vertex AI / newer Claude messages)
	if content == "" {
		if resp, ok := doc["response"].(map[string]interface{}); ok {
			if output, ok := resp["output"].([]interface{}); ok && len(output) > 0 {
				if first, ok := output[0].(map[string]interface{}); ok {
					if cArr, ok := first["content"].([]interface{}); ok && len(cArr) > 0 {
						if cObj, ok := cArr[0].(map[string]interface{}); ok {
							if t, ok := cObj["text"].(string); ok {
								content = t
							}
						}
					}
				}
			}
		}
	}

	if content != "" {
		return EstimateTokensFromString(content)
	}

	return int64(len(body) / 4)
}
