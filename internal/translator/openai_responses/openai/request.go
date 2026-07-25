package openai

import (
	"encoding/json"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// convertResponsesRequestToOpenAI converts an OpenAI Responses API request
// into an OpenAI Chat Completions request.
func convertResponsesRequestToOpenAI(modelName string, body []byte, stream bool) []byte {
	root := gjson.ParseBytes(body)

	out := []byte(`{"model":"","messages":[]}`)
	out, _ = sjson.SetBytes(out, "model", modelName)
	out, _ = sjson.SetBytes(out, "stream", stream)

	var messages []map[string]interface{}

	if instructions := extractInstructions(root); instructions != "" {
		messages = append(messages, map[string]interface{}{"role": "system", "content": instructions})
	}

	if input := root.Get("input"); input.Exists() {
		switch input.Type {
		case gjson.String:
			if text := strings.TrimSpace(input.String()); text != "" {
				messages = append(messages, map[string]interface{}{"role": "user", "content": text})
			}
		case gjson.JSON:
			if input.IsArray() {
				input.ForEach(func(_, item gjson.Result) bool {
					messages = append(messages, convertInputItem(item)...)
					return true
				})
			}
		}
	}

	if len(messages) > 0 {
		raw, _ := json.Marshal(messages)
		out, _ = sjson.SetRawBytes(out, "messages", raw)
	}

	if v := root.Get("max_output_tokens"); v.Exists() {
		out, _ = sjson.SetBytes(out, "max_tokens", v.Int())
	} else if v := root.Get("max_tokens"); v.Exists() {
		out, _ = sjson.SetBytes(out, "max_tokens", v.Int())
	}
	if v := root.Get("temperature"); v.Exists() && v.Type == gjson.Number {
		out, _ = sjson.SetBytes(out, "temperature", v.Float())
	}
	if v := root.Get("top_p"); v.Exists() && v.Type == gjson.Number {
		out, _ = sjson.SetBytes(out, "top_p", v.Float())
	}
	if v := root.Get("reasoning.effort"); v.Exists() && v.Type == gjson.String {
		out, _ = sjson.SetBytes(out, "reasoning_effort", v.String())
	}
	if v := root.Get("reasoning.generate_summary"); v.Exists() {
		out, _ = sjson.SetBytes(out, "reasoning.generate_summary", v.Bool())
	}

	if tools := root.Get("tools"); tools.Exists() && tools.IsArray() {
		var chatTools []map[string]interface{}
		tools.ForEach(func(_, tool gjson.Result) bool {
			chatTools = append(chatTools, jsonRawToMap(tool.Raw))
			return true
		})
		if len(chatTools) > 0 {
			raw, _ := json.Marshal(chatTools)
			out, _ = sjson.SetRawBytes(out, "tools", raw)
		}
	}

	if tc := root.Get("tool_choice"); tc.Exists() {
		switch tc.Type {
		case gjson.String:
			out, _ = sjson.SetBytes(out, "tool_choice", tc.String())
		default:
			out, _ = sjson.SetRawBytes(out, "tool_choice", []byte(tc.Raw))
		}
	}

	if textFmt := root.Get("text.format"); textFmt.Exists() {
		out, _ = sjson.SetRawBytes(out, "response_format", []byte(textFmt.Raw))
	}

	if stream {
		out, _ = sjson.SetBytes(out, "stream_options.include_usage", true)
	}

	return out
}

func extractInstructions(root gjson.Result) string {
	if instr := root.Get("instructions"); instr.Exists() {
		return textFromResult(instr)
	}
	return ""
}

func textFromResult(v gjson.Result) string {
	switch v.Type {
	case gjson.String:
		return v.String()
	case gjson.JSON:
		if v.IsArray() {
			var parts []string
			v.ForEach(func(_, elem gjson.Result) bool {
				if elem.Type == gjson.String {
					parts = append(parts, elem.String())
					return true
				}
				if t := elem.Get("text"); t.Exists() {
					parts = append(parts, t.String())
				}
				return true
			})
			return strings.Join(parts, "\n")
		}
	}
	return ""
}

func convertInputItem(item gjson.Result) []map[string]interface{} {
	typ := item.Get("type").String()
	if typ == "" && item.Get("role").Exists() {
		typ = "message"
	}

	switch typ {
	case "message":
		return convertMessageItem(item)
	case "function_call":
		return convertFunctionCallItem(item)
	case "function_call_output":
		return convertFunctionCallOutputItem(item)
	case "reasoning":
		return convertReasoningItem(item)
	}
	return nil
}

func convertMessageItem(item gjson.Result) []map[string]interface{} {
	role := item.Get("role").String()
	switch role {
	case "system", "developer":
		role = "system"
	case "assistant", "model":
		role = "assistant"
	case "tool":
		role = "tool"
	default:
		role = "user"
	}

	content := item.Get("content")
	var msg map[string]interface{}

	switch content.Type {
	case gjson.String:
		msg = map[string]interface{}{
			"role":    role,
			"content": content.String(),
		}
	case gjson.JSON:
		if content.IsArray() {
			chatContent := convertContentParts(content)
			if len(chatContent) == 1 {
				if text, ok := chatContent[0]["text"].(string); ok {
					msg = map[string]interface{}{
						"role":    role,
						"content": text,
					}
					break
				}
			}
			msg = map[string]interface{}{
				"role":    role,
				"content": chatContent,
			}
		} else {
			msg = map[string]interface{}{
				"role":    role,
				"content": textFromResult(content),
			}
		}
	default:
		msg = map[string]interface{}{
			"role":    role,
			"content": "",
		}
	}

	if name := item.Get("name").String(); name != "" && role == "tool" {
		msg["name"] = name
	}

	return []map[string]interface{}{msg}
}

func convertContentParts(parts gjson.Result) []map[string]interface{} {
	var out []map[string]interface{}
	parts.ForEach(func(_, part gjson.Result) bool {
		ptype := part.Get("type").String()
		switch ptype {
		case "input_text", "output_text":
			out = append(out, map[string]interface{}{
				"type": "text",
				"text": part.Get("text").String(),
			})
		case "input_image":
			url := part.Get("image_url").String()
			if url == "" {
				url = part.Get("url").String()
			}
			if url != "" {
				out = append(out, map[string]interface{}{
					"type": "image_url",
					"image_url": map[string]interface{}{
						"url": url,
					},
				})
			}
		case "input_file":
			if fileData := part.Get("file_data").String(); fileData != "" {
				out = append(out, map[string]interface{}{
					"type": "text",
					"text": fileData,
				})
			}
		}
		return true
	})
	return out
}

func convertFunctionCallItem(item gjson.Result) []map[string]interface{} {
	callID := item.Get("call_id").String()
	if callID == "" {
		callID = item.Get("id").String()
	}
	return []map[string]interface{}{
		{
			"role": "assistant",
			"tool_calls": []map[string]interface{}{
				{
					"id":   callID,
					"type": "function",
					"function": map[string]interface{}{
						"name":      item.Get("name").String(),
						"arguments": item.Get("arguments").String(),
					},
				},
			},
		},
	}
}

func convertFunctionCallOutputItem(item gjson.Result) []map[string]interface{} {
	callID := item.Get("call_id").String()
	if callID == "" {
		callID = item.Get("id").String()
	}
	return []map[string]interface{}{
		{
			"role":         "tool",
			"tool_call_id": callID,
			"content":      item.Get("output").String(),
		},
	}
}

func convertReasoningItem(item gjson.Result) []map[string]interface{} {
	if summary := item.Get("summary"); summary.Exists() {
		text := textFromResult(summary)
		if text != "" {
			return []map[string]interface{}{
				{
					"role":    "assistant",
					"content": text,
				},
			}
		}
	}
	return nil
}

func jsonRawToMap(raw string) map[string]interface{} {
	var out map[string]interface{}
	json.Unmarshal([]byte(raw), &out)
	return out
}
