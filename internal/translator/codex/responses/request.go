package responses

import (
	"encoding/json"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// ConvertOpenAIRequestToCodex converts an OpenAI Chat Completions request to Codex Responses format.
// Codex uses the OpenAI Responses API format which is different from Chat Completions.
func ConvertOpenAIRequestToCodex(modelName string, body []byte, stream bool) []byte {
	root := gjson.ParseBytes(body)

	out := []byte(`{}`)
	out, _ = sjson.SetBytes(out, "model", modelName)
	out, _ = sjson.SetBytes(out, "stream", stream)
	out, _ = sjson.SetBytes(out, "store", false)

	// Convert messages array to input array (Responses format)
	if messages := root.Get("messages"); messages.Exists() && messages.IsArray() {
		var input []map[string]interface{}
		messages.ForEach(func(_, msg gjson.Result) bool {
			role := msg.Get("role").String()
			content := msg.Get("content")

			item := map[string]interface{}{}
			if role == "system" {
				item["role"] = "developer"
			} else {
				item["role"] = role
			}

			if content.Type == gjson.String {
				item["type"] = "message"
				item["content"] = []map[string]interface{}{{
					"type": "input_text",
					"text": content.String(),
				}}
			} else if content.Exists() && content.IsArray() {
				item["type"] = "message"
				var parts []map[string]interface{}
				content.ForEach(func(_, part gjson.Result) bool {
					pType := part.Get("type").String()
					if pType == "text" {
						parts = append(parts, map[string]interface{}{
							"type": "input_text",
							"text": part.Get("text").String(),
						})
					} else if pType == "image_url" {
						parts = append(parts, map[string]interface{}{
							"type":      "input_image",
							"image_url": part.Get("image_url.url").String(),
						})
					}
					return true
				})
				item["content"] = parts
			}
			input = append(input, item)
			return true
		})
		out, _ = sjson.SetRawBytes(out, "input", mustMarshal(input))
	}

	// Max tokens (Responses format uses max_output_tokens)
	if maxTokens := root.Get("max_tokens"); maxTokens.Exists() {
		out, _ = sjson.SetBytes(out, "max_output_tokens", maxTokens.Int())
	}

	// Tools (convert from Chat Completions to Responses format)
	if tools := root.Get("tools"); tools.Exists() && tools.IsArray() {
		var responsesTools []map[string]interface{}
		tools.ForEach(func(_, tool gjson.Result) bool {
			t := map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
					"name":        tool.Get("function.name").String(),
					"description": tool.Get("function.description").String(),
					"parameters":  parseJSON(tool.Get("function.parameters").Raw),
				},
			}
			responsesTools = append(responsesTools, t)
			return true
		})
		out, _ = sjson.SetRawBytes(out, "tools", mustMarshal(responsesTools))
	}

	return out
}

func mustMarshal(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}

func parseJSON(raw string) interface{} {
	var v interface{}
	json.Unmarshal([]byte(raw), &v)
	return v
}
