package openai

import (
	"encoding/json"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// convertClaudeRequestToOpenAI converts an Anthropic Messages request to OpenAI Chat Completions format.
func convertClaudeRequestToOpenAI(modelName string, body []byte, stream bool) []byte {
	root := gjson.ParseBytes(body)

	out := []byte(`{"model":"","messages":[]}`)
	out, _ = sjson.SetBytes(out, "model", modelName)
	out, _ = sjson.SetBytes(out, "stream", stream)

	if maxTokens := root.Get("max_tokens"); maxTokens.Exists() {
		out, _ = sjson.SetBytes(out, "max_tokens", maxTokens.Int())
	}
	if temp := root.Get("temperature"); temp.Exists() {
		out, _ = sjson.SetBytes(out, "temperature", temp.Float())
	}
	if topP := root.Get("top_p"); topP.Exists() {
		out, _ = sjson.SetBytes(out, "top_p", topP.Float())
	}

	// Stop sequences
	if stop := root.Get("stop_sequences"); stop.Exists() && stop.IsArray() {
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

	// System message
	if sys := root.Get("system"); sys.Exists() {
		systemContent := ""
		if sys.Type == gjson.String {
			systemContent = sys.String()
		}
		if systemContent != "" {
			sysMsg := []byte(`{"role":"system","content":""}`)
			sysMsg, _ = sjson.SetBytes(sysMsg, "content", systemContent)
			out, _ = sjson.SetRawBytes(out, "messages.-1", sysMsg)
		}
	}

	// Messages
	if msgs := root.Get("messages"); msgs.Exists() && msgs.IsArray() {
		msgs.ForEach(func(_, msg gjson.Result) bool {
			role := msg.Get("role").String()

			// Skip thinking blocks in user messages
			if content := msg.Get("content"); content.Exists() {
				if content.Type == gjson.String {
					oaiMsg := []byte(`{"role":"","content":""}`)
					oaiMsg, _ = sjson.SetBytes(oaiMsg, "role", role)
					oaiMsg, _ = sjson.SetBytes(oaiMsg, "content", content.String())
					out, _ = sjson.SetRawBytes(out, "messages.-1", oaiMsg)
				} else if content.IsArray() {
					oaiMsg := []byte(`{"role":"","content":[]}`)
					oaiMsg, _ = sjson.SetBytes(oaiMsg, "role", role)

					var toolCalls []map[string]interface{}

					content.ForEach(func(_, part gjson.Result) bool {
						pType := part.Get("type").String()
						switch pType {
						case "text":
							partJSON := []byte(`{"type":"text","text":""}`)
							partJSON, _ = sjson.SetBytes(partJSON, "text", part.Get("text").String())
							oaiMsg, _ = sjson.SetRawBytes(oaiMsg, "content.-1", partJSON)
						case "image":
							if source := part.Get("source"); source.Exists() {
								partJSON := []byte(`{"type":"image_url","image_url":{"url":""}}`)
								partJSON, _ = sjson.SetBytes(partJSON, "image_url.url", source.Get("url").String())
								oaiMsg, _ = sjson.SetRawBytes(oaiMsg, "content.-1", partJSON)
							}
						case "tool_use":
							tc := map[string]interface{}{
								"id":   part.Get("id").String(),
								"type": "function",
								"function": map[string]interface{}{
									"name":      part.Get("name").String(),
									"arguments": part.Get("input").Raw,
								},
							}
							toolCalls = append(toolCalls, tc)
						case "tool_result":
							partJSON := []byte(`{"type":"tool_result","tool_use_id":"","content":""}`)
							partJSON, _ = sjson.SetBytes(partJSON, "tool_use_id", part.Get("tool_use_id").String())
							if c := part.Get("content"); c.Exists() {
								partJSON, _ = sjson.SetBytes(partJSON, "content", c.String())
							}
							oaiMsg, _ = sjson.SetRawBytes(oaiMsg, "content.-1", partJSON)
						case "thinking":
							// skip thinking blocks
						}
						return true
					})

					if len(toolCalls) > 0 {
						oaiMsg, _ = sjson.SetRawBytes(oaiMsg, "tool_calls", mustMarshal(toolCalls))
					}

					out, _ = sjson.SetRawBytes(out, "messages.-1", oaiMsg)
				}
			}
			return true
		})
	}

	// Tools
	if tools := root.Get("tools"); tools.Exists() && tools.IsArray() {
		var oaiTools []map[string]interface{}
		tools.ForEach(func(_, tool gjson.Result) bool {
			oaiTool := map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
					"name": tool.Get("name").String(),
				},
			}
			if desc := tool.Get("description"); desc.Exists() {
				oaiTool["function"].(map[string]interface{})["description"] = desc.String()
			}
			if schema := tool.Get("input_schema"); schema.Exists() {
				oaiTool["function"].(map[string]interface{})["parameters"] = json.RawMessage(schema.Raw)
			}
			oaiTools = append(oaiTools, oaiTool)
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
