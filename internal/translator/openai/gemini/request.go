package gemini

import (
	"encoding/json"

	"github.com/tidwall/gjson"
)

// convertOpenAIRequestToGemini converts an OpenAI Chat Completions request to Gemini generateContent format.
func convertOpenAIRequestToGemini(modelName string, body []byte, stream bool) []byte {
	root := gjson.ParseBytes(body)

	out := make(map[string]interface{})

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
	if stop := root.Get("stop"); stop.Exists() && stop.IsArray() {
		var stops []string
		stop.ForEach(func(_, v gjson.Result) bool {
			stops = append(stops, v.String())
			return true
		})
		genConfig["stopSequences"] = stops
	}
	if len(genConfig) > 0 {
		out["generationConfig"] = genConfig
	}

	// System instruction
	if sys := root.Get("messages"); sys.Exists() && sys.IsArray() {
		var systemParts []map[string]interface{}
		sys.ForEach(func(_, msg gjson.Result) bool {
			if msg.Get("role").String() == "system" {
				if content := msg.Get("content"); content.Exists() {
					if content.Type == gjson.String {
						systemParts = append(systemParts, map[string]interface{}{"text": content.String()})
					} else if content.IsArray() {
						content.ForEach(func(_, part gjson.Result) bool {
							if part.Get("type").String() == "text" {
								systemParts = append(systemParts, map[string]interface{}{"text": part.Get("text").String()})
							}
							return true
						})
					}
				}
			}
			return true
		})
		if len(systemParts) > 0 {
			out["systemInstruction"] = map[string]interface{}{"parts": systemParts}
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
						pType := part.Get("type").String()
						switch pType {
						case "text":
							parts = append(parts, map[string]interface{}{"text": part.Get("text").String()})
						case "image_url":
							if url := part.Get("image_url.url"); url.Exists() {
								// Gemini uses inlineData with base64
								parts = append(parts, map[string]interface{}{
									"inlineData": map[string]interface{}{
										"mimeType": "image/jpeg",
										"data":     url.String(),
									},
								})
							}
						case "tool_use":
							parts = append(parts, map[string]interface{}{
								"functionCall": map[string]interface{}{
									"name": part.Get("name").String(),
									"args": json.RawMessage(part.Get("input").Raw),
								},
							})
						case "tool_result":
							parts = append(parts, map[string]interface{}{
								"functionResponse": map[string]interface{}{
									"name":     part.Get("tool_use_id").String(),
									"response": map[string]interface{}{"result": part.Get("content").String()},
								},
							})
						}
						return true
					})
				}
			}

			// Tool calls from assistant
			if toolCalls := msg.Get("tool_calls"); toolCalls.Exists() && toolCalls.IsArray() {
				toolCalls.ForEach(func(_, tc gjson.Result) bool {
					parts = append(parts, map[string]interface{}{
						"functionCall": map[string]interface{}{
							"name": tc.Get("function.name").String(),
							"args": json.RawMessage(tc.Get("function.arguments").Raw),
						},
					})
					return true
				})
			}

			if len(parts) > 0 {
				content["parts"] = parts
				contents = append(contents, content)
			}
			return true
		})
	}
	out["contents"] = contents

	// Tools
	if tools := root.Get("tools"); tools.Exists() && tools.IsArray() {
		var geminiTools []map[string]interface{}
		tools.ForEach(func(_, tool gjson.Result) bool {
			geminiTool := map[string]interface{}{
				"functionDeclarations": []map[string]interface{}{{
					"name": tool.Get("function.name").String(),
				}},
			}
			if desc := tool.Get("function.description"); desc.Exists() {
				geminiTool["functionDeclarations"].([]map[string]interface{})[0]["description"] = desc.String()
			}
			if params := tool.Get("function.parameters"); params.Exists() {
				geminiTool["functionDeclarations"].([]map[string]interface{})[0]["parameters"] = json.RawMessage(params.Raw)
			}
			geminiTools = append(geminiTools, geminiTool)
			return true
		})
		if len(geminiTools) > 0 {
			out["tools"] = geminiTools
		}
	}

	result, _ := json.Marshal(out)
	return result
}
