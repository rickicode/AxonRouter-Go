package openai

import (
	"encoding/json"
	"fmt"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// convertGeminiRequestToOpenAI converts a Gemini generateContent request to OpenAI Chat Completions format.
func convertGeminiRequestToOpenAI(modelName string, body []byte, stream bool) []byte {
	root := gjson.ParseBytes(body)

	out := []byte(`{"model":"","messages":[]}`)
	out, _ = sjson.SetBytes(out, "model", modelName)
	out, _ = sjson.SetBytes(out, "stream", stream)

	// Generation config
	if genConfig := root.Get("generationConfig"); genConfig.Exists() {
		if temp := genConfig.Get("temperature"); temp.Exists() {
			out, _ = sjson.SetBytes(out, "temperature", temp.Float())
		}
		if maxTokens := genConfig.Get("maxOutputTokens"); maxTokens.Exists() {
			out, _ = sjson.SetBytes(out, "max_tokens", maxTokens.Int())
		}
		if topP := genConfig.Get("topP"); topP.Exists() {
			out, _ = sjson.SetBytes(out, "top_p", topP.Float())
		}
		if stop := genConfig.Get("stopSequences"); stop.Exists() && stop.IsArray() {
			var stops []string
			stop.ForEach(func(_, v gjson.Result) bool {
				stops = append(stops, v.String())
				return true
			})
			if len(stops) == 1 {
				out, _ = sjson.SetBytes(out, "stop", stops[0])
			} else if len(stops) > 1 {
				out, _ = sjson.SetBytes(out, "stop", stops)
			}
		}
	}

	// System instruction
	if sysInst := root.Get("systemInstruction"); sysInst.Exists() {
		if parts := sysInst.Get("parts"); parts.Exists() && parts.IsArray() {
			var textParts []string
			parts.ForEach(func(_, part gjson.Result) bool {
				if text := part.Get("text"); text.Exists() {
					textParts = append(textParts, text.String())
				}
				return true
			})
			if len(textParts) > 0 {
				joined := ""
				for i, t := range textParts {
					if i > 0 {
						joined += "\n"
					}
					joined += t
				}
				sysMsg := []byte(`{"role":"system","content":""}`)
				sysMsg, _ = sjson.SetBytes(sysMsg, "content", joined)
				out, _ = sjson.SetRawBytes(out, "messages.-1", sysMsg)
			}
		}
	}

	toolCallIdx := 0

	// Contents
	if contents := root.Get("contents"); contents.Exists() && contents.IsArray() {
		contents.ForEach(func(_, content gjson.Result) bool {
			role := content.Get("role").String()
			oaiRole := "user"
			if role == "model" {
				oaiRole = "assistant"
			}

			oaiMsg := []byte(`{"role":"","content":[]}`)
			oaiMsg, _ = sjson.SetBytes(oaiMsg, "role", oaiRole)

			var toolCalls []map[string]interface{}

			if parts := content.Get("parts"); parts.Exists() && parts.IsArray() {
				parts.ForEach(func(_, part gjson.Result) bool {
					if text := part.Get("text"); text.Exists() {
						partJSON := []byte(`{"type":"text","text":""}`)
						partJSON, _ = sjson.SetBytes(partJSON, "text", text.String())
						oaiMsg, _ = sjson.SetRawBytes(oaiMsg, "content.-1", partJSON)
					}
					if fc := part.Get("functionCall"); fc.Exists() {
						toolCallIdx++
						tc := map[string]interface{}{
							"id":   fmt.Sprintf("call_%d", toolCallIdx),
							"type": "function",
							"function": map[string]interface{}{
								"name":      fc.Get("name").String(),
								"arguments": fc.Get("args").Raw,
							},
						}
						toolCalls = append(toolCalls, tc)
					}
					if fr := part.Get("functionResponse"); fr.Exists() {
						partJSON := []byte(`{"type":"tool_result","tool_use_id":"","content":""}`)
						partJSON, _ = sjson.SetBytes(partJSON, "tool_use_id", fr.Get("name").String())
						if resp := fr.Get("response.result"); resp.Exists() {
							partJSON, _ = sjson.SetBytes(partJSON, "content", resp.String())
						}
						oaiMsg, _ = sjson.SetRawBytes(oaiMsg, "content.-1", partJSON)
					}
					return true
				})
			}

			if len(toolCalls) > 0 {
				oaiMsg, _ = sjson.SetRawBytes(oaiMsg, "tool_calls", mustMarshal(toolCalls))
			}

			out, _ = sjson.SetRawBytes(out, "messages.-1", oaiMsg)
			return true
		})
	}

	// Tools
	if tools := root.Get("tools"); tools.Exists() && tools.IsArray() {
		var oaiTools []map[string]interface{}
		tools.ForEach(func(_, tool gjson.Result) bool {
			if funcDecls := tool.Get("functionDeclarations"); funcDecls.Exists() && funcDecls.IsArray() {
				funcDecls.ForEach(func(_, fd gjson.Result) bool {
					oaiTool := map[string]interface{}{
						"type": "function",
						"function": map[string]interface{}{
							"name": fd.Get("name").String(),
						},
					}
					if desc := fd.Get("description"); desc.Exists() {
						oaiTool["function"].(map[string]interface{})["description"] = desc.String()
					}
					if params := fd.Get("parameters"); params.Exists() {
						oaiTool["function"].(map[string]interface{})["parameters"] = json.RawMessage(params.Raw)
					}
					oaiTools = append(oaiTools, oaiTool)
					return true
				})
			}
			return true
		})
		if len(oaiTools) > 0 {
			out, _ = sjson.SetRawBytes(out, "tools", mustMarshal(oaiTools))
		}
	}

	return out
}

func mustMarshal(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}
