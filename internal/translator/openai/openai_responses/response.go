package openai_responses

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// codexStreamState holds state for Codex Responses→OpenAI streaming.
type codexStreamState struct {
	MessageID   string
	Model       string
	OutputIndex int
	ToolIndex   int
	ToolArgsAcc map[int]*strings.Builder
	ToolNames   map[int]string
	ContentAcc  strings.Builder
}

var dataTag = []byte("data:")

func getCodexState(param *any) *codexStreamState {
	if *param == nil {
		*param = &codexStreamState{
			ToolArgsAcc: make(map[int]*strings.Builder),
			ToolNames:   make(map[int]string),
		}
	}
	return (*param).(*codexStreamState)
}

// convertCodexResponsesToOpenAIStream converts Codex Responses streaming events to OpenAI Chat Completions format.
func convertCodexResponsesToOpenAIStream(_ context.Context, _ string, _, _ []byte, rawChunk []byte, param *any) [][]byte {
	state := getCodexState(param)

	raw := bytes.TrimSpace(rawChunk)
	if bytes.HasPrefix(raw, dataTag) {
		raw = bytes.TrimSpace(raw[5:])
	}
	if len(raw) == 0 || bytes.Equal(raw, []byte("[DONE]")) {
		return nil
	}

	root := gjson.ParseBytes(raw)
	eventType := root.Get("type").String()

	if state.MessageID == "" {
		state.MessageID = root.Get("response.id").String()
	}
	if state.Model == "" {
		state.Model = root.Get("response.model").String()
	}

	switch eventType {
	case "response.output_text.delta":
		text := root.Get("delta").String()
		chunk := buildOpenAIChunkFromCodex(state.MessageID, state.Model, &text, nil)
		return [][]byte{chunk}

	case "response.output_item.done":
		if root.Get("item.type").String() == "function_call" {
			state.ToolIndex++
			name := root.Get("item.name").String()
			callID := root.Get("item.call_id").String()
			args := root.Get("item.arguments").String()
			tc := map[string]interface{}{
				"index": state.ToolIndex,
				"id":    callID,
				"type":  "function",
				"function": map[string]interface{}{
					"name":      name,
					"arguments": args,
				},
			}
			chunk := buildOpenAIChunkFromCodex(state.MessageID, state.Model, nil, []map[string]interface{}{tc})
			return [][]byte{chunk}
		}

	case "response.completed":
		chunk := buildOpenAIChunkFromCodex(state.MessageID, state.Model, nil, nil)
		chunk, _ = sjson.SetBytes(chunk, "choices.0.finish_reason", "stop")
		done := []byte("data: [DONE]\n\n")
		return [][]byte{chunk, done}

	case "response.function_call_arguments.delta":
		// Function call arguments are streamed in Codex Responses
		return nil
	}

	return nil
}

// convertCodexResponsesToOpenAINonStream converts a complete Codex Responses response to OpenAI format.
func convertCodexResponsesToOpenAINonStream(_ context.Context, _ string, _, _ []byte, rawResponse []byte, _ *any) []byte {
	root := gjson.ParseBytes(rawResponse)

	out := make(map[string]interface{})
	out["id"] = root.Get("id").String()
	out["object"] = "chat.completion"
	out["model"] = root.Get("model").String()

	var textParts []string
	var toolCalls []map[string]interface{}
	toolIdx := 0

	if output := root.Get("output"); output.Exists() && output.IsArray() {
		output.ForEach(func(_, item gjson.Result) bool {
			iType := item.Get("type").String()
			switch iType {
			case "message":
				if content := item.Get("content"); content.Exists() && content.IsArray() {
					content.ForEach(func(_, part gjson.Result) bool {
						if part.Get("type").String() == "output_text" {
							textParts = append(textParts, part.Get("text").String())
						}
						return true
					})
				}
			case "function_call":
				toolIdx++
				toolCalls = append(toolCalls, map[string]interface{}{
					"id":   item.Get("call_id").String(),
					"type": "function",
					"function": map[string]interface{}{
						"name":      item.Get("name").String(),
						"arguments": item.Get("arguments").String(),
					},
				})
			}
			return true
		})
	}

	msg := map[string]interface{}{
		"role": "assistant",
	}
	if len(textParts) > 0 {
		msg["content"] = strings.Join(textParts, "")
	}
	if len(toolCalls) > 0 {
		msg["tool_calls"] = toolCalls
		msg["finish_reason"] = "tool_calls"
	} else {
		msg["finish_reason"] = "stop"
	}
	out["choices"] = []map[string]interface{}{{"index": 0, "message": msg}}

	if usage := root.Get("usage"); usage.Exists() {
		out["usage"] = map[string]interface{}{
			"prompt_tokens":     usage.Get("input_tokens").Int(),
			"completion_tokens": usage.Get("output_tokens").Int(),
			"total_tokens":      usage.Get("input_tokens").Int() + usage.Get("output_tokens").Int(),
		}
	}

	result, _ := json.Marshal(out)
	return result
}

func buildOpenAIChunkFromCodex(id, model string, content *string, toolCalls []map[string]interface{}) []byte {
	chunk := []byte(`{"object":"chat.completion.chunk","choices":[{"index":0,"delta":{}}]}`)
	chunk, _ = sjson.SetBytes(chunk, "id", "chatcmpl-"+id)
	chunk, _ = sjson.SetBytes(chunk, "model", model)
	if content != nil {
		chunk, _ = sjson.SetBytes(chunk, "choices.0.delta.content", *content)
	}
	if toolCalls != nil {
		b, _ := json.Marshal(toolCalls)
		chunk, _ = sjson.SetRawBytes(chunk, "choices.0.delta.tool_calls", b)
	}
	return []byte(fmt.Sprintf("data: %s\n\n", string(chunk)))
}
