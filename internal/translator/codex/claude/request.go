package claude

import (
	"encoding/json"
	"fmt"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// convertCodexRequestToClaude transforms a Codex Responses API request to Claude Messages format.
func convertCodexRequestToClaude(modelName string, body []byte, stream bool) []byte {
	root := gjson.ParseBytes(body)

	out := []byte(`{"messages":[]}`)
	out, _ = sjson.SetBytes(out, "model", modelName)
	out, _ = sjson.SetBytes(out, "max_tokens", 4096)
	out, _ = sjson.SetBytes(out, "stream", stream)

	// Max output tokens (Responses format uses max_output_tokens)
	if maxTokens := root.Get("max_output_tokens"); maxTokens.Exists() {
		out, _ = sjson.SetBytes(out, "max_tokens", maxTokens.Int())
	} else if maxTokens := root.Get("max_tokens"); maxTokens.Exists() {
		out, _ = sjson.SetBytes(out, "max_tokens", maxTokens.Int())
	}

	// Temperature
	if temp := root.Get("temperature"); temp.Exists() {
		out, _ = sjson.SetBytes(out, "temperature", temp.Float())
	}
	if topP := root.Get("top_p"); topP.Exists() {
		out, _ = sjson.SetBytes(out, "top_p", topP.Float())
	}

	// System prompt
	if sys := root.Get("system"); sys.Exists() {
		if sys.Type == gjson.String {
			out, _ = sjson.SetBytes(out, "system", sys.String())
		}
	}

	// Input messages (Codex Responses format uses "input" array)
	if input := root.Get("input"); input.Exists() && input.IsArray() {
		input.ForEach(func(_, item gjson.Result) bool {
			itemType := item.Get("type").String()

			switch itemType {
			case "message":
				role := item.Get("role").String()
				claudeRole := "user"
				if role == "assistant" || role == "model" {
					claudeRole = "assistant"
				} else if role == "developer" || role == "system" {
					claudeRole = "user"
				}

				content := item.Get("content")
				claudeMsg := map[string]interface{}{
					"role": claudeRole,
				}

				var contentParts []map[string]interface{}
				if content.Type == gjson.String {
					claudeMsg["content"] = content.String()
				} else if content.IsArray() {
					content.ForEach(func(_, part gjson.Result) bool {
						pType := part.Get("type").String()
						switch pType {
						case "input_text":
							contentParts = append(contentParts, map[string]interface{}{
								"type": "text",
								"text": part.Get("text").String(),
							})
						case "input_image":
							if url := part.Get("image_url"); url.Exists() {
								contentParts = append(contentParts, map[string]interface{}{
									"type": "image",
									"source": map[string]interface{}{
										"type": "url",
										"url":  url.String(),
									},
								})
							}
						}
						return true
					})
					claudeMsg["content"] = contentParts
				}

				out, _ = sjson.SetRawBytes(out, "messages.-1", mustMarshal(claudeMsg))

			case "function_call":
				name := item.Get("name").String()
				callID := item.Get("call_id").String()
				args := item.Get("arguments").Raw
				var input map[string]interface{}
				if args != "" {
					json.Unmarshal([]byte(args), &input)
				}
				if input == nil {
					input = map[string]interface{}{}
				}
				claudeMsg := map[string]interface{}{
					"role": "assistant",
					"content": []map[string]interface{}{{
						"type":  "tool_use",
						"id":    callID,
						"name":  name,
						"input": input,
					}},
				}
				out, _ = sjson.SetRawBytes(out, "messages.-1", mustMarshal(claudeMsg))

			case "function_call_output":
				callID := item.Get("call_id").String()
				output := item.Get("output").String()
				claudeMsg := map[string]interface{}{
					"role": "user",
					"content": []map[string]interface{}{{
						"type":        "tool_result",
						"tool_use_id": callID,
						"content":     output,
					}},
				}
				out, _ = sjson.SetRawBytes(out, "messages.-1", mustMarshal(claudeMsg))
			}
			return true
		})
	}

	// Tools
	if tools := root.Get("tools"); tools.Exists() && tools.IsArray() {
		var functionTools []map[string]interface{}
		tools.ForEach(func(_, tool gjson.Result) bool {
			tType := tool.Get("type").String()
			if tType == "function" {
				fn := map[string]interface{}{
					"name":        tool.Get("name").String(),
					"description": tool.Get("description").String(),
				}
				if params := tool.Get("parameters"); params.Exists() {
					var p interface{}
					json.Unmarshal([]byte(params.Raw), &p)
					fn["parameters"] = p
				}
				functionTools = append(functionTools, map[string]interface{}{
					"type":     "function",
					"function": fn,
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
