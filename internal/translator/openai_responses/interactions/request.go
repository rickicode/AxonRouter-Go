package interactions

import (
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// ConvertOpenAIResponsesRequestToInteractions converts an OpenAI Responses API
// request into the Google Interactions API shape.
func ConvertOpenAIResponsesRequestToInteractions(modelName string, inputRawJSON []byte, stream bool) []byte {
	root := gjson.ParseBytes(inputRawJSON)

	out := []byte(`{"model":"","input":[]}`)
	out, _ = sjson.SetBytes(out, "model", firstNonEmpty(modelName, root.Get("model").String()))
	if stream || root.Get("stream").Bool() {
		out, _ = sjson.SetBytes(out, "stream", true)
	}

	if instructions := root.Get("instructions"); instructions.Exists() {
		out, _ = sjson.SetBytes(out, "system_instruction", responsesInstructionsText(instructions))
	}
	if prev := root.Get("previous_response_id"); prev.Exists() && prev.Type == gjson.String {
		out, _ = sjson.SetBytes(out, "previous_interaction_id", prev.String())
	}
	if input := root.Get("input"); input.Exists() {
		out = setResponsesInputOnInteractions(out, input)
	}

	out = appendResponsesToolsToInteractions(out, root.Get("tools"))
	if toolChoice := root.Get("tool_choice"); toolChoice.Exists() {
		out, _ = sjson.SetRawBytes(out, "generation_config.tool_choice", []byte(toolChoice.Raw))
	}
	if effort := root.Get("reasoning.effort"); effort.Exists() && effort.Type == gjson.String {
		out, _ = sjson.SetBytes(out, "generation_config.thinking_level", strings.ToLower(strings.TrimSpace(effort.String())))
	}
	if summary := root.Get("reasoning.summary"); summary.Exists() && summary.Type == gjson.String {
		out, _ = sjson.SetBytes(out, "generation_config.thinking_summaries", summary.String())
	}
	if format := root.Get("text.format"); format.Exists() {
		out, _ = sjson.SetRawBytes(out, "response_format", []byte(format.Raw))
	} else if format := root.Get("response_format"); format.Exists() {
		out, _ = sjson.SetRawBytes(out, "response_format", []byte(format.Raw))
	}

	return out
}

func responsesInstructionsText(instructions gjson.Result) string {
	if instructions.Type == gjson.String {
		return instructions.String()
	}
	if text := instructions.Get("text"); text.Exists() {
		return text.String()
	}
	if parts := instructions.Get("content"); parts.Exists() && parts.IsArray() {
		var builder strings.Builder
		parts.ForEach(func(_, part gjson.Result) bool {
			if t := part.Get("text").String(); t != "" {
				builder.WriteString(t)
			}
			return true
		})
		return builder.String()
	}
	return instructions.String()
}

func setResponsesInputOnInteractions(out []byte, input gjson.Result) []byte {
	functionNamesByCallID := make(map[string]string)
	var items [][]byte
	if input.Type == gjson.String {
		items = append(items, interactionsTextStep("user_input", input.String()))
	} else if input.IsArray() {
		input.ForEach(func(_, item gjson.Result) bool {
			if converted := responsesInputItemToInteractions(item, functionNamesByCallID); converted != nil {
				items = append(items, converted)
			}
			return true
		})
	} else if input.IsObject() {
		if converted := responsesInputItemToInteractions(input, functionNamesByCallID); converted != nil {
			items = append(items, converted)
		}
	}
	if len(items) > 0 {
		out, _ = sjson.SetRawBytes(out, "input", joinRaw(items))
	}
	return out
}

func responsesInputItemToInteractions(item gjson.Result, functionNamesByCallID map[string]string) []byte {
	switch item.Get("type").String() {
	case "message":
		stepType := "user_input"
		if role := item.Get("role").String(); role == "assistant" || role == "model" {
			stepType = "model_output"
		}
		step := []byte(`{"type":"","content":[]}`)
		step, _ = sjson.SetBytes(step, "type", stepType)
		return appendResponsesContentToInteractions(step, item.Get("content"))
	case "function_call":
		callID := firstNonEmpty(item.Get("call_id").String(), item.Get("id").String())
		if callID != "" {
			if name := item.Get("name").String(); name != "" {
				functionNamesByCallID[callID] = name
			}
		}
		return responsesFunctionCallToInteractions(item)
	case "function_call_output":
		return responsesFunctionOutputToInteractions(item, functionNamesByCallID)
	case "input_text", "output_text", "text":
		stepType := "user_input"
		if item.Get("type").String() == "output_text" {
			stepType = "model_output"
		}
		return interactionsTextStep(stepType, item.Get("text").String())
	case "input_image", "output_image":
		stepType := "user_input"
		if item.Get("type").String() == "output_image" {
			stepType = "model_output"
		}
		step := []byte(`{"type":"","content":[]}`)
		step, _ = sjson.SetBytes(step, "type", stepType)
		step, _ = sjson.SetRawBytes(step, "content.-1", responsesImagePartToInteractions(item))
		return step
	default:
		if content := item.Get("content"); content.Exists() {
			step := []byte(`{"type":"user_input","content":[]}`)
			return appendResponsesContentToInteractions(step, content)
		}
	}
	return nil
}

func appendResponsesContentToInteractions(step []byte, content gjson.Result) []byte {
	var contentItems [][]byte
	if content.Type == gjson.String {
		part := []byte(`{"type":"text","text":""}`)
		part, _ = sjson.SetBytes(part, "text", content.String())
		contentItems = append(contentItems, part)
	} else if content.IsArray() {
		content.ForEach(func(_, item gjson.Result) bool {
			if part, ok := responsesContentPartToInteractions(item); ok {
				contentItems = append(contentItems, part)
			}
			return true
		})
	} else if content.IsObject() {
		if part, ok := responsesContentPartToInteractions(content); ok {
			contentItems = append(contentItems, part)
		}
	}
	if len(contentItems) > 0 {
		step, _ = sjson.SetRawBytes(step, "content", joinRaw(contentItems))
	}
	return step
}

func responsesContentPartToInteractions(part gjson.Result) ([]byte, bool) {
	switch part.Get("type").String() {
	case "input_text", "output_text", "text":
		out := []byte(`{"type":"text","text":""}`)
		out, _ = sjson.SetBytes(out, "text", part.Get("text").String())
		return out, true
	case "input_image", "output_image":
		return responsesImagePartToInteractions(part), true
	}
	if text := part.Get("text"); text.Exists() {
		out := []byte(`{"type":"text","text":""}`)
		out, _ = sjson.SetBytes(out, "text", text.String())
		return out, true
	}
	return nil, false
}

func responsesImagePartToInteractions(part gjson.Result) []byte {
	out := []byte(`{"type":"image"}`)
	imageURL := firstNonEmpty(part.Get("image_url").String(), part.Get("url").String())
	if mimeType, data, ok := parseDataURL(imageURL); ok {
		out, _ = sjson.SetBytes(out, "mime_type", mimeType)
		out, _ = sjson.SetBytes(out, "data", data)
		return out
	}
	if data := part.Get("data").String(); data != "" {
		out, _ = sjson.SetBytes(out, "data", data)
		if mimeType := part.Get("mime_type").String(); mimeType != "" {
			out, _ = sjson.SetBytes(out, "mime_type", mimeType)
		}
		return out
	}
	if imageURL != "" {
		out, _ = sjson.SetBytes(out, "image_url", imageURL)
	}
	return out
}

func responsesFunctionCallToInteractions(item gjson.Result) []byte {
	out := []byte(`{"type":"function_call","name":"","arguments":{}}`)
	out, _ = sjson.SetBytes(out, "name", item.Get("name").String())
	if callID := firstNonEmpty(item.Get("call_id").String(), item.Get("id").String()); callID != "" {
		out, _ = sjson.SetBytes(out, "call_id", callID)
	}
	out = setRawJSON(out, "arguments", item.Get("arguments"), []byte(`{}`))
	return out
}

func responsesFunctionOutputToInteractions(item gjson.Result, functionNamesByCallID map[string]string) []byte {
	out := []byte(`{"type":"function_result","name":"","result":{}}`)
	callID := firstNonEmpty(item.Get("call_id").String(), item.Get("id").String())
	if name := item.Get("name").String(); name != "" {
		out, _ = sjson.SetBytes(out, "name", name)
	} else if name := functionNamesByCallID[callID]; name != "" {
		out, _ = sjson.SetBytes(out, "name", name)
	}
	if callID != "" {
		out, _ = sjson.SetBytes(out, "call_id", callID)
	}
	result := item.Get("output")
	if !result.Exists() {
		result = item.Get("result")
	}
	return setRawJSON(out, "result", result, []byte(`{}`))
}

func interactionsTextStep(stepType, text string) []byte {
	step := []byte(`{"type":"","content":[{"type":"text","text":""}]}`)
	step, _ = sjson.SetBytes(step, "type", stepType)
	step, _ = sjson.SetBytes(step, "content.0.text", text)
	return step
}

func appendResponsesToolsToInteractions(out []byte, tools gjson.Result) []byte {
	if !tools.Exists() || !tools.IsArray() {
		return out
	}
	var toolItems [][]byte
	tools.ForEach(func(_, tool gjson.Result) bool {
		switch tool.Get("type").String() {
		case "function", "":
			if converted, ok := functionToolToInteractions(tool); ok {
				toolItems = append(toolItems, converted)
			}
		case "namespace":
			var declarationItems [][]byte
			children := tool.Get("children")
			if !children.Exists() {
				children = tool.Get("tools")
			}
			children.ForEach(func(_, child gjson.Result) bool {
				if converted, ok := functionDeclarationFromTool(child); ok {
					declarationItems = append(declarationItems, converted)
				}
				return true
			})
			if len(declarationItems) > 0 {
				group := []byte(`{"function_declarations":[]}`)
				group, _ = sjson.SetRawBytes(group, "function_declarations", joinRaw(declarationItems))
				toolItems = append(toolItems, group)
			}
		}
		return true
	})
	if len(toolItems) > 0 {
		out, _ = sjson.SetRawBytes(out, "tools", joinRaw(toolItems))
	}
	return out
}

func functionToolToInteractions(tool gjson.Result) ([]byte, bool) {
	name := firstNonEmpty(tool.Get("name").String(), tool.Get("function.name").String())
	if name == "" {
		return nil, false
	}
	out := []byte(`{"type":"function","name":""}`)
	out, _ = sjson.SetBytes(out, "name", name)
	if desc := firstExisting(tool.Get("description"), tool.Get("function.description")); desc.Exists() {
		out, _ = sjson.SetBytes(out, "description", desc.String())
	}
	if params := firstExisting(tool.Get("parameters"), tool.Get("function.parameters")); params.Exists() {
		out, _ = sjson.SetRawBytes(out, "parameters", []byte(params.Raw))
	}
	return out, true
}

func functionDeclarationFromTool(tool gjson.Result) ([]byte, bool) {
	name := firstNonEmpty(tool.Get("name").String(), tool.Get("function.name").String())
	if name == "" {
		return nil, false
	}
	out := []byte(`{"name":""}`)
	out, _ = sjson.SetBytes(out, "name", name)
	if desc := firstExisting(tool.Get("description"), tool.Get("function.description")); desc.Exists() {
		out, _ = sjson.SetBytes(out, "description", desc.String())
	}
	if params := firstExisting(tool.Get("parameters"), tool.Get("function.parameters")); params.Exists() {
		out, _ = sjson.SetRawBytes(out, "parameters", []byte(params.Raw))
	}
	return out, true
}
