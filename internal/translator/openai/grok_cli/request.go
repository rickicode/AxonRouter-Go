package grok_cli

import (
	"encoding/json"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// ConvertOpenAIRequestToGrokCLI translates an OpenAI Chat Completions request
// into the Grok CLI Responses API request shape.
//
// Differences from the generic OpenAI Responses translator:
//   - "system" messages keep role "system" (they are not converted to "developer").
//   - Tool declarations are flattened from Chat Completions' nested "function"
//     object to the Responses-style {type:"function", name, description, parameters}.
//   - Generation params are mapped: max_tokens -> max_output_tokens, with
//     temperature, top_p and reasoning_effort preserved as top-level fields.
func ConvertOpenAIRequestToGrokCLI(modelName string, body []byte, stream bool) []byte {
	root := gjson.ParseBytes(body)

	out := []byte(`{}`)
	out, _ = sjson.SetBytes(out, "model", modelName)
	out, _ = sjson.SetBytes(out, "stream", stream)
	out, _ = sjson.SetBytes(out, "store", false)

	// Convert messages to input items.
	if messages := root.Get("messages"); messages.Exists() && messages.IsArray() {
		var input []map[string]interface{}
		messages.ForEach(func(_, msg gjson.Result) bool {
			role := msg.Get("role").String()

			if role == "tool" {
				item := map[string]interface{}{
					"type":    "function_call_output",
					"call_id": msg.Get("tool_call_id").String(),
				}
				if content := msg.Get("content"); content.Type == gjson.String {
					item["output"] = content.String()
				} else if content.Exists() {
					item["output"] = parseJSON(content.Raw)
				}
				input = append(input, item)
				return true
			}

			item := map[string]interface{}{
				"type": "message",
				"role": role,
			}
			outputRole := role == "assistant"

			content := msg.Get("content")
			var parts []map[string]interface{}

			if content.Type == gjson.String && content.String() != "" {
				partType := "input_text"
				if outputRole {
					partType = "output_text"
				}
				parts = append(parts, map[string]interface{}{
					"type": partType,
					"text": content.String(),
				})
			} else if content.Exists() && content.IsArray() {
				content.ForEach(func(_, part gjson.Result) bool {
					pType := part.Get("type").String()
					switch pType {
					case "text":
						pt := "input_text"
						if outputRole {
							pt = "output_text"
						}
						parts = append(parts, map[string]interface{}{
							"type": pt,
							"text": part.Get("text").String(),
						})
					case "image_url":
						if !outputRole {
							parts = append(parts, map[string]interface{}{
								"type":      "input_image",
								"image_url": part.Get("image_url.url").String(),
							})
						}
					case "input_audio":
						if !outputRole {
							parts = append(parts, map[string]interface{}{
								"type":   "input_audio",
								"data":   part.Get("input_audio.data").String(),
								"format": part.Get("input_audio.format").String(),
							})
						}
					case "file":
						if !outputRole {
							p := map[string]interface{}{
								"type": "input_file",
							}
							if v := part.Get("file.file_data").String(); v != "" {
								p["file_data"] = v
							}
							if v := part.Get("file.file_id").String(); v != "" {
								p["file_id"] = v
							}
							if v := part.Get("file.filename").String(); v != "" {
								p["filename"] = v
							}
							parts = append(parts, p)
						}
					}
					return true
				})
			}

			if len(parts) > 0 {
				item["content"] = parts
				input = append(input, item)
			}

			// Emit assistant tool calls as top-level function_call items.
			if role == "assistant" {
				toolCalls := msg.Get("tool_calls")
				if toolCalls.Exists() && toolCalls.IsArray() {
					toolCalls.ForEach(func(_, tc gjson.Result) bool {
						if tc.Get("type").String() != "function" {
							return true
						}
						input = append(input, map[string]interface{}{
							"type":      "function_call",
							"call_id":   tc.Get("id").String(),
							"name":      tc.Get("function.name").String(),
							"arguments": tc.Get("function.arguments").String(),
						})
						return true
					})
				}
			}

			return true
		})
		out, _ = sjson.SetRawBytes(out, "input", mustMarshal(input))
	}

	// Generation params.
	if v := root.Get("max_tokens"); v.Exists() {
		out, _ = sjson.SetBytes(out, "max_output_tokens", v.Int())
	}
	if v := root.Get("temperature"); v.Exists() {
		out, _ = sjson.SetBytes(out, "temperature", v.Float())
	}
	if v := root.Get("top_p"); v.Exists() {
		out, _ = sjson.SetBytes(out, "top_p", v.Float())
	}
	if v := root.Get("reasoning_effort"); v.Exists() {
		out, _ = sjson.SetBytes(out, "reasoning_effort", v.Value())
	}

	// Flatten tools from Chat Completions to Responses style.
	if tools := root.Get("tools"); tools.Exists() && tools.IsArray() {
		var flat []map[string]interface{}
		tools.ForEach(func(_, tool gjson.Result) bool {
			toolType := tool.Get("type").String()
			if toolType != "" && toolType != "function" && tool.IsObject() {
				// Pass through built-in / hosted tools unchanged.
				flat = append(flat, parseJSON(tool.Raw).(map[string]interface{}))
				return true
			}
			if toolType == "function" {
				item := map[string]interface{}{
					"type": "function",
				}
				fn := tool.Get("function")
				if fn.Exists() {
					if v := fn.Get("name"); v.Exists() {
						item["name"] = v.String()
					}
					if v := fn.Get("description"); v.Exists() {
						item["description"] = v.Value()
					}
					if v := fn.Get("parameters"); v.Exists() {
						item["parameters"] = parseJSON(v.Raw)
					}
					if v := fn.Get("strict"); v.Exists() {
						item["strict"] = v.Value()
					}
				}
				flat = append(flat, item)
			}
			return true
		})
		if len(flat) > 0 {
			out, _ = sjson.SetRawBytes(out, "tools", mustMarshal(flat))
		}
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
