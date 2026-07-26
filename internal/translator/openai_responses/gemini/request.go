package gemini

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/translator/antigravity/openai"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// ConvertOpenAIResponsesRequestToGemini converts an OpenAI Responses API request
// into a Gemini generateContent request body.
func ConvertOpenAIResponsesRequestToGemini(modelName string, body []byte, stream bool) []byte {
	_ = stream
	root := gjson.ParseBytes(body)

	out := []byte(`{"contents":[]}`)
	out, _ = sjson.SetBytes(out, "model", modelName)

	// Generation config.
	if maxTokens := root.Get("max_output_tokens"); maxTokens.Exists() {
		out, _ = sjson.SetBytes(out, "generationConfig.maxOutputTokens", maxTokens.Int())
	} else if maxTokens := root.Get("max_tokens"); maxTokens.Exists() {
		out, _ = sjson.SetBytes(out, "generationConfig.maxOutputTokens", maxTokens.Int())
	}
	if temp := root.Get("temperature"); temp.Exists() {
		out, _ = sjson.SetBytes(out, "generationConfig.temperature", temp.Float())
	}
	if topP := root.Get("top_p"); topP.Exists() {
		out, _ = sjson.SetBytes(out, "generationConfig.topP", topP.Float())
	}

	// System instruction from instructions field.
	if sysText := extractInstructionsText(root); sysText != "" {
		out, _ = sjson.SetBytes(out, "systemInstruction.role", "user")
		out, _ = sjson.SetBytes(out, "systemInstruction.parts.0.text", sysText)
	}

	// Input items → contents.
	input := root.Get("input")
	switch {
	case input.Exists() && input.IsArray():
		input.ForEach(func(_, item gjson.Result) bool {
			out = appendResponsesInputItem(out, item, input)
			return true
		})
	case input.Exists() && input.Type == gjson.String:
		node := []byte(`{"role":"user","parts":[{"text":""}]}`)
		node, _ = sjson.SetBytes(node, "parts.0.text", input.String())
		out, _ = sjson.SetRawBytes(out, "contents.-1", node)
	}

	// Tools → functionDeclarations.
	if tools := root.Get("tools"); tools.Exists() && tools.IsArray() {
		var functionDeclarations []map[string]interface{}
		tools.ForEach(func(_, tool gjson.Result) bool {
			tType := tool.Get("type").String()
			if tType != "function" {
				return true
			}
			decl := map[string]interface{}{
				"name": openai.SanitizeFunctionName(tool.Get("name").String()),
			}
			if desc := tool.Get("description"); desc.Exists() {
				decl["description"] = desc.String()
			}
			if params := tool.Get("parameters"); params.Exists() {
				cleaned := executor.CleanJSONSchemaForGemini(params.Raw)
				decl["parameters"] = unmarshalJSON(cleaned)
			}
			if strict := tool.Get("strict"); strict.Exists() {
				decl["strict"] = strict.Bool()
			}
			functionDeclarations = append(functionDeclarations, decl)
			return true
		})
		if len(functionDeclarations) > 0 {
			b, _ := json.Marshal(functionDeclarations)
			out, _ = sjson.SetRawBytes(out, "tools.0.functionDeclarations", b)
		}
	}

	// text.format → responseMimeType/responseJsonSchema.
	if text := root.Get("text"); text.Exists() && text.IsObject() {
		if format := text.Get("format"); format.Exists() && format.IsObject() {
			formatType := format.Get("type").String()
			switch formatType {
			case "json_object":
				out, _ = sjson.SetBytes(out, "generationConfig.responseMimeType", "application/json")
			case "json_schema":
				out, _ = sjson.SetBytes(out, "generationConfig.responseMimeType", "application/json")
				if schema := format.Get("schema"); schema.Exists() {
					out, _ = sjson.SetRawBytes(out, "generationConfig.responseJsonSchema", []byte(schema.Raw))
				}
			}
		}
	}

	// reasoning.effort → thinkingConfig.
	if reasoning := root.Get("reasoning"); reasoning.Exists() && reasoning.IsObject() {
		if effort := reasoning.Get("effort"); effort.Exists() && effort.Type == gjson.String {
			out = applyResponsesReasoningEffort(out, effort.String())
		}
	}

	// Attach default safety settings.
	out = openai.AttachDefaultSafetySettings(out, "safetySettings")

	return out
}

func appendResponsesInputItem(out []byte, item, allInput gjson.Result) []byte {
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
			node, _ = sjson.SetBytes(node, partKey(p, "text"), content.String())
			p++
		} else if content.IsArray() {
			content.ForEach(func(_, part gjson.Result) bool {
				pType := part.Get("type").String()
				switch pType {
				case "input_text", "output_text":
					node, _ = sjson.SetBytes(node, partKey(p, "text"), part.Get("text").String())
					p++
				case "input_image":
					if url := part.Get("image_url"); url.Exists() {
						mime, data := parseInlineData(url.String())
						node, _ = sjson.SetBytes(node, partKey(p, "inlineData.mimeType"), mime)
						node, _ = sjson.SetBytes(node, partKey(p, "inlineData.data"), data)
						p++
					}
				case "input_audio":
					if dataVal := part.Get("data"); dataVal.Exists() {
						mime, data := parseInlineData(dataVal.String())
						if mime == "" {
							mime = audioMimeType(part.Get("format").String())
						}
						node, _ = sjson.SetBytes(node, partKey(p, "inlineData.mimeType"), mime)
						node, _ = sjson.SetBytes(node, partKey(p, "inlineData.data"), data)
						p++
					}
				case "input_file":
					if fileData := part.Get("file_data"); fileData.Exists() && fileData.String() != "" {
						mime, data := parseInlineData(fileData.String())
						if mime != "" && data != "" {
							node, _ = sjson.SetBytes(node, partKey(p, "inlineData.mimeType"), mime)
							node, _ = sjson.SetBytes(node, partKey(p, "inlineData.data"), data)
							p++
						} else {
							node, _ = sjson.SetBytes(node, partKey(p, "text"), fmt.Sprintf("[file data: %s]", fileData.String()))
							p++
						}
					} else if filename := part.Get("filename"); filename.Exists() && filename.String() != "" {
						node, _ = sjson.SetBytes(node, partKey(p, "text"), fmt.Sprintf("[file: %s]", filename.String()))
						p++
					}
				}
				return true
			})
		}

		// Drop empty model messages (e.g. assistant messages that only carry tool calls).
		if geminiRole == "model" && p == 0 {
			return out
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
			"name": openai.SanitizeFunctionName(name),
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
			"name": openai.SanitizeFunctionName(name),
			"response": map[string]interface{}{
				"result": output,
			},
		}))
		out, _ = sjson.SetRawBytes(out, "contents.-1", node)

	case "reasoning":
		// Reasoning history items become model content with thought=true parts.
		summary := item.Get("summary")
		var textParts []string
		if summary.IsArray() {
			summary.ForEach(func(_, s gjson.Result) bool {
				if t := s.Get("text"); t.Exists() {
					textParts = append(textParts, t.String())
				} else if s.Type == gjson.String {
					textParts = append(textParts, s.String())
				}
				return true
			})
		} else if summary.Type == gjson.String {
			textParts = append(textParts, summary.String())
		}
		if len(textParts) == 0 {
			return out
		}
		node := []byte(`{"role":"model","parts":[]}`)
		for i, t := range textParts {
			node, _ = sjson.SetBytes(node, partKey(i, "text"), t)
			node, _ = sjson.SetBytes(node, partKey(i, "thought"), true)
		}
		out, _ = sjson.SetRawBytes(out, "contents.-1", node)
	}

	return out
}

func extractInstructionsText(root gjson.Result) string {
	if instructions := root.Get("instructions"); instructions.Exists() {
		return textFromStringOrTextParts(instructions)
	}
	if system := root.Get("system"); system.Exists() {
		return textFromStringOrTextParts(system)
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

// parseInlineData parses a data URL (data:[<mime>][;base64],<data>) and returns
// the MIME type and data payload. Non-data URLs are returned with an empty MIME.
func parseInlineData(s string) (string, string) {
	s = strings.TrimSpace(s)
	const prefix = "data:"
	if !strings.HasPrefix(s, prefix) {
		return "", s
	}
	rest := s[len(prefix):]
	idx := strings.Index(rest, ",")
	if idx < 0 {
		return "", s
	}
	meta := rest[:idx]
	data := rest[idx+1:]

	mime := ""
	if p := strings.Index(meta, ";"); p >= 0 {
		mime = meta[:p]
	} else if meta != "" {
		mime = meta
	}
	return mime, data
}

func audioMimeType(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "mp3":
		return "audio/mpeg"
	case "wav":
		return "audio/wav"
	case "ogg":
		return "audio/ogg"
	case "flac":
		return "audio/flac"
	case "aac":
		return "audio/aac"
	case "webm":
		return "audio/webm"
	default:
		return "audio/wav"
	}
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

func applyResponsesReasoningEffort(out []byte, effort string) []byte {
	effort = strings.ToLower(strings.TrimSpace(effort))
	if effort == "" {
		return out
	}
	const path = "generationConfig.thinkingConfig"
	switch effort {
	case "none":
		out, _ = sjson.SetBytes(out, path+".includeThoughts", false)
	case "auto":
		out, _ = sjson.SetBytes(out, path+".thinkingBudget", -1)
		out, _ = sjson.SetBytes(out, path+".includeThoughts", true)
	default:
		out, _ = sjson.SetBytes(out, path+".thinkingLevel", effort)
		out, _ = sjson.SetBytes(out, path+".includeThoughts", true)
	}
	return out
}

func argsStringToMap(raw string) map[string]interface{} {
	if raw == "" {
		return map[string]interface{}{}
	}
	// OpenAI Responses passes function_call.arguments as a JSON string.
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
