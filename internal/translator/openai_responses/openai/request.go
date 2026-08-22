package openai

import (
	"encoding/json"
	"strings"

	"github.com/rickicode/AxonRouter-Go/internal/headroom"
	"github.com/rickicode/AxonRouter-Go/internal/translator/common"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// ConvertOpenAIResponsesRequestToOpenAI transforms an OpenAI Responses API
// request into an OpenAI Chat Completions request body.
func ConvertOpenAIResponsesRequestToOpenAI(modelName string, inputRawJSON []byte, stream bool) []byte {
	rawJSON := common.CompressToolBlocks(inputRawJSON, headroom.GlobalToolCompressor(), headroom.DefaultToolThreshold)

	out := []byte(`{"model":"","messages":[],"stream":false}`)

	root := gjson.ParseBytes(rawJSON)

	out, _ = sjson.SetBytes(out, "model", modelName)
	out, _ = sjson.SetBytes(out, "stream", stream)

	// Generation parameters.
	if maxTokens := root.Get("max_output_tokens"); maxTokens.Exists() {
		out, _ = sjson.SetBytes(out, "max_tokens", maxTokens.Int())
	} else if maxTokens := root.Get("max_tokens"); maxTokens.Exists() {
		out, _ = sjson.SetBytes(out, "max_tokens", maxTokens.Int())
	}
	if temperature := root.Get("temperature"); temperature.Exists() && temperature.Type == gjson.Number {
		out, _ = sjson.SetBytes(out, "temperature", temperature.Float())
	}
	if topP := root.Get("top_p"); topP.Exists() && topP.Type == gjson.Number {
		out, _ = sjson.SetBytes(out, "top_p", topP.Float())
	}
	if reasoningEffort := root.Get("reasoning.effort"); reasoningEffort.Exists() && reasoningEffort.Type == gjson.String {
		effort := strings.ToLower(strings.TrimSpace(reasoningEffort.String()))
		if effort != "" {
			out, _ = sjson.SetBytes(out, "reasoning_effort", effort)
		}
	}

	messages := make([][]byte, 0)
	appendMessage := func(message []byte) {
		messages = append(messages, message)
	}

	// Top-level instructions become the leading system message.
	if instructions := root.Get("instructions"); instructions.Exists() && instructions.Type == gjson.String {
		if text := instructions.String(); text != "" {
			sys := []byte(`{"role":"system","content":""}`)
			sys, _ = sjson.SetBytes(sys, "content", text)
			appendMessage(sys)
		}
	}

	// Convert input items to chat completion messages.
	if input := root.Get("input"); input.Exists() && input.IsArray() {
		items := input.Array()

		// Pre-scan tool output call IDs so regular messages between a tool call
		// and its output can be deferred, preserving strict adjacency rules.
		outputCallIDs := make(map[string]struct{})
		for _, item := range items {
			itemType := item.Get("type").String()
			if itemType != "function_call_output" && itemType != "custom_tool_call_output" {
				continue
			}
			if callID := strings.TrimSpace(item.Get("call_id").String()); callID != "" {
				outputCallIDs[callID] = struct{}{}
			}
		}

		pendingToolCalls := make([]interface{}, 0)
		pendingToolCallIDs := make([]string, 0)
		awaitingToolOutputs := make(map[string]struct{})
		deferredMessages := make([][]byte, 0)

		hasAwaitingToolOutput := func() bool {
			for id := range awaitingToolOutputs {
				if _, ok := outputCallIDs[id]; ok {
					return true
				}
			}
			return false
		}
		appendRegularMessage := func(message []byte) {
			if hasAwaitingToolOutput() {
				deferredMessages = append(deferredMessages, message)
				return
			}
			appendMessage(message)
		}
		flushPendingToolCalls := func() {
			if len(pendingToolCalls) == 0 {
				return
			}
			asst := []byte(`{"role":"assistant","tool_calls":[]}`)
			asst, _ = sjson.SetBytes(asst, "tool_calls", pendingToolCalls)
			appendMessage(asst)
			for _, id := range pendingToolCallIDs {
				if id != "" {
					awaitingToolOutputs[id] = struct{}{}
				}
			}
			pendingToolCalls = pendingToolCalls[:0]
			pendingToolCallIDs = pendingToolCallIDs[:0]
		}
		flushDeferredMessages := func() {
			for _, message := range deferredMessages {
				appendMessage(message)
			}
			deferredMessages = deferredMessages[:0]
		}

		for _, item := range items {
			itemType := item.Get("type").String()
			if itemType == "" && item.Get("role").String() != "" {
				itemType = "message"
			}
			if itemType != "function_call" && itemType != "custom_tool_call" {
				flushPendingToolCalls()
			}

			switch itemType {
			case "message", "":
				role := item.Get("role").String()
				role = normalizeResponsesRole(role)
				if role == "" {
					role = "user"
				}

				message := []byte(`{"role":"","content":[]}`)
				message, _ = sjson.SetBytes(message, "role", role)

				if content := item.Get("content"); content.Exists() && content.IsArray() {
					parts := make([][]byte, 0)
					content.ForEach(func(_, part gjson.Result) bool {
						if p := convertResponsesContentPartToChat(part); len(p) > 0 {
							parts = append(parts, p)
						}
						return true
					})
					if len(parts) > 0 {
						message = common.SetRawArrayItems(message, "content", parts)
					} else {
						message, _ = sjson.SetBytes(message, "content", "")
					}
				} else if content.Type == gjson.String {
					message, _ = sjson.SetBytes(message, "content", content.String())
				} else {
					message, _ = sjson.SetBytes(message, "content", "")
				}

				appendRegularMessage(message)

			case "reasoning":
				// Reasoning history items are not part of the standard Chat
				// Completions schema, so they are dropped on conversion.

			case "function_call":
				toolCall := []byte(`{"id":"","type":"function","function":{"name":"","arguments":""}}`)
				callID := strings.TrimSpace(item.Get("call_id").String())
				toolCall, _ = sjson.SetBytes(toolCall, "id", callID)

				name := item.Get("name").String()
				if namespace := strings.TrimSpace(item.Get("namespace").String()); namespace != "" {
					name = qualifyResponsesNamespaceToolName(namespace, name)
				}
				toolCall, _ = sjson.SetBytes(toolCall, "function.name", name)

				arguments := normalizeFunctionArguments(item.Get("arguments"))
				toolCall, _ = sjson.SetBytes(toolCall, "function.arguments", arguments)

				pendingToolCalls = append(pendingToolCalls, gjson.ParseBytes(toolCall).Value())
				if callID != "" {
					pendingToolCallIDs = append(pendingToolCallIDs, callID)
				}

			case "function_call_output":
				toolMsg := []byte(`{"role":"tool","tool_call_id":"","content":""}`)
				callID := strings.TrimSpace(item.Get("call_id").String())
				toolMsg, _ = sjson.SetBytes(toolMsg, "tool_call_id", callID)

				content := responsesToolOutputText(item.Get("output"))
				toolMsg, _ = sjson.SetBytes(toolMsg, "content", content)

				delete(awaitingToolOutputs, callID)
				appendMessage(toolMsg)
				if len(awaitingToolOutputs) == 0 && len(deferredMessages) > 0 {
					flushDeferredMessages()
				}

			case "custom_tool_call":
				toolCall := []byte(`{"id":"","type":"function","function":{"name":"","arguments":""}}`)
				callID := strings.TrimSpace(item.Get("call_id").String())
				toolCall, _ = sjson.SetBytes(toolCall, "id", callID)
				toolCall, _ = sjson.SetBytes(toolCall, "function.name", item.Get("name").String())

				wrapped, _ := sjson.SetBytes([]byte(`{"input":""}`), "input", item.Get("input").String())
				toolCall, _ = sjson.SetBytes(toolCall, "function.arguments", string(wrapped))

				pendingToolCalls = append(pendingToolCalls, gjson.ParseBytes(toolCall).Value())
				if callID != "" {
					pendingToolCallIDs = append(pendingToolCallIDs, callID)
				}

			case "custom_tool_call_output":
				toolMsg := []byte(`{"role":"tool","tool_call_id":"","content":""}`)
				callID := strings.TrimSpace(item.Get("call_id").String())
				toolMsg, _ = sjson.SetBytes(toolMsg, "tool_call_id", callID)
				content := responsesToolOutputText(item.Get("output"))
				toolMsg, _ = sjson.SetBytes(toolMsg, "content", content)

				delete(awaitingToolOutputs, callID)
				appendMessage(toolMsg)
				if len(awaitingToolOutputs) == 0 && len(deferredMessages) > 0 {
					flushDeferredMessages()
				}
			}
		}

		flushPendingToolCalls()
		flushDeferredMessages()
	} else if input.Exists() && input.Type == gjson.String {
		msg := []byte(`{}`)
		msg, _ = sjson.SetBytes(msg, "role", "user")
		msg, _ = sjson.SetBytes(msg, "content", input.String())
		appendMessage(msg)
	}

	if len(messages) > 0 {
		out, _ = sjson.SetRawBytes(out, "messages", common.JoinRawArray(messages))
	}

	// Convert tools.
	tools := root.Get("tools")
	if tools.Exists() && tools.IsArray() {
		chatTools := make([]interface{}, 0)
		tools.ForEach(func(_, tool gjson.Result) bool {
			for _, t := range convertResponsesToolToOpenAIChatTools(tool) {
				chatTools = append(chatTools, gjson.ParseBytes(t).Value())
			}
			return true
		})
		if len(chatTools) > 0 {
			out, _ = sjson.SetBytes(out, "tools", chatTools)
			if parallelToolCalls := root.Get("parallel_tool_calls"); parallelToolCalls.Exists() {
				out, _ = sjson.SetBytes(out, "parallel_tool_calls", parallelToolCalls.Bool())
			}
			if toolChoice := root.Get("tool_choice"); toolChoice.Exists() {
				out, _ = sjson.SetRawBytes(out, "tool_choice", []byte(toolChoice.Raw))
			}
		}
	}

	return out
}

func responsesImageURL(value gjson.Result) string {
	if value.Type == gjson.String {
		return value.String()
	}
	for _, path := range []string{"url", "uri", "file_uri"} {
		if candidate := value.Get(path); candidate.Type == gjson.String && candidate.String() != "" {
			return candidate.String()
		}
	}
	return ""
}

func normalizeResponsesRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "developer", "system":
		return "system"
	case "user":
		return "user"
	case "assistant", "model":
		return "assistant"
	case "tool":
		return "tool"
	default:
		return ""
	}
}

func convertResponsesContentPartToChat(part gjson.Result) []byte {
	pType := part.Get("type").String()
	if pType == "" {
		pType = "input_text"
	}
	switch pType {
	case "input_text", "output_text":
		if text := part.Get("text"); text.Exists() {
			p := []byte(`{"type":"text","text":""}`)
			p, _ = sjson.SetBytes(p, "text", text.String())
			return p
		}
	case "input_image":
		imageURL := responsesImageURL(part.Get("image_url"))
		if imageURL == "" {
			imageURL = responsesImageURL(part.Get("url"))
		}
		if imageURL == "" {
			return nil
		}
		p := []byte(`{"type":"image_url","image_url":{"url":""}}`)
		p, _ = sjson.SetBytes(p, "image_url.url", imageURL)
		if detail := part.Get("detail"); detail.Exists() {
			p, _ = sjson.SetBytes(p, "image_url.detail", detail.String())
		}
		return p
	}
	return nil
}

func normalizeFunctionArguments(arguments gjson.Result) string {
	if !arguments.Exists() {
		return ""
	}
	if arguments.Type == gjson.String {
		return arguments.String()
	}
	// Object/array must be serialized into a JSON string for Chat Completions.
	if raw := arguments.Raw; raw != "" {
		return raw
	}
	b, _ := json.Marshal(arguments.Value())
	return string(b)
}
