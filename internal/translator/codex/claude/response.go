package claude

import (
	"bytes"
	"context"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

var dataTag = []byte("data:")

// claudeStreamState holds per-stream state for Claude SSE → Codex Responses SSE.
type claudeStreamState struct {
	MessageID string
	Model     string
	OutputIndex int

	ReasoningOpen      bool
	ReasoningBuf       strings.Builder
	ReasoningSignature string

	ToolCallOpen      bool
	ToolCallID        string
	ToolName          string
	ToolArgs          strings.Builder
	ToolCallAnnounced bool

	TextBlockOpen bool
	BlockTypes    map[int]string
}

func getClaudeState(param *any) *claudeStreamState {
	if *param == nil {
		*param = &claudeStreamState{
			BlockTypes: make(map[int]string),
		}
	}
	return (*param).(*claudeStreamState)
}

func (s *claudeStreamState) blockType(idx int) string {
	if s == nil || s.BlockTypes == nil {
		return ""
	}
	return s.BlockTypes[idx]
}

func (s *claudeStreamState) setBlockType(idx int, t string) {
	if s == nil {
		return
	}
	if s.BlockTypes == nil {
		s.BlockTypes = make(map[int]string)
	}
	s.BlockTypes[idx] = t
}

// convertClaudeResponseToCodexStream converts Claude streaming events to Codex
// Responses SSE events. Each emitted chunk is formatted as
// `data: {"type":"..."}\n\n`.
func convertClaudeResponseToCodexStream(_ context.Context, _ string, _, _ []byte, rawChunk []byte, param *any) [][]byte {
	state := getClaudeState(param)

	raw := bytes.TrimSpace(rawChunk)
	if bytes.HasPrefix(raw, dataTag) {
		raw = bytes.TrimSpace(raw[5:])
	}
	if len(raw) == 0 {
		return nil
	}

	root := gjson.ParseBytes(raw)
	eventType := root.Get("type").String()

	var output []byte

	flushReasoning := func() {
		if !state.ReasoningOpen {
			return
		}
		partDone := []byte(`{"type":"response.reasoning_summary_part.done"}`)
		output = append(output, sse(partDone)...)
		state.ReasoningOpen = false
	}

	flushToolCall := func() {
		if !state.ToolCallOpen {
			return
		}
		item := map[string]any{
			"type":         "response.output_item.done",
			"output_index": state.OutputIndex,
			"item": map[string]any{
				"type":      "function_call",
				"id":        state.ToolCallID,
				"call_id":   state.ToolCallID,
				"name":      state.ToolName,
				"arguments": state.ToolArgs.String(),
				"status":    "completed",
			},
		}
		output = append(output, sse(mustMarshal(item))...)
		state.OutputIndex++
		state.ToolCallOpen = false
		state.ToolCallID = ""
		state.ToolName = ""
		state.ToolArgs.Reset()
		state.ToolCallAnnounced = false
	}

	flushTextBlock := func() {
		if !state.TextBlockOpen {
			return
		}
		state.TextBlockOpen = false
	}

	switch eventType {
	case "message_start":
		state.MessageID = root.Get("message.id").String()
		if state.Model == "" {
			state.Model = root.Get("message.model").String()
		}
		created := map[string]any{
			"type": "response.created",
			"response": map[string]any{
				"id":         state.MessageID,
				"model":      state.Model,
				"object":     "response",
				"created_at": root.Get("message.created_at").Int(),
			},
		}
		output = append(output, sse(mustMarshal(created))...)

	case "content_block_start":
		idx := int(root.Get("index").Int())
		blockType := root.Get("content_block.type").String()
		state.setBlockType(idx, blockType)
		switch blockType {
		case "text":
			flushReasoning()
			flushToolCall()
			state.TextBlockOpen = true
		case "thinking":
			flushReasoning()
			flushToolCall()
			flushTextBlock()
			state.ReasoningOpen = true
			state.ReasoningBuf.Reset()
			state.ReasoningSignature = ""
			added := map[string]any{
				"type": "response.reasoning_summary_part.added",
				"item": map[string]any{"type": "reasoning", "summary": []any{}},
			}
			output = append(output, sse(mustMarshal(added))...)
		case "tool_use":
			flushReasoning()
			flushTextBlock()
			flushToolCall()
			state.ToolCallOpen = true
			state.ToolCallID = root.Get("content_block.id").String()
			state.ToolName = root.Get("content_block.name").String()
			state.ToolArgs.Reset()
			state.ToolCallAnnounced = true
			added := map[string]any{
				"type":         "response.output_item.added",
				"output_index": state.OutputIndex,
				"item": map[string]any{
					"type":      "function_call",
					"id":        state.ToolCallID,
					"call_id":   state.ToolCallID,
					"name":      state.ToolName,
					"arguments": "",
				},
			}
			output = append(output, sse(mustMarshal(added))...)
		case "server_tool_use":
			flushReasoning()
			flushToolCall()
			flushTextBlock()
			id := root.Get("content_block.id").String()
			if id == "" {
				id = "web_search_" + strconv.Itoa(state.OutputIndex)
			}
			query := ""
			if input := root.Get("content_block.input"); input.Exists() && input.IsObject() {
				query = input.Get("query").String()
			}
			added := map[string]any{
				"type":         "response.output_item.added",
				"output_index": state.OutputIndex,
				"item": map[string]any{
					"type":   "web_search_call",
					"id":     id,
					"action": "web_search",
					"query":  query,
				},
			}
			output = append(output, sse(mustMarshal(added))...)
			state.OutputIndex++
		}

	case "content_block_delta":
		deltaType := root.Get("delta.type").String()
		switch deltaType {
		case "text_delta":
			delta := map[string]any{
				"type":  "response.output_text.delta",
				"delta": root.Get("delta.text").String(),
			}
			output = append(output, sse(mustMarshal(delta))...)
		case "thinking_delta":
			if !state.ReasoningOpen {
				state.ReasoningOpen = true
				added := map[string]any{
					"type": "response.reasoning_summary_part.added",
					"item": map[string]any{"type": "reasoning", "summary": []any{}},
				}
				output = append(output, sse(mustMarshal(added))...)
			}
			state.ReasoningBuf.WriteString(root.Get("delta.thinking").String())
			delta := map[string]any{
				"type":  "response.reasoning_summary_text.delta",
				"delta": root.Get("delta.thinking").String(),
			}
			output = append(output, sse(mustMarshal(delta))...)
		case "signature_delta":
			state.ReasoningSignature = root.Get("delta.signature").String()
		case "input_json_delta":
			partial := root.Get("delta.partial_json").String()
			state.ToolArgs.WriteString(partial)
			if state.ToolCallAnnounced {
				delta := map[string]any{
					"type":         "response.function_call_arguments.delta",
					"output_index": state.OutputIndex,
					"delta":        partial,
				}
				output = append(output, sse(mustMarshal(delta))...)
			}
		}

	case "content_block_stop":
		idx := int(root.Get("index").Int())
		blockType := state.blockType(idx)
		delete(state.BlockTypes, idx)
		switch blockType {
		case "text":
			flushTextBlock()
		case "thinking":
			flushReasoning()
		case "tool_use":
			flushToolCall()
		case "server_tool_use":
			done := map[string]any{
				"type":         "response.output_item.done",
				"output_index": state.OutputIndex - 1,
				"item": map[string]any{
					"type":   "web_search_call",
					"id":     "web_search_" + strconv.Itoa(state.OutputIndex-1),
					"action": "web_search",
				},
			}
			output = append(output, sse(mustMarshal(done))...)
		}

	case "message_delta":
		flushReasoning()
		flushToolCall()
		flushTextBlock()

		stopReason := mapClaudeStopReasonToCodex(root.Get("delta.stop_reason").String())
		completed := map[string]any{
			"type": "response.completed",
			"response": map[string]any{
				"id":          state.MessageID,
				"model":       state.Model,
				"object":      "response",
				"status":      "completed",
				"stop_reason": stopReason,
				"output":      []any{},
			},
		}
		if usage := root.Get("usage"); usage.Exists() {
			completed["response"].(map[string]any)["usage"] = map[string]any{
				"input_tokens":  usage.Get("input_tokens").Int(),
				"output_tokens": usage.Get("output_tokens").Int(),
			}
		}
		if sig := state.ReasoningSignature; sig != "" || state.ReasoningBuf.Len() > 0 {
			outputItems := completed["response"].(map[string]any)["output"].([]any)
			summary := []map[string]any{}
			if state.ReasoningBuf.Len() > 0 {
				summary = append(summary, map[string]any{
					"type": "summary_text",
					"text": state.ReasoningBuf.String(),
				})
			}
			reasoningItem := map[string]any{
				"type":    "reasoning",
				"summary": summary,
			}
			if sig != "" {
				reasoningItem["encrypted_content"] = sig
			}
			completed["response"].(map[string]any)["output"] = append(outputItems, reasoningItem)
		}
		output = append(output, sse(mustMarshal(completed))...)

	case "message_stop":
		// message_stop carries no extra metadata; message_delta already emitted
		// response.completed. Finalize open blocks defensively.
		flushReasoning()
		flushToolCall()
		flushTextBlock()
	}

	if len(output) == 0 {
		return nil
	}
	return [][]byte{output}
}

// convertClaudeResponseToCodexNonStream converts a complete Claude Messages
// response JSON to a Codex Responses JSON.
func convertClaudeResponseToCodexNonStream(_ context.Context, _ string, _, _ []byte, rawResponse []byte, _ *any) []byte {
	root := gjson.ParseBytes(rawResponse)
	if root.Get("type").String() != "message" {
		return []byte{}
	}

	out := []byte(`{"id":"","object":"response","model":"","output":[],"stop_reason":null,"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}}`)
	out, _ = sjson.SetBytes(out, "id", root.Get("id").String())
	out, _ = sjson.SetBytes(out, "model", root.Get("model").String())

	stopReason := mapClaudeStopReasonToCodex(root.Get("stop_reason").String())
	out, _ = sjson.SetBytes(out, "stop_reason", stopReason)

	inputTokens := root.Get("usage.input_tokens").Int()
	outputTokens := root.Get("usage.output_tokens").Int()
	cachedTokens := root.Get("usage.cache_read_input_tokens").Int()
	out, _ = sjson.SetBytes(out, "usage.input_tokens", inputTokens)
	out, _ = sjson.SetBytes(out, "usage.output_tokens", outputTokens)
	out, _ = sjson.SetBytes(out, "usage.total_tokens", inputTokens+outputTokens)
	if cachedTokens > 0 {
		out, _ = sjson.SetBytes(out, "usage.input_tokens_details.cached_tokens", cachedTokens)
	}

	content := root.Get("content")
	if !content.Exists() || !content.IsArray() {
		return out
	}

	var textParts []string
	flushText := func() {
		if len(textParts) == 0 {
			return
		}
		item := map[string]any{
			"type": "message",
			"content": []map[string]any{{
				"type": "output_text",
				"text": strings.Join(textParts, ""),
			}},
		}
		out, _ = sjson.SetRawBytes(out, "output.-1", mustMarshal(item))
		textParts = textParts[:0]
	}

	content.ForEach(func(_, block gjson.Result) bool {
		bType := block.Get("type").String()
		switch bType {
		case "text":
			if s := block.Get("text").String(); s != "" {
				textParts = append(textParts, s)
			}
		case "thinking":
			flushText()
			summary := []map[string]any{}
			if s := block.Get("thinking").String(); s != "" {
				summary = append(summary, map[string]any{"type": "summary_text", "text": s})
			}
			item := map[string]any{
				"type":    "reasoning",
				"summary": summary,
			}
			if sig := block.Get("signature").String(); sig != "" {
				item["encrypted_content"] = sig
			}
			out, _ = sjson.SetRawBytes(out, "output.-1", mustMarshal(item))
		case "tool_use":
			flushText()
			argsRaw := block.Get("input").Raw
			if argsRaw == "" || argsRaw == "null" {
				argsRaw = "{}"
			}
			item := map[string]any{
				"type":      "function_call",
				"id":        block.Get("id").String(),
				"call_id":   block.Get("id").String(),
				"name":      block.Get("name").String(),
				"arguments": argsRaw,
				"status":    "completed",
			}
			out, _ = sjson.SetRawBytes(out, "output.-1", mustMarshal(item))
		case "server_tool_use":
			flushText()
			id := block.Get("id").String()
			query := ""
			if input := block.Get("input"); input.Exists() && input.IsObject() {
				query = input.Get("query").String()
			}
			item := map[string]any{
				"type":   "web_search_call",
				"id":     id,
				"action": "web_search",
				"query":  query,
			}
			out, _ = sjson.SetRawBytes(out, "output.-1", mustMarshal(item))
		}
		return true
	})
	flushText()

	return out
}

func sse(data []byte) []byte {
	return []byte("data: " + string(data) + "\n\n")
}

func mapClaudeStopReasonToCodex(stopReason string) string {
	switch stopReason {
	case "end_turn":
		return "stop"
	case "tool_use":
		return "tool_use"
	case "max_tokens":
		return "max_tokens"
	case "stop_sequence":
		return "stop_sequence"
	case "content_filter", "refusal":
		return "content_filter"
	case "model_context_window_exceeded":
		return "max_tokens"
	default:
		if stopReason != "" {
			return stopReason
		}
		return "stop"
	}
}
