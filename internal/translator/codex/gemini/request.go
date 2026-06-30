package gemini

import (
	"encoding/json"
	"fmt"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// convertCodexRequestToGemini transforms a Codex Responses API request to Gemini generateContent format.
func convertCodexRequestToGemini(modelName string, body []byte, stream bool) []byte {
	root := gjson.ParseBytes(body)

	out := []byte(`{"contents":[]}`)
	out, _ = sjson.SetBytes(out, "model", modelName)

	// Max output tokens
	if maxTokens := root.Get("max_output_tokens"); maxTokens.Exists() {
		out, _ = sjson.SetBytes(out, "generationConfig.maxOutputTokens", maxTokens.Int())
	} else if maxTokens := root.Get("max_tokens"); maxTokens.Exists() {
		out, _ = sjson.SetBytes(out, "generationConfig.maxOutputTokens", maxTokens.Int())
	}

	// Temperature
	if temp := root.Get("temperature"); temp.Exists() {
		out, _ = sjson.SetBytes(out, "generationConfig.temperature", temp.Float())
	}
	if topP := root.Get("top_p"); topP.Exists() {
		out, _ = sjson.SetBytes(out, "generationConfig.topP", topP.Float())
	}

	// System prompt
	if sys := root.Get("system"); sys.Exists() {
		if sys.Type == gjson.String {
			out, _ = sjson.SetBytes(out, "systemInstruction.role", "user")
			out, _ = sjson.SetBytes(out, "systemInstruction.parts.0.text", sys.String())
		}
	}

	// Input messages (Codex Responses format uses "input" array)
	if input := root.Get("input"); input.Exists() && input.IsArray() {
		input.ForEach(func(_, item gjson.Result) bool {
			itemType := item.Get("type").String()

			switch itemType {
			case "message":
				role := item.Get("role").String()
				geminiRole := "user"
				if role == "assistant" || role == "model" {
					geminiRole = "model"
				}

				content := item.Get("content")
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
						case "input_text":
							node, _ = sjson.SetBytes(node, fmt.Sprintf("parts.%d.text", p), part.Get("text").String())
							p++
						case "input_image":
							if url := part.Get("image_url"); url.Exists() {
								node, _ = sjson.SetBytes(node, fmt.Sprintf("parts.%d.inlineData.mimeType", p), "image/jpeg")
								node, _ = sjson.SetBytes(node, fmt.Sprintf("parts.%d.inlineData.data", p), url.String())
								p++
							}
						}
						return true
					})
				}

				out, _ = sjson.SetRawBytes(out, "contents.-1", node)

			case "function_call":
				name := item.Get("name").String()
				callID := item.Get("call_id").String()
				args := item.Get("arguments").Raw
				var argsMap map[string]interface{}
				if args != "" {
					json.Unmarshal([]byte(args), &argsMap)
				}
				if argsMap == nil {
					argsMap = map[string]interface{}{}
				}

				node := []byte(`{"role":"model","parts":[]}`)
				node, _ = sjson.SetRawBytes(node, "parts.0.functionCall", mustMarshal(map[string]interface{}{
					"id":   callID,
					"name": name,
					"args": argsMap,
				}))
				out, _ = sjson.SetRawBytes(out, "contents.-1", node)

			case "function_call_output":
				callID := item.Get("call_id").String()
				output := item.Get("output").String()

				node := []byte(`{"role":"user","parts":[]}`)
				node, _ = sjson.SetRawBytes(node, "parts.0.functionResponse", mustMarshal(map[string]interface{}{
					"id":   callID,
					"name": "function",
					"response": map[string]interface{}{
						"result": output,
					},
				}))
				out, _ = sjson.SetRawBytes(out, "contents.-1", node)
			}
			return true
		})
	}

	// Tools
	if tools := root.Get("tools"); tools.Exists() && tools.IsArray() {
		var functionDeclarations []map[string]interface{}
		tools.ForEach(func(_, tool gjson.Result) bool {
			tType := tool.Get("type").String()
			if tType == "function" {
				decl := map[string]interface{}{
					"name":        tool.Get("name").String(),
					"description": tool.Get("description").String(),
				}
				if params := tool.Get("parameters"); params.Exists() {
					var p interface{}
					json.Unmarshal([]byte(params.Raw), &p)
					decl["parameters"] = p
				}
				functionDeclarations = append(functionDeclarations, decl)
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

func mustMarshal(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}

func itoa(i int) string {
	return fmt.Sprintf("%d", i)
}
