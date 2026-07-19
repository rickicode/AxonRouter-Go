package grok_cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// grokStreamState holds per-stream bookkeeping for Grok CLI Responses → OpenAI.
type grokStreamState struct {
	MessageID string
	Model     string
	ToolIndex int
	ToolNames map[int]string
	ToolArgs  map[string]string // call_id -> accumulated arguments
}

var dataTag = []byte("data:")

func getGrokState(param *any) *grokStreamState {
	if *param == nil {
		*param = &grokStreamState{
			ToolIndex: -1,
			ToolNames: make(map[int]string),
			ToolArgs:  make(map[string]string),
		}
	}
	return (*param).(*grokStreamState)
}

// convertGrokResponseToOpenAIStream converts Grok CLI Responses streaming events
// to OpenAI Chat Completions SSE chunks.
func convertGrokResponseToOpenAIStream(_ context.Context, _ string, _, _ []byte, rawChunk []byte, param *any) [][]byte {
	state := getGrokState(param)

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
		if text == "" {
			return nil
		}
		chunk := buildOpenAIChunkFromGrok(state.MessageID, state.Model, &text, nil)
		return [][]byte{chunk}

	case "response.output_item.done":
		item := root.Get("item")
		if item.Get("type").String() == "function_call" {
			state.ToolIndex++
			name := item.Get("name").String()
			state.ToolNames[state.ToolIndex] = name
			callID := item.Get("call_id").String()
			args := item.Get("arguments").String()
			if args == "" {
				if acc, ok := state.ToolArgs[callID]; ok {
					args = acc
					delete(state.ToolArgs, callID)
				}
			}
			tc := map[string]interface{}{
				"index": state.ToolIndex,
				"id":    callID,
				"type":  "function",
				"function": map[string]interface{}{
					"name":      name,
					"arguments": args,
				},
			}
			chunk := buildOpenAIChunkFromGrok(state.MessageID, state.Model, nil, []map[string]interface{}{tc})
			return [][]byte{chunk}
		}

	case "response.completed":
		finishReason := "stop"
		if state.ToolIndex >= 0 && len(state.ToolNames) > 0 {
			finishReason = "tool_calls"
		}
		chunk := buildOpenAIChunkFromGrok(state.MessageID, state.Model, nil, nil)
		chunk, _ = sjson.SetBytes(chunk, "choices.0.finish_reason", finishReason)
		return [][]byte{chunk}

	case "response.function_call_arguments.delta":
		callID := root.Get("item_id").String()
		if callID == "" {
			callID = root.Get("call_id").String()
		}
		if callID != "" {
			state.ToolArgs[callID] += root.Get("delta").String()
		}
		return nil
	}

	return nil
}

// convertGrokResponseToOpenAINonStream converts a complete Grok CLI Responses
// response body to OpenAI Chat Completions format.
func convertGrokResponseToOpenAINonStream(_ context.Context, _ string, _, _ []byte, rawResponse []byte, _ *any) []byte {
	root := gjson.ParseBytes(rawResponse)

	// Grok CLI's non-stream executor returns the SSE event wrapper for
	// /v1/responses compatibility. If we see a top-level "response" object,
	// unwrap it before translating to OpenAI Chat Completions format.
	if r := root.Get("response"); r.Exists() && r.Type == gjson.JSON {
		root = r
	}

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
	}

	finishReason := "stop"
	if len(toolCalls) > 0 {
		finishReason = "tool_calls"
	}
	out["choices"] = []map[string]interface{}{{
		"index":         0,
		"message":       msg,
		"finish_reason": finishReason,
	}}

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

func buildOpenAIChunkFromGrok(id, model string, content *string, toolCalls []map[string]interface{}) []byte {
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
