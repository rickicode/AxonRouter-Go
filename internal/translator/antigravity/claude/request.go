package claude

import (
	"encoding/json"
	"fmt"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// convertAntigravityRequestToClaude converts an Antigravity request to Claude Messages format.
// Antigravity uses a Gemini-like format with request.contents and request.systemInstruction.
func convertAntigravityRequestToClaude(modelName string, body []byte, stream bool) []byte {
	root := gjson.ParseBytes(body)

	out := []byte(`{"messages":[]}`)
	out, _ = sjson.SetBytes(out, "model", modelName)
	out, _ = sjson.SetBytes(out, "max_tokens", 4096)
	out, _ = sjson.SetBytes(out, "stream", stream)

	// Generation config
	if genConfig := root.Get("request.generationConfig"); genConfig.Exists() {
		if maxTokens := genConfig.Get("maxOutputTokens"); maxTokens.Exists() {
			out, _ = sjson.SetBytes(out, "max_tokens", maxTokens.Int())
		}
		if temp := genConfig.Get("temperature"); temp.Exists() {
			out, _ = sjson.SetBytes(out, "temperature", temp.Float())
		}
		if topP := genConfig.Get("topP"); topP.Exists() {
			out, _ = sjson.SetBytes(out, "top_p", topP.Float())
		}
	}

	// System instruction
	if sysInst := root.Get("request.systemInstruction"); sysInst.Exists() {
		var systemParts []string
		if parts := sysInst.Get("parts"); parts.Exists() && parts.IsArray() {
			parts.ForEach(func(_, part gjson.Result) bool {
				if text := part.Get("text"); text.Exists() {
					systemParts = append(systemParts, text.String())
				}
				return true
			})
		}
		if len(systemParts) > 0 {
			out, _ = sjson.SetBytes(out, "system", joinStrings(systemParts))
		}
	}

	// Messages
	if contents := root.Get("request.contents"); contents.Exists() && contents.IsArray() {
		contents.ForEach(func(_, content gjson.Result) bool {
			role := content.Get("role").String()
			claudeRole := "user"
			if role == "model" {
				claudeRole = "assistant"
			}

			claudeMsg := map[string]interface{}{
				"role": claudeRole,
			}

			var contentParts []map[string]interface{}
			if parts := content.Get("parts"); parts.Exists() && parts.IsArray() {
				parts.ForEach(func(_, part gjson.Result) bool {
					if text := part.Get("text"); text.Exists() {
						contentParts = append(contentParts, map[string]interface{}{
							"type": "text",
							"text": text.String(),
						})
					}
					if fc := part.Get("functionCall"); fc.Exists() {
						name := fc.Get("name").String()
						args := fc.Get("args").Raw
						var input map[string]interface{}
						if args != "" {
							json.Unmarshal([]byte(args), &input)
						}
						if input == nil {
							input = map[string]interface{}{}
						}
						contentParts = append(contentParts, map[string]interface{}{
							"type":  "tool_use",
							"id":    "toolu_" + name,
							"name":  name,
							"input": input,
						})
					}
					if fr := part.Get("functionResponse"); fr.Exists() {
						name := fr.Get("name").String()
						result := fr.Get("response.result")
						var resultContent interface{}
						if result.Exists() {
							if result.Type == gjson.String {
								resultContent = result.String()
							} else {
								resultContent = result.Raw
							}
						} else {
							resultContent = "{}"
						}
						contentParts = append(contentParts, map[string]interface{}{
							"type":        "tool_result",
							"tool_use_id": "toolu_" + name,
							"name":        name,
							"content":     resultContent,
						})
					}
					return true
				})
			}

			if len(contentParts) == 1 && contentParts[0]["type"] == "text" {
				claudeMsg["content"] = contentParts[0]["text"]
			} else {
				claudeMsg["content"] = contentParts
			}

			out, _ = sjson.SetRawBytes(out, "messages.-1", mustMarshal(claudeMsg))
			return true
		})
	}

	// Tools
	if tools := root.Get("request.tools"); tools.Exists() && tools.IsArray() {
		var functionTools []map[string]interface{}
		tools.ForEach(func(_, tool gjson.Result) bool {
			if decls := tool.Get("functionDeclarations"); decls.Exists() && decls.IsArray() {
				decls.ForEach(func(_, decl gjson.Result) bool {
					fn := map[string]interface{}{
						"name":        decl.Get("name").String(),
						"description": decl.Get("description").String(),
					}
					if params := decl.Get("parameters"); params.Exists() {
						var p interface{}
						json.Unmarshal([]byte(params.Raw), &p)
						fn["parameters"] = p
					}
					functionTools = append(functionTools, map[string]interface{}{
						"type":     "function",
						"function": fn,
					})
					return true
				})
			}
			return true
		})
		if len(functionTools) > 0 {
			out, _ = sjson.SetRawBytes(out, "tools", mustMarshal(functionTools))
		}
	}

	return out
}

func mustMarshal(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}

func itoa(i int) string {
	return fmt.Sprintf("%d", i)
}

func joinStrings(parts []string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += "\n"
		}
		result += p
	}
	return result
}
