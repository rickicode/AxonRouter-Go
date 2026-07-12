package gemini

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// convertCodexRequestToGemini transforms a Codex Responses API request to Gemini generateContent format.
func convertCodexRequestToGemini(modelName string, body []byte, stream bool) []byte {
	_ = stream
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

	// System instruction: prefer Codex "instructions", fallback to top-level "system".
	if sysText := extractSystemText(root); sysText != "" {
		out, _ = sjson.SetBytes(out, "systemInstruction.role", "user")
		out, _ = sjson.SetBytes(out, "systemInstruction.parts.0.text", sysText)
	}

	// Input messages (Codex Responses format uses "input" array; coerce a string to a single user message).
	input := root.Get("input")
	switch {
	case input.Exists() && input.IsArray():
		input.ForEach(func(_, item gjson.Result) bool {
			out = appendCodexInputItem(out, item, input)
			return true
		})
	case input.Exists() && input.Type == gjson.String:
		node := []byte(`{"role":"user","parts":[{"text":""}]}`)
		node, _ = sjson.SetBytes(node, "parts.0.text", input.String())
		out, _ = sjson.SetRawBytes(out, "contents.-1", node)
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
					decl["parameters"] = unmarshalJSON(params.Raw)
				}
				if strict := tool.Get("strict"); strict.Exists() {
					decl["strict"] = strict.Bool()
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

func appendCodexInputItem(out []byte, item, allInput gjson.Result) []byte {
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
					node, _ = sjson.SetBytes(node, partKey(p, "text"), part.Get("text").String())
					p++
				case "output_text":
					node, _ = sjson.SetBytes(node, partKey(p, "text"), part.Get("text").String())
					p++
				case "input_image":
					if url := part.Get("image_url"); url.Exists() {
						mime, data := parseInlineImage(url.String())
						node, _ = sjson.SetBytes(node, partKey(p, "inlineData.mimeType"), mime)
						node, _ = sjson.SetBytes(node, partKey(p, "inlineData.data"), data)
						p++
					}
				case "input_file":
					if filename := part.Get("filename"); filename.Exists() {
						node, _ = sjson.SetBytes(node, partKey(p, "text"), fmt.Sprintf("[file: %s]", filename.String()))
						p++
					} else if fileData := part.Get("file_data"); fileData.Exists() {
						node, _ = sjson.SetBytes(node, partKey(p, "text"), fmt.Sprintf("[file data: %s]", fileData.String()))
						p++
					}
				case "input_audio":
					if data := part.Get("data"); data.Exists() {
						node, _ = sjson.SetBytes(node, partKey(p, "inlineData.mimeType"), "audio/wav")
						node, _ = sjson.SetBytes(node, partKey(p, "inlineData.data"), data.String())
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
		argsMap := argsStringToMap(args)

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
		name := findFunctionNameByCallID(allInput, callID)
		if name == "" {
			name = "function"
		}

		node := []byte(`{"role":"user","parts":[]}`)
		node, _ = sjson.SetRawBytes(node, "parts.0.functionResponse", mustMarshal(map[string]interface{}{
			"id":   callID,
			"name": name,
			"response": map[string]interface{}{
				"result": output,
			},
		}))
		out, _ = sjson.SetRawBytes(out, "contents.-1", node)
	}

	return out
}

func extractSystemText(root gjson.Result) string {
	if sys := root.Get("instructions"); sys.Exists() {
		return textFromStringOrTextParts(sys)
	}
	if sys := root.Get("system"); sys.Exists() {
		return textFromStringOrTextParts(sys)
	}
	return ""
}

func textFromStringOrTextParts(v gjson.Result) string {
	if v.Type == gjson.String {
		return v.String()
	}
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
	return ""
}

func parseInlineImage(s string) (string, string) {
	s = strings.TrimSpace(s)
	const prefix = "data:"
	if !strings.HasPrefix(s, prefix) {
		return "image/jpeg", s
	}
	rest := s[len(prefix):]
	idx := strings.Index(rest, ",")
	if idx < 0 {
		return "image/jpeg", s
	}
	meta := rest[:idx]
	data := rest[idx+1:]

	mime := "image/jpeg"
	if p := strings.Index(meta, ";"); p >= 0 {
		mime = meta[:p]
	} else if meta != "" {
		mime = meta
	}
	return mime, data
}

func findFunctionNameByCallID(input gjson.Result, callID string) string {
	if !input.IsArray() {
		return ""
	}
	var name string
	input.ForEach(func(_, item gjson.Result) bool {
		if item.Get("type").String() == "function_call" && item.Get("call_id").String() == callID {
			name = item.Get("name").String()
			return false
		}
		return true
	})
	return name
}

func argsStringToMap(raw string) map[string]interface{} {
	if raw == "" {
		return map[string]interface{}{}
	}

	// Codex Responses passes function_call.arguments as a JSON string.
	var s string
	if err := json.Unmarshal([]byte(raw), &s); err == nil {
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(s), &m); err == nil {
			return m
		}
		return map[string]interface{}{}
	}

	var m map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &m); err == nil {
		return m
	}
	return map[string]interface{}{}
}

func partKey(index int, field string) string {
	return fmt.Sprintf("parts.%d.%s", index, field)
}

func unmarshalJSON(raw string) interface{} {
	if raw == "" {
		return nil
	}
	var v interface{}
	_ = json.Unmarshal([]byte(raw), &v)
	return v
}

func mustMarshal(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}
