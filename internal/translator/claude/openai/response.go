package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"github.com/rickicode/AxonRouter-Go/internal/translator/openai/claude"
	"github.com/tidwall/gjson"
)

// claudeStreamState holds accumulated state for OpenAI→Claude streaming.
type claudeStreamState struct {
	MessageID             string
	Model                 string
	CreatedAt             int64
	ToolNameMap           map[string]string
	ContentBlockStarted   bool
	ContentBlockIndex     int
	ThinkingBlockStarted  bool
	ThinkingBlockIndex    int
	ToolBlocks            map[int]int
	ToolStartEmitted      map[int]bool
	ToolArgsAccum         map[int]*strings.Builder
	TextAccum             strings.Builder
	ThinkingAccum         strings.Builder
	FinishReason          string
	MessageStartSent      bool
	MessageStopSent       bool
	SawToolCall           bool
	NextContentBlockIndex int
}

var dataTag = []byte("data:")

func getState(param *any) *claudeStreamState {
	if *param == nil {
		*param = &claudeStreamState{
			ToolBlocks:            make(map[int]int),
			ToolStartEmitted:      make(map[int]bool),
			ToolArgsAccum:         make(map[int]*strings.Builder),
			NextContentBlockIndex: 0,
		}
	}
	return (*param).(*claudeStreamState)
}

// convertOpenAIResponseToClaudeStream converts OpenAI streaming chunks to Claude SSE events.
func convertOpenAIResponseToClaudeStream(_ context.Context, _ string, _, _ []byte, rawChunk []byte, param *any) [][]byte {
	state := getState(param)

	if !bytes.HasPrefix(rawChunk, dataTag) {
		return nil
	}
	rawChunk = bytes.TrimSpace(rawChunk[5:])

	if bytes.Equal(rawChunk, []byte("[DONE]")) {
		return handleClaudeDone(state)
	}

	root := gjson.ParseBytes(rawChunk)

	// Initialize from first chunk
	if state.MessageID == "" {
		state.MessageID = root.Get("id").String()
	}
	if state.Model == "" {
		state.Model = root.Get("model").String()
	}

	// Send message_start if not sent
	var results [][]byte
	if !state.MessageStartSent {
		results = append(results, buildClaudeMessageStart(state))
		state.MessageStartSent = true
	}

	if choices := root.Get("choices"); choices.Exists() && choices.IsArray() {
		choices.ForEach(func(_, choice gjson.Result) bool {
			delta := choice.Get("delta")
			finish := choice.Get("finish_reason").String()

			// Handle reasoning_content → thinking block
			if reasoningContent := delta.Get("reasoning_content"); reasoningContent.Exists() && reasoningContent.String() != "" {
				if !state.ThinkingBlockStarted {
					results = append(results, buildClaudeContentBlockStart(state, "thinking"))
					state.ThinkingBlockStarted = true
					state.ThinkingBlockIndex = state.NextContentBlockIndex
					state.NextContentBlockIndex++
				}
				state.ThinkingAccum.WriteString(reasoningContent.String())
				results = append(results, buildClaudeThinkingDelta(state.ThinkingBlockIndex, reasoningContent.String()))
			}

			if content := delta.Get("content"); content.Exists() {
				// Close any open thinking block before starting text
				if state.ThinkingBlockStarted {
					results = append(results, buildClaudeContentBlockStop(state.ThinkingBlockIndex))
					state.ThinkingBlockStarted = false
				}
				if !state.ContentBlockStarted {
					results = append(results, buildClaudeContentBlockStart(state, "text"))
					state.ContentBlockStarted = true
					state.ContentBlockIndex = state.NextContentBlockIndex
					state.NextContentBlockIndex++
				}
				results = append(results, buildClaudeTextDelta(state.ContentBlockIndex, content.String()))
			}

			if toolCalls := delta.Get("tool_calls"); toolCalls.Exists() && toolCalls.IsArray() {
				// Close any open thinking block before starting tool
				if state.ThinkingBlockStarted {
					results = append(results, buildClaudeContentBlockStop(state.ThinkingBlockIndex))
					state.ThinkingBlockStarted = false
				}
				toolCalls.ForEach(func(_, tc gjson.Result) bool {
					idx := int(tc.Get("index").Int())
					if !state.ToolStartEmitted[idx] {
						toolID := tc.Get("id").String()
						toolName := claude.UncloakClaudeToolName(tc.Get("function.name").String())
						if toolID == "" {
							toolID = "call_" + state.MessageID
						}
						results = append(results, buildClaudeToolUseStart(state, idx, toolID, toolName))
						state.ToolStartEmitted[idx] = true
					}
					if args := tc.Get("function.arguments"); args.Exists() {
						if acc, ok := state.ToolArgsAccum[idx]; ok {
							acc.WriteString(args.String())
						} else {
							acc := &strings.Builder{}
							acc.WriteString(args.String())
							state.ToolArgsAccum[idx] = acc
						}
					}
					return true
				})
			}

			if finish != "" {
				state.FinishReason = finish
			}
			return true
		})
	}

	return results
}

// convertOpenAIResponseToClaudeNonStream converts a complete OpenAI response to Claude format.
func convertOpenAIResponseToClaudeNonStream(_ context.Context, _ string, _, _ []byte, rawResponse []byte, _ *any) []byte {
	root := gjson.ParseBytes(rawResponse)

	out := make(map[string]interface{})
	out["id"] = root.Get("id").String()
	out["type"] = "message"
	out["role"] = "assistant"
	out["model"] = root.Get("model").String()

	var content []map[string]interface{}
	if choices := root.Get("choices"); choices.Exists() && choices.IsArray() {
		choices.ForEach(func(_, choice gjson.Result) bool {
			msg := choice.Get("message")

			// Handle reasoning_content → thinking block
			if reasoningContent := msg.Get("reasoning_content"); reasoningContent.Exists() && reasoningContent.String() != "" {
				content = append(content, map[string]interface{}{
					"type":     "thinking",
					"thinking": reasoningContent.String(),
				})
			}

			if text := msg.Get("content"); text.Exists() && text.String() != "" {
				content = append(content, map[string]interface{}{
					"type": "text",
					"text": text.String(),
				})
			}
			if toolCalls := msg.Get("tool_calls"); toolCalls.Exists() && toolCalls.IsArray() {
				toolCalls.ForEach(func(_, tc gjson.Result) bool {
					var args interface{}
					json.Unmarshal([]byte(tc.Get("function.arguments").String()), &args)
					content = append(content, map[string]interface{}{
						"type":  "tool_use",
						"id":    tc.Get("id").String(),
						"name":  tc.Get("function.name").String(),
						"input": args,
					})
					return true
				})
			}

			finish := choice.Get("finish_reason").String()
			switch finish {
			case "stop":
				out["stop_reason"] = "end_turn"
			case "tool_calls":
				out["stop_reason"] = "tool_use"
			case "length":
				out["stop_reason"] = "max_tokens"
			default:
				out["stop_reason"] = "end_turn"
			}
			return true
		})
	}
	out["content"] = content

	if usage := root.Get("usage"); usage.Exists() {
		out["usage"] = map[string]interface{}{
			"input_tokens":  usage.Get("prompt_tokens").Int(),
			"output_tokens": usage.Get("completion_tokens").Int(),
		}
	}

	result, _ := json.Marshal(out)
	return result
}

func buildClaudeMessageStart(state *claudeStreamState) []byte {
	event := map[string]interface{}{
		"type": "message_start",
		"message": map[string]interface{}{
			"id":            state.MessageID,
			"type":          "message",
			"role":          "assistant",
			"model":         state.Model,
			"content":       []interface{}{},
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage":         map[string]interface{}{"input_tokens": 0, "output_tokens": 0},
		},
	}
	b, _ := json.Marshal(event)
	return []byte("data: " + string(b) + "\n\n")
}

func buildClaudeContentBlockStart(state *claudeStreamState, blockType string) []byte {
	block := map[string]interface{}{
		"type": blockType,
	}
	if blockType == "text" {
		block["text"] = ""
	} else if blockType == "thinking" {
		block["thinking"] = ""
	}
	event := map[string]interface{}{
		"type":          "content_block_start",
		"index":         state.NextContentBlockIndex - 1,
		"content_block": block,
	}
	b, _ := json.Marshal(event)
	return []byte("data: " + string(b) + "\n\n")
}

func buildClaudeTextDelta(index int, text string) []byte {
	event := map[string]interface{}{
		"type":  "content_block_delta",
		"index": index,
		"delta": map[string]interface{}{
			"type": "text_delta",
			"text": text,
		},
	}
	b, _ := json.Marshal(event)
	return []byte("data: " + string(b) + "\n\n")
}

func buildClaudeThinkingDelta(index int, thinking string) []byte {
	event := map[string]interface{}{
		"type":  "content_block_delta",
		"index": index,
		"delta": map[string]interface{}{
			"type":     "thinking_delta",
			"thinking": thinking,
		},
	}
	b, _ := json.Marshal(event)
	return []byte("data: " + string(b) + "\n\n")
}

func buildClaudeContentBlockStop(index int) []byte {
	event := map[string]interface{}{
		"type":  "content_block_stop",
		"index": index,
	}
	b, _ := json.Marshal(event)
	return []byte("data: " + string(b) + "\n\n")
}

func buildClaudeToolUseStart(state *claudeStreamState, idx int, id, name string) []byte {
	blockIndex := state.NextContentBlockIndex
	state.ToolBlocks[idx] = blockIndex
	state.NextContentBlockIndex++

	event := map[string]interface{}{
		"type":  "content_block_start",
		"index": blockIndex,
		"content_block": map[string]interface{}{
			"type":  "tool_use",
			"id":    id,
			"name":  name,
			"input": map[string]interface{}{},
		},
	}
	b, _ := json.Marshal(event)
	return []byte("data: " + string(b) + "\n\n")
}

func handleClaudeDone(state *claudeStreamState) [][]byte {
	var results [][]byte

	// Stop any open thinking block first
	if state.ThinkingBlockStarted {
		results = append(results, buildClaudeContentBlockStop(state.ThinkingBlockIndex))
	}

	// Stop any open content blocks
	if state.ContentBlockStarted {
		results = append(results, buildClaudeContentBlockStop(state.ContentBlockIndex))
	}

	// Stop any open tool blocks
	for idx, blockIndex := range state.ToolBlocks {
		if acc, ok := state.ToolArgsAccum[idx]; ok && acc.Len() > 0 {
			// Emit input_json_delta for accumulated args
			delta := map[string]interface{}{
				"type":  "content_block_delta",
				"index": blockIndex,
				"delta": map[string]interface{}{
					"type":         "input_json_delta",
					"partial_json": acc.String(),
				},
			}
			d, _ := json.Marshal(delta)
			results = append(results, []byte("data: "+string(d)+"\n\n"))
		}
		results = append(results, buildClaudeContentBlockStop(blockIndex))
	}

	// message_delta with stop_reason
	stopReason := "end_turn"
	if state.SawToolCall || state.FinishReason == "tool_calls" {
		stopReason = "tool_use"
	}
	msgDelta := map[string]interface{}{
		"type":  "message_delta",
		"delta": map[string]interface{}{"stop_reason": stopReason},
	}
	md, _ := json.Marshal(msgDelta)
	results = append(results, []byte("data: "+string(md)+"\n\n"))

	// message_stop
	msgStop := map[string]interface{}{"type": "message_stop"}
	ms, _ := json.Marshal(msgStop)
	results = append(results, []byte("data: "+string(ms)+"\n\n"))

	return results
}
