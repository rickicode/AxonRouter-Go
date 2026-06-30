package gemini

import (
	"encoding/json"
	"fmt"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// convertClaudeRequestToGemini converts a Claude Messages request to Gemini generateContent format.
func convertClaudeRequestToGemini(modelName string, body []byte, stream bool) []byte {
	root := gjson.ParseBytes(body)

	out := []byte(`{"contents":[]}`)
	out, _ = sjson.SetBytes(out, "model", modelName)

	// Generation config
	if maxTokens := root.Get("max_tokens"); maxTokens.Exists() {
		out, _ = sjson.SetBytes(out, "generationConfig.maxOutputTokens", maxTokens.Int())
	}
	if temp := root.Get("temperature"); temp.Exists() {
		out, _ = sjson.SetBytes(out, "generationConfig.temperature", temp.Float())
	}
	if topP := root.Get("top_p"); topP.Exists() {
		out, _ = sjson.SetBytes(out, "generationConfig.topP", topP.Float())
	}
	if stopSeqs := root.Get("stop_sequences"); stopSeqs.Exists() && stopSeqs.IsArray() {
		var stops []string
		stopSeqs.ForEach(func(_, v gjson.Result) bool {
			stops = append(stops, v.String())
			return true
		})
		if len(stops) > 0 {
			out, _ = sjson.SetBytes(out, "generationConfig.stopSequences", stops)
		}
	}

	// System instruction
	if sys := root.Get("system"); sys.Exists() {
		if sys.Type == gjson.String {
			out, _ = sjson.SetBytes(out, "systemInstruction.role", "user")
			out, _ = sjson.SetBytes(out, "systemInstruction.parts.0.text", sys.String())
		} else if sys.IsArray() {
			var parts []map[string]interface{}
			sys.ForEach(func(_, part gjson.Result) bool {
				if part.Get("type").String() == "text" {
					parts = append(parts, map[string]interface{}{
						"text": part.Get("text").String(),
					})
				}
				return true
			})
			if len(parts) > 0 {
				out, _ = sjson.SetBytes(out, "systemInstruction.role", "user")
				b, _ := json.Marshal(parts)
				out, _ = sjson.SetRawBytes(out, "systemInstruction.parts", b)
			}
		}
	}

	// Messages
	if messages := root.Get("messages"); messages.Exists() && messages.IsArray() {
		messages.ForEach(func(_, msg gjson.Result) bool {
			role := msg.Get("role").String()
			geminiRole := "user"
			if role == "assistant" {
				geminiRole = "model"
			}

			content := msg.Get("content")
			node := []byte(`{"role":"","parts":[]}`)
			node, _ = sjson.SetBytes(node, "role", geminiRole)

			p := 0
			if content.Type == gjson.String {
				node, _ = sjson.SetBytes(node, "parts.0.text", content.String())
				p = 1
			} else if content.IsArray() {
				content.ForEach(func(_, part gjson.Result) bool {
					pType := part.Get("type").String()
					switch pType {
					case "text":
						text := part.Get("text").String()
						if text != "" {
							node, _ = sjson.SetBytes(node, fmt.Sprintf("parts.%d.text", p), text)
							p++
						}
					case "image":
						source := part.Get("source")
						if source.Exists() {
							mimeType := source.Get("media_type").String()
							data := source.Get("data").String()
							if data != "" {
								node, _ = sjson.SetBytes(node, fmt.Sprintf("parts.%d.inlineData.mimeType", p), mimeType)
								node, _ = sjson.SetBytes(node, fmt.Sprintf("parts.%d.inlineData.data", p), data)
								p++
							}
						}
					case "tool_use":
						name := part.Get("name").String()
						input := part.Get("input").Raw
						fc := map[string]interface{}{
							"name": name,
						}
						if gjson.Valid(input) {
							var args interface{}
							json.Unmarshal([]byte(input), &args)
							fc["args"] = args
						}
						b, _ := json.Marshal(fc)
						node, _ = sjson.SetRawBytes(node, fmt.Sprintf("parts.%d.functionCall", p), b)
						p++
					case "tool_result":
						toolUseID := part.Get("tool_use_id").String()
						name := part.Get("name").String()
						resultContent := part.Get("content")
						var resultParts []map[string]interface{}
						if resultContent.Type == gjson.String {
							resultParts = []map[string]interface{}{{"text": resultContent.String()}}
						} else if resultContent.IsArray() {
							resultContent.ForEach(func(_, rp gjson.Result) bool {
								if rp.Get("type").String() == "text" {
									resultParts = append(resultParts, map[string]interface{}{"text": rp.Get("text").String()})
								}
								return true
							})
						}
						if len(resultParts) == 0 {
							resultParts = []map[string]interface{}{{"text": ""}}
						}
						fr := map[string]interface{}{
							"id":   toolUseID,
							"name": name,
							"response": map[string]interface{}{
								"result": resultParts,
							},
						}
						b, _ := json.Marshal(fr)
						node, _ = sjson.SetRawBytes(node, fmt.Sprintf("parts.%d.functionResponse", p), b)
						p++
					}
					return true
				})
			}

			out, _ = sjson.SetRawBytes(out, "contents.-1", node)
			return true
		})
	}

	// Tools
	if tools := root.Get("tools"); tools.Exists() && tools.IsArray() {
		var functionDeclarations []map[string]interface{}
		tools.ForEach(func(_, tool gjson.Result) bool {
			if tool.Get("type").String() == "function" {
				fn := tool.Get("function")
				if fn.Exists() {
					decl := map[string]interface{}{
						"name":        fn.Get("name").String(),
						"description": fn.Get("description").String(),
					}
					if params := fn.Get("parameters"); params.Exists() {
						var p interface{}
						json.Unmarshal([]byte(params.Raw), &p)
						decl["parameters"] = p
					}
					functionDeclarations = append(functionDeclarations, decl)
				}
			}
			return true
		})
		if len(functionDeclarations) > 0 {
			b, _ := json.Marshal(functionDeclarations)
			out, _ = sjson.SetRawBytes(out, "tools.0.functionDeclarations", b)
		}
	}

	return out
}
