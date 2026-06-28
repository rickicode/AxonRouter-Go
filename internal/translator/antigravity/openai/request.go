package openai

import (
	"encoding/json"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// convertOpenAIRequestToAntigravity converts an OpenAI Chat Completions request to Antigravity format.
// Antigravity wraps Gemini format with additional metadata.
func convertOpenAIRequestToAntigravity(modelName string, body []byte, stream bool) []byte {
	root := gjson.ParseBytes(body)

	// Antigravity envelope
	out := []byte(`{"request":{"contents":[]},"model":""}`)
	out, _ = sjson.SetBytes(out, "model", modelName)

	// Generation config
	genConfig := make(map[string]interface{})
	if temp := root.Get("temperature"); temp.Exists() {
		genConfig["temperature"] = temp.Float()
	}
	if maxTokens := root.Get("max_tokens"); maxTokens.Exists() {
		genConfig["maxOutputTokens"] = maxTokens.Int()
	}
	if topP := root.Get("top_p"); topP.Exists() {
		genConfig["topP"] = topP.Float()
	}
	if len(genConfig) > 0 {
		out, _ = sjson.SetRawBytes(out, "request.generationConfig", mustMarshal(genConfig))
	}

	// System instruction
	if sys := root.Get("messages"); sys.Exists() && sys.IsArray() {
		var systemParts []map[string]interface{}
		sys.ForEach(func(_, msg gjson.Result) bool {
			if msg.Get("role").String() == "system" {
				if content := msg.Get("content"); content.Exists() {
					if content.Type == gjson.String {
						systemParts = append(systemParts, map[string]interface{}{"text": content.String()})
					}
				}
			}
			return true
		})
		if len(systemParts) > 0 {
			out, _ = sjson.SetRawBytes(out, "request.systemInstruction", mustMarshal(map[string]interface{}{"parts": systemParts}))
		}
	}

	// Contents (messages)
	var contents []map[string]interface{}
	if msgs := root.Get("messages"); msgs.Exists() && msgs.IsArray() {
		msgs.ForEach(func(_, msg gjson.Result) bool {
			role := msg.Get("role").String()
			if role == "system" {
				return true
			}

			geminiRole := "user"
			if role == "assistant" {
				geminiRole = "model"
			}

			content := map[string]interface{}{
				"role": geminiRole,
			}

			var parts []map[string]interface{}
			if c := msg.Get("content"); c.Exists() {
				if c.Type == gjson.String {
					parts = append(parts, map[string]interface{}{"text": c.String()})
				} else if c.IsArray() {
					c.ForEach(func(_, part gjson.Result) bool {
						if part.Get("type").String() == "text" {
							parts = append(parts, map[string]interface{}{"text": part.Get("text").String()})
						}
						return true
					})
				}
			}

			if len(parts) > 0 {
				content["parts"] = parts
				contents = append(contents, content)
			}
			return true
		})
	}
	out, _ = sjson.SetRawBytes(out, "request.contents", mustMarshal(contents))

	return out
}

func mustMarshal(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}
