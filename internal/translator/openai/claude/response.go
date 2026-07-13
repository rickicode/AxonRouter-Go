package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// claudeStreamState holds accumulated state for streaming response translation.
type claudeStreamState struct {
	MessageID            string
	Model                string
	ContentBlockStarted  bool
	ContentBlockIndex    int
	ThinkingBlockStarted bool
	ThinkingBlockIndex   int
	ToolBlocks           map[int]int // upstream tool index -> claude content block index
	ToolNames            map[int]string
	ToolStartEmitted     map[int]bool
	ToolArgsAccum        map[int]*strings.Builder
	TextAccum            strings.Builder
	ThinkingAccum        strings.Builder
	FinishReason         string
	MessageStartSent     bool
	MessageStopSent      bool
	ContentBlocksStopped bool
	SawToolCall          bool
}

var dataTag = []byte("data:")

func getStreamState(param *any) *claudeStreamState {
	if *param == nil {
		*param = &claudeStreamState{
			ToolBlocks:       make(map[int]int),
			ToolNames:        make(map[int]string),
			ToolStartEmitted: make(map[int]bool),
			ToolArgsAccum:    make(map[int]*strings.Builder),
		}
	}
	return (*param).(*claudeStreamState)
}

// convertClaudeResponseToOpenAIStream converts Claude streaming events to OpenAI chunks.
func convertClaudeResponseToOpenAIStream(_ context.Context, _ string, _, _ []byte, rawChunk []byte, param *any) [][]byte {
	state := getStreamState(param)

	if !bytes.HasPrefix(rawChunk, dataTag) {
		return nil
	}
	rawChunk = bytes.TrimSpace(rawChunk[5:])

	// Parse Claude SSE event
	root := gjson.ParseBytes(rawChunk)
	eventType := root.Get("type").String()

	switch eventType {
	case "message_start":
		return handleMessageStart(root, state)
	case "content_block_start":
		return handleContentBlockStart(root, state)
	case "content_block_delta":
		return handleContentBlockDelta(root, state)
	case "content_block_stop":
		return handleContentBlockStop(root, state)
	case "message_delta":
		return handleMessageDelta(root, state)
	case "message_stop":
		return handleMessageStop(state)
	case "ping", "error":
		return nil
	}
	return nil
}

// convertClaudeResponseToOpenAINonStream converts a complete Claude response to OpenAI format.
func convertClaudeResponseToOpenAINonStream(_ context.Context, _ string, _, _ []byte, rawResponse []byte, _ *any) []byte {
	root := gjson.ParseBytes(rawResponse)

	out := make(map[string]interface{})
	out["id"] = "chatcmpl-" + root.Get("id").String()
	out["object"] = "chat.completion"
	out["model"] = root.Get("model").String()
	out["created"] = root.Get("created_at").Int()

	msg := map[string]interface{}{
		"role": "assistant",
	}

	var textParts []string
	var reasoningParts []string
	var toolCalls []map[string]interface{}

	if content := root.Get("content"); content.Exists() && content.IsArray() {
		content.ForEach(func(_, block gjson.Result) bool {
			bType := block.Get("type").String()
			switch bType {
			case "text":
				textParts = append(textParts, block.Get("text").String())
			case "thinking":
				reasoningParts = append(reasoningParts, block.Get("thinking").String())
			case "tool_use":
				tc := map[string]interface{}{
					"id":   block.Get("id").String(),
					"type": "function",
					"function": map[string]interface{}{
						"name":      UncloakClaudeToolName(block.Get("name").String()),
						"arguments": block.Get("input").Raw,
					},
				}
				toolCalls = append(toolCalls, tc)
			}
			return true
		})
	}

	if len(textParts) > 0 {
		msg["content"] = strings.Join(textParts, "")
	}
	if len(reasoningParts) > 0 {
		msg["reasoning_content"] = strings.Join(reasoningParts, "")
	}
	if len(toolCalls) > 0 {
		msg["tool_calls"] = toolCalls
	}

	stopReason := root.Get("stop_reason").String()
	switch stopReason {
	case "end_turn":
		msg["finish_reason"] = "stop"
	case "tool_use":
		msg["finish_reason"] = "tool_calls"
	case "max_tokens":
		msg["finish_reason"] = "length"
	default:
		msg["finish_reason"] = "stop"
	}

	out["choices"] = []map[string]interface{}{{"index": 0, "message": msg}}

	if usage := root.Get("usage"); usage.Exists() {
		in := usage.Get("input_tokens").Int()
		read := usage.Get("cache_read_input_tokens").Int()
		creation := usage.Get("cache_creation_input_tokens").Int()
		out["usage"] = map[string]interface{}{
			"prompt_tokens":     in + read + creation,
			"completion_tokens": usage.Get("output_tokens").Int(),
			"total_tokens":      in + read + creation + usage.Get("output_tokens").Int(),
			"prompt_tokens_details": map[string]interface{}{
				"cached_tokens":         read,
				"cache_creation_tokens": creation,
			},
		}
	}

	result, _ := json.Marshal(out)
	return result
}

func handleMessageStart(root gjson.Result, state *claudeStreamState) [][]byte {
	msg := root.Get("message")
	state.MessageID = msg.Get("id").String()
	state.Model = msg.Get("model").String()
	state.MessageStartSent = true

	chunk := buildOpenAIChunk(state.MessageID, state.Model, nil, nil, nil)
	return [][]byte{chunk}
}

func handleContentBlockStart(root gjson.Result, state *claudeStreamState) [][]byte {
	index := int(root.Get("index").Int())
	blockType := root.Get("content_block.type").String()

	if blockType == "thinking" {
		state.ThinkingBlockStarted = true
		state.ThinkingBlockIndex = index
		state.ThinkingAccum.Reset()
		return nil
	}

	if blockType == "tool_use" {
		toolID := root.Get("content_block.id").String()
		toolName := root.Get("content_block.name").String()
		state.ToolNames[index] = toolName
		state.ToolBlocks[index] = index
		state.ToolStartEmitted[index] = false
		state.ToolArgsAccum[index] = &strings.Builder{}
		_ = toolID
		return nil
	}

	if blockType == "text" && !state.ContentBlockStarted {
		state.ContentBlockStarted = true
		state.ContentBlockIndex = index
	}
	return nil
}

func handleContentBlockDelta(root gjson.Result, state *claudeStreamState) [][]byte {
	index := int(root.Get("index").Int())
	deltaType := root.Get("delta.type").String()

	if deltaType == "thinking_delta" {
		text := root.Get("delta.thinking").String()
		state.ThinkingAccum.WriteString(text)
		chunk := buildOpenAIChunk(state.MessageID, state.Model, nil, nil, &text)
		return [][]byte{chunk}
	}

	if deltaType == "text_delta" {
		text := root.Get("delta.text").String()
		state.TextAccum.WriteString(text)

		chunk := buildOpenAIChunk(state.MessageID, state.Model, &text, nil, nil)
		return [][]byte{chunk}
	}

	if deltaType == "input_json_delta" {
		if acc, ok := state.ToolArgsAccum[index]; ok {
			acc.WriteString(root.Get("delta.partial_json").String())
		}
		return nil
	}

	return nil
}

func handleContentBlockStop(root gjson.Result, state *claudeStreamState) [][]byte {
	index := int(root.Get("index").Int())

	// If a tool block just completed, emit the tool_call delta
	if args, ok := state.ToolArgsAccum[index]; ok {
		name := UncloakClaudeToolName(state.ToolNames[index])
		argsStr := args.String()
		if argsStr == "" {
			argsStr = "{}"
		}

		tc := map[string]interface{}{
			"index": index,
			"id":    "call_" + state.MessageID,
			"type":  "function",
			"function": map[string]interface{}{
				"name":      name,
				"arguments": argsStr,
			},
		}
		chunk := buildOpenAIChunk(state.MessageID, state.Model, nil, []map[string]interface{}{tc}, nil)
		state.SawToolCall = true
		delete(state.ToolArgsAccum, index)
		delete(state.ToolNames, index)
		return [][]byte{chunk}
	}

	return nil
}

func handleMessageDelta(root gjson.Result, state *claudeStreamState) [][]byte {
	stopReason := root.Get("delta.stop_reason").String()
	state.FinishReason = stopReason

	var finishReason *string
	switch stopReason {
	case "end_turn":
		s := "stop"
		finishReason = &s
	case "tool_use":
		s := "tool_calls"
		finishReason = &s
	case "max_tokens":
		s := "length"
		finishReason = &s
	}

	chunk := buildOpenAIChunk(state.MessageID, state.Model, nil, nil, nil)
	if finishReason != nil {
		chunk, _ = sjson.SetBytes(chunk, "choices.0.finish_reason", *finishReason)
	}

	return [][]byte{chunk}
}

func handleMessageStop(state *claudeStreamState) [][]byte {
	state.MessageStopSent = true
	done := []byte("data: [DONE]\n\n")
	return [][]byte{done}
}

func buildOpenAIChunk(id, model string, content *string, toolCalls []map[string]interface{}, reasoningContent *string) []byte {
	chunk := []byte(`{"object":"chat.completion.chunk","choices":[{"index":0,"delta":{}}]}`)
	chunk, _ = sjson.SetBytes(chunk, "id", "chatcmpl-"+id)
	chunk, _ = sjson.SetBytes(chunk, "model", model)
	if content != nil {
		chunk, _ = sjson.SetBytes(chunk, "choices.0.delta.content", *content)
	}
	if toolCalls != nil {
		chunk, _ = sjson.SetRawBytes(chunk, "choices.0.delta.tool_calls", mustMarshal(toolCalls))
	}
	if reasoningContent != nil {
		chunk, _ = sjson.SetBytes(chunk, "choices.0.delta.reasoning_content", *reasoningContent)
	}
	return []byte("data: " + string(chunk) + "\n\n")
}

func mustMarshal(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}
