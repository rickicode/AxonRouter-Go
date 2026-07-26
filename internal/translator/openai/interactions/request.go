package interactions

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// convertOpenAIRequestToInteractions converts an OpenAI Chat Completions request
// to the Google Interactions API shape.
func convertOpenAIRequestToInteractions(modelName string, body []byte, stream bool) []byte {
	root := gjson.ParseBytes(body)

	out := []byte(`{"model":"","input":[]}`)
	out, _ = sjson.SetBytes(out, "model", firstNonEmpty(modelName, root.Get("model").String()))
	if stream || root.Get("stream").Bool() {
		out, _ = sjson.SetBytes(out, "stream", true)
	}

	if sys := extractOpenAISystemInstruction(root); sys != "" {
		out, _ = sjson.SetBytes(out, "system_instruction", sys)
	}
	if messages := root.Get("messages"); messages.Exists() && messages.IsArray() {
		out = appendOpenAIMessagesToInteractions(out, messages)
	}

	out = copyOpenAIToolsToInteractions(out, root.Get("tools"))
	out = copyOpenAIGenerationConfigToInteractions(out, root)

	return out
}

// convertInteractionsRequestToOpenAI converts an Interactions request to OpenAI
// Chat Completions format.
func convertInteractionsRequestToOpenAI(modelName string, body []byte, stream bool) []byte {
	root := gjson.ParseBytes(body)

	out := []byte(`{"model":"","messages":[]}`)
	out, _ = sjson.SetBytes(out, "model", firstNonEmpty(modelName, root.Get("model").String()))
	if stream || root.Get("stream").Bool() {
		out, _ = sjson.SetBytes(out, "stream", true)
	}

	if sys := interactionsSystemInstructionText(root); sys != "" {
		msg := []byte(`{"role":"system","content":""}`)
		msg, _ = sjson.SetBytes(msg, "content", sys)
		out, _ = sjson.SetRawBytes(out, "messages.-1", msg)
	}
	if input := root.Get("input"); input.Exists() {
		out = appendInteractionsInputToOpenAI(out, input)
	}

	out = copyInteractionsToolsToOpenAI(out, root.Get("tools"))
	out = copyInteractionsGenerationConfigToOpenAI(out, root)

	return out
}

func extractOpenAISystemInstruction(root gjson.Result) string {
	msgs := root.Get("messages")
	if !msgs.Exists() || !msgs.IsArray() {
		return ""
	}
	var parts []string
	msgs.ForEach(func(_, msg gjson.Result) bool {
		if msg.Get("role").String() != "system" {
			return true
		}
		if content := msg.Get("content"); content.Type == gjson.String {
			parts = append(parts, content.String())
		} else if content.IsArray() {
			content.ForEach(func(_, part gjson.Result) bool {
				if part.Get("type").String() == "text" {
					parts = append(parts, part.Get("text").String())
				}
				return true
			})
		}
		return true
	})
	return strings.Join(parts, "\n")
}

func appendOpenAIMessagesToInteractions(out []byte, messages gjson.Result) []byte {
	var items [][]byte
	messages.ForEach(func(_, msg gjson.Result) bool {
		role := msg.Get("role").String()
		if role == "system" {
			return true
		}
		switch role {
		case "user":
			items = append(items, openAIMessageToInteractionsStep(msg, "user_input"))
		case "assistant":
			items = append(items, openAIAssistantMessageToInteractionsStep(msg))
		case "tool":
			items = append(items, openAIToolResultToInteractionsStep(msg))
		}
		return true
	})
	if len(items) > 0 {
		out, _ = sjson.SetRawBytes(out, "input", joinRaw(items))
	}
	return out
}

func openAIMessageToInteractionsStep(msg gjson.Result, stepType string) []byte {
	step := []byte(`{"type":""}`)
	step, _ = sjson.SetBytes(step, "type", stepType)
	content := msg.Get("content")
	if content.Type == gjson.String {
		step, _ = sjson.SetRawBytes(step, "content", []byte(fmt.Sprintf(`[{"type":"text","text":%s}]`, jsonString(content))))
	} else if content.IsArray() {
		var parts []map[string]interface{}
		content.ForEach(func(_, part gjson.Result) bool {
			if p, ok := openAIContentPartToInteractions(part); ok {
				parts = append(parts, p)
			}
			return true
		})
		if len(parts) > 0 {
			step, _ = sjson.SetRawBytes(step, "content", mustMarshal(parts))
		}
	}
	return step
}

func openAIAssistantMessageToInteractionsStep(msg gjson.Result) []byte {
	if toolCalls := msg.Get("tool_calls"); toolCalls.Exists() && toolCalls.IsArray() {
		var items [][]byte
		toolCalls.ForEach(func(_, tc gjson.Result) bool {
			items = append(items, openAIToolCallToInteractionsStep(tc))
			return true
		})
		if content := msg.Get("content"); content.Exists() && content.String() != "" {
			items = append([][]byte{openAIMessageToInteractionsStep(msg, "model_output")}, items...)
		}
		return joinSlices(items)
	}
	return openAIMessageToInteractionsStep(msg, "model_output")
}

func openAIToolCallToInteractionsStep(tc gjson.Result) []byte {
	step := []byte(`{"type":"function_call","name":"","arguments":{}}`)
	step, _ = sjson.SetBytes(step, "name", tc.Get("function.name").String())
	if id := tc.Get("id").String(); id != "" {
		step, _ = sjson.SetBytes(step, "id", id)
		step, _ = sjson.SetBytes(step, "call_id", id)
	}
	step = setRawJSON(step, "arguments", tc.Get("function.arguments"), []byte("{}"))
	return step
}

func openAIToolResultToInteractionsStep(msg gjson.Result) []byte {
	step := []byte(`{"type":"function_result","name":"","result":""}`)
	step, _ = sjson.SetBytes(step, "name", msg.Get("name").String())
	if id := msg.Get("tool_call_id").String(); id != "" {
		step, _ = sjson.SetBytes(step, "call_id", id)
	}
	step, _ = sjson.SetBytes(step, "result", jsonStringValue(msg.Get("content"), ""))
	return step
}

func openAIContentPartToInteractions(part gjson.Result) (map[string]interface{}, bool) {
	switch part.Get("type").String() {
	case "text":
		return map[string]interface{}{"type": "text", "text": part.Get("text").String()}, true
	case "image_url":
		return map[string]interface{}{"type": "image", "image_url": part.Get("image_url.url").String()}, true
	}
	return nil, false
}

func copyOpenAIToolsToInteractions(out []byte, tools gjson.Result) []byte {
	if !tools.Exists() || !tools.IsArray() {
		return out
	}
	var items []map[string]interface{}
	tools.ForEach(func(_, tool gjson.Result) bool {
		name := firstNonEmpty(tool.Get("function.name").String(), tool.Get("name").String())
		if name == "" {
			return true
		}
		decl := map[string]interface{}{"name": name}
		if desc := firstExisting(tool.Get("function.description"), tool.Get("description")); desc.Exists() {
			decl["description"] = desc.String()
		}
		if params := firstExisting(tool.Get("function.parameters"), tool.Get("parameters")); params.Exists() {
			decl["parameters"] = json.RawMessage(params.Raw)
		}
		items = append(items, map[string]interface{}{"function_declarations": []map[string]interface{}{decl}})
		return true
	})
	if len(items) > 0 {
		out, _ = sjson.SetRawBytes(out, "tools", mustMarshal(items))
	}
	return out
}

func copyOpenAIGenerationConfigToInteractions(out []byte, root gjson.Result) []byte {
	if temp := root.Get("temperature"); temp.Exists() {
		out, _ = sjson.SetBytes(out, "generation_config.temperature", temp.Float())
	}
	if maxTokens := root.Get("max_tokens"); maxTokens.Exists() {
		out, _ = sjson.SetBytes(out, "generation_config.max_output_tokens", maxTokens.Int())
	}
	if topP := root.Get("top_p"); topP.Exists() {
		out, _ = sjson.SetBytes(out, "generation_config.top_p", topP.Float())
	}
	if stop := root.Get("stop"); stop.Exists() && stop.IsArray() {
		out, _ = sjson.SetRawBytes(out, "generation_config.stop_sequences", []byte(stop.Raw))
	}
	return out
}

// --- Interactions -> OpenAI helpers ---

func interactionsSystemInstructionText(root gjson.Result) string {
	sys := root.Get("system_instruction")
	if !sys.Exists() {
		return ""
	}
	if sys.Type == gjson.String {
		return sys.String()
	}
	if text := sys.Get("text"); text.Exists() {
		return text.String()
	}
	if parts := sys.Get("parts"); parts.Exists() && parts.IsArray() {
		var texts []string
		parts.ForEach(func(_, part gjson.Result) bool {
			if t := part.Get("text"); t.Exists() {
				texts = append(texts, t.String())
			}
			return true
		})
		return strings.Join(texts, "\n")
	}
	return ""
}

func appendInteractionsInputToOpenAI(out []byte, input gjson.Result) []byte {
	var messages [][]byte
	if input.Type == gjson.String {
		messages = append(messages, openAITextMessage(input.String(), "user"))
	} else if input.IsArray() {
		input.ForEach(func(_, item gjson.Result) bool {
			if msg := interactionsInputItemToOpenAIMessage(item); msg != nil {
				messages = append(messages, msg)
			}
			return true
		})
	} else if input.IsObject() {
		if msg := interactionsInputItemToOpenAIMessage(input); msg != nil {
			messages = append(messages, msg)
		}
	}
	if len(messages) > 0 {
		out, _ = sjson.SetRawBytes(out, "messages", joinRaw(messages))
	}
	return out
}

func interactionsInputItemToOpenAIMessage(item gjson.Result) []byte {
	switch item.Get("type").String() {
	case "user_input":
		return interactionsMessageToOpenAIMessage(item, "user")
	case "model_output":
		return interactionsMessageToOpenAIMessage(item, "assistant")
	case "function_call":
		return interactionsFunctionCallToOpenAIMessage(item)
	case "function_result":
		return interactionsFunctionResultToOpenAIMessage(item)
	case "thought":
		return interactionsThoughtToOpenAIMessage(item)
	}
	if item.Type == gjson.String {
		return openAITextMessage(item.String(), "user")
	}
	return nil
}

func interactionsMessageToOpenAIMessage(item gjson.Result, role string) []byte {
	msg := []byte(`{"role":"","content":""}`)
	msg, _ = sjson.SetBytes(msg, "role", role)

	content := item.Get("content")
	if content.Type == gjson.String {
		msg, _ = sjson.SetBytes(msg, "content", content.String())
	} else if content.IsArray() {
		var texts []string
		content.ForEach(func(_, part gjson.Result) bool {
			if t := part.Get("text"); t.Exists() {
				texts = append(texts, t.String())
			}
			return true
		})
		if len(texts) > 0 {
			msg, _ = sjson.SetBytes(msg, "content", strings.Join(texts, ""))
		}
	}
	return msg
}

func interactionsFunctionCallToOpenAIMessage(item gjson.Result) []byte {
	callID := firstNonEmpty(item.Get("call_id").String(), item.Get("id").String(), "call_0")
	args := item.Get("arguments").Raw
	if args == "" {
		args = "{}"
	}
	toolCall := map[string]interface{}{
		"id":   callID,
		"type": "function",
		"function": map[string]interface{}{
			"name":      item.Get("name").String(),
			"arguments": args,
		},
	}
	return mustMarshal(map[string]interface{}{
		"role":       "assistant",
		"content":    "",
		"tool_calls": []map[string]interface{}{toolCall},
	})
}

func interactionsFunctionResultToOpenAIMessage(item gjson.Result) []byte {
	callID := firstNonEmpty(item.Get("call_id").String(), item.Get("id").String())
	msg := []byte(`{"role":"tool","tool_call_id":"","content":""}`)
	msg, _ = sjson.SetBytes(msg, "tool_call_id", callID)
	msg, _ = sjson.SetBytes(msg, "content", jsonStringValue(item.Get("result"), ""))
	return msg
}

func interactionsThoughtToOpenAIMessage(item gjson.Result) []byte {
	content := item.Get("content")
	var texts []string
	if content.Type == gjson.String {
		texts = []string{content.String()}
	} else if content.IsArray() {
		content.ForEach(func(_, part gjson.Result) bool {
			if t := part.Get("text"); t.Exists() {
				texts = append(texts, t.String())
			}
			return true
		})
	}
	return mustMarshal(map[string]interface{}{
		"role":              "assistant",
		"content":           "",
		"reasoning_content": strings.Join(texts, ""),
	})
}

func copyInteractionsToolsToOpenAI(out []byte, tools gjson.Result) []byte {
	if !tools.Exists() || !tools.IsArray() {
		return out
	}
	var items []map[string]interface{}
	tools.ForEach(func(_, tool gjson.Result) bool {
		decls := tool.Get("function_declarations")
		if !decls.Exists() || !decls.IsArray() {
			decls = tool.Get("functionDeclarations")
		}
		if !decls.Exists() || !decls.IsArray() {
			return true
		}
		decls.ForEach(func(_, decl gjson.Result) bool {
			name := firstNonEmpty(decl.Get("name").String(), decl.Get("function.name").String())
			if name == "" {
				return true
			}
			fn := map[string]interface{}{"name": name}
			if desc := firstExisting(decl.Get("description"), decl.Get("function.description")); desc.Exists() {
				fn["description"] = desc.String()
			}
			if params := firstExisting(decl.Get("parameters"), decl.Get("function.parameters")); params.Exists() {
				fn["parameters"] = json.RawMessage(params.Raw)
			}
			items = append(items, map[string]interface{}{
				"type":     "function",
				"function": fn,
			})
			return true
		})
		return true
	})
	if len(items) > 0 {
		out, _ = sjson.SetRawBytes(out, "tools", mustMarshal(items))
	}
	return out
}

func copyInteractionsGenerationConfigToOpenAI(out []byte, root gjson.Result) []byte {
	gen := root.Get("generation_config")
	if !gen.Exists() {
		gen = root.Get("generationConfig")
	}
	if temp := gen.Get("temperature"); temp.Exists() {
		out, _ = sjson.SetBytes(out, "temperature", temp.Float())
	}
	if maxTokens := firstExisting(gen.Get("max_output_tokens"), gen.Get("maxOutputTokens")); maxTokens.Exists() {
		out, _ = sjson.SetBytes(out, "max_tokens", maxTokens.Int())
	}
	if topP := firstExisting(gen.Get("top_p"), gen.Get("topP")); topP.Exists() {
		out, _ = sjson.SetBytes(out, "top_p", topP.Float())
	}
	if stop := firstExisting(gen.Get("stop_sequences"), gen.Get("stopSequences")); stop.Exists() && stop.IsArray() {
		out, _ = sjson.SetRawBytes(out, "stop", []byte(stop.Raw))
	}
	return out
}
