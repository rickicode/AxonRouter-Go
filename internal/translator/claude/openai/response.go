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
	MessageID            string
	Model                string
	CreatedAt            int64
	ToolNameMap          map[string]string
	ContentBlockStarted  bool
	ContentBlockIndex    int
	ThinkingBlockStarted bool
	ThinkingBlockIndex   int
	ToolBlocks           map[int]int
	ToolStartEmitted     map[int]bool
	ToolArgsAccum        map[int]*strings.Builder
	TextAccum            strings.Builder
	ThinkingAccum        strings.Builder
	FinishReason         string
	MessageStartSent     bool
	MessageStopSent      bool
	SawToolCall          bool
	NextContentBlockIndex int
	InputTokens          int64
	OutputTokens         int64
	CachedTokens         int64
}

var dataTag = []byte("data:")

// Static JSON event structs eliminate per-event map allocations while keeping
// the exact byte ordering the original map[string]interface{} + json.Marshal
// produced. Fields are declared in alphabetical order so marshalling output
// matches the prior map order.

type claudeUsage struct {
	CacheReadInputTokens int64 `json:"cache_read_input_tokens,omitempty"`
	InputTokens          int64 `json:"input_tokens"`
	OutputTokens         int64 `json:"output_tokens"`
}

type claudeContentBlock struct {
	ID       string         `json:"id,omitempty"`
	Input    map[string]any `json:"input,omitempty"`
	Name     string         `json:"name,omitempty"`
	Text     string         `json:"text,omitempty"`
	Thinking string         `json:"thinking,omitempty"`
	Type     string         `json:"type"`
}

type claudeContentDelta struct {
	PartialJSON string `json:"partial_json,omitempty"`
	Text        string `json:"text,omitempty"`
	Thinking    string `json:"thinking,omitempty"`
	Type        string `json:"type"`
}

type messageStartMessage struct {
	Content      []any     `json:"content"`
	ID           string    `json:"id"`
	Model        string    `json:"model"`
	Role         string    `json:"role"`
	StopReason   any       `json:"stop_reason"`
	StopSequence any       `json:"stop_sequence"`
	Type         string    `json:"type"`
	Usage        claudeUsage `json:"usage"`
}

type messageStartEvent struct {
	Message messageStartMessage `json:"message"`
	Type    string              `json:"type"`
}

type contentBlockStartEvent struct {
	ContentBlock claudeContentBlock `json:"content_block"`
	Index        int                `json:"index"`
	Type         string             `json:"type"`
}

type contentBlockDeltaEvent struct {
	Delta claudeContentDelta `json:"delta"`
	Index int                `json:"index"`
	Type  string             `json:"type"`
}

type contentBlockStopEvent struct {
	Index int    `json:"index"`
	Type  string `json:"type"`
}

type messageDeltaInner struct {
	StopReason string      `json:"stop_reason"`
	Usage      claudeUsage `json:"usage"`
}

type messageDeltaEvent struct {
	Delta messageDeltaInner `json:"delta"`
	Type  string            `json:"type"`
}

type messageStopEvent struct {
	Type string `json:"type"`
}

type claudeNonStream struct {
	Content    []claudeContentBlock `json:"content"`
	ID         string               `json:"id"`
	Model      string               `json:"model"`
	Role       string               `json:"role"`
	StopReason string               `json:"stop_reason"`
	Type       string               `json:"type"`
	Usage      *claudeUsage         `json:"usage,omitempty"`
}

func wrapEvent(b []byte) []byte {
	event := make([]byte, 0, len(dataTag)+1+len(b)+2)
	event = append(event, dataTag...)
	event = append(event, ' ')
	event = append(event, b...)
	event = append(event, '\n', '\n')
	return event
}

func getState(param *any) *claudeStreamState {
	if *param == nil {
		*param = &claudeStreamState{
			ToolBlocks:       make(map[int]int),
			ToolStartEmitted: make(map[int]bool),
			ToolArgsAccum:    make(map[int]*strings.Builder),
			NextContentBlockIndex: 0,
		}
	}
	return (*param).(*claudeStreamState)
}

// ConvertOpenAIResponseToClaudeStream converts OpenAI streaming chunks to Claude SSE events.
func ConvertOpenAIResponseToClaudeStream(_ context.Context, _ string, _, _ []byte, rawChunk []byte, param *any) [][]byte {
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

	// Capture usage if present (OpenAI may include it in any chunk, often the last).
	if usage := root.Get("usage"); usage.Exists() {
		if pt := usage.Get("prompt_tokens"); pt.Exists() {
			state.InputTokens = pt.Int()
		}
		if ct := usage.Get("completion_tokens"); ct.Exists() {
			state.OutputTokens = ct.Int()
		}
		if dt := usage.Get("prompt_tokens_details.cached_tokens"); dt.Exists() {
			state.CachedTokens = dt.Int()
		}
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

// ConvertOpenAIResponseToClaudeNonStream converts a complete OpenAI response to Claude format.
func ConvertOpenAIResponseToClaudeNonStream(_ context.Context, _ string, _, _ []byte, rawResponse []byte, _ *any) []byte {
	root := gjson.ParseBytes(rawResponse)
	out := claudeNonStream{
		ID:    root.Get("id").String(),
		Type:  "message",
		Role:  "assistant",
		Model: root.Get("model").String(),
	}

	if choices := root.Get("choices"); choices.Exists() && choices.IsArray() {
		choices.ForEach(func(_, choice gjson.Result) bool {
			msg := choice.Get("message")

			if reasoningContent := msg.Get("reasoning_content"); reasoningContent.Exists() && reasoningContent.String() != "" {
				out.Content = append(out.Content, claudeContentBlock{
					Type:     "thinking",
					Thinking: reasoningContent.String(),
				})
			}
			if text := msg.Get("content"); text.Exists() && text.String() != "" {
				out.Content = append(out.Content, claudeContentBlock{
					Type: "text",
					Text: text.String(),
				})
			}
			if toolCalls := msg.Get("tool_calls"); toolCalls.Exists() && toolCalls.IsArray() {
				toolCalls.ForEach(func(_, tc gjson.Result) bool {
					var raw interface{}
					argsJSON := tc.Get("function.arguments").String()
					if err := json.Unmarshal([]byte(argsJSON), &raw); err != nil || raw == nil {
						raw = map[string]interface{}{}
					}
					input, _ := raw.(map[string]any)
					if input == nil {
						input = map[string]any{}
					}
					out.Content = append(out.Content, claudeContentBlock{
						Type:  "tool_use",
						ID:    tc.Get("id").String(),
						Name:  tc.Get("function.name").String(),
						Input: input,
					})
					return true
				})
			}

			finish := choice.Get("finish_reason").String()
			switch finish {
			case "stop":
				out.StopReason = "end_turn"
			case "tool_calls":
				out.StopReason = "tool_use"
			case "length":
				out.StopReason = "max_tokens"
			default:
				out.StopReason = "end_turn"
			}
			return true
		})
	}

	if usage := root.Get("usage"); usage.Exists() {
		u := claudeUsage{
			InputTokens:  usage.Get("prompt_tokens").Int(),
			OutputTokens: usage.Get("completion_tokens").Int(),
		}
		if dt := usage.Get("prompt_tokens_details.cached_tokens"); dt.Exists() && dt.Int() > 0 {
			u.CacheReadInputTokens = dt.Int()
		}
		out.Usage = &u
	}

	result, _ := json.Marshal(out)
	return result
}

func buildClaudeMessageStart(state *claudeStreamState) []byte {
	event := messageStartEvent{
		Type: "message_start",
		Message: messageStartMessage{
			ID:           state.MessageID,
			Type:         "message",
			Role:         "assistant",
			Model:        state.Model,
			Content:      []any{},
			StopReason:   nil,
			StopSequence: nil,
			Usage:        claudeUsage{InputTokens: 0, OutputTokens: 0},
		},
	}
	b, _ := json.Marshal(event)
	return wrapEvent(b)
}

func buildClaudeContentBlockStart(state *claudeStreamState, blockType string) []byte {
	block := claudeContentBlock{Type: blockType}
	if blockType == "text" {
		block.Text = ""
	} else if blockType == "thinking" {
		block.Thinking = ""
	}
	event := contentBlockStartEvent{
		Type:         "content_block_start",
		Index:        state.NextContentBlockIndex - 1,
		ContentBlock: block,
	}
	b, _ := json.Marshal(event)
	return wrapEvent(b)
}

func buildClaudeTextDelta(index int, text string) []byte {
	event := contentBlockDeltaEvent{
		Type:  "content_block_delta",
		Index: index,
		Delta: claudeContentDelta{Type: "text_delta", Text: text},
	}
	b, _ := json.Marshal(event)
	return wrapEvent(b)
}

func buildClaudeThinkingDelta(index int, thinking string) []byte {
	event := contentBlockDeltaEvent{
		Type:  "content_block_delta",
		Index: index,
		Delta: claudeContentDelta{Type: "thinking_delta", Thinking: thinking},
	}
	b, _ := json.Marshal(event)
	return wrapEvent(b)
}

func buildClaudeContentBlockStop(index int) []byte {
	event := contentBlockStopEvent{Type: "content_block_stop", Index: index}
	b, _ := json.Marshal(event)
	return wrapEvent(b)
}

func buildClaudeToolUseStart(state *claudeStreamState, idx int, id, name string) []byte {
	blockIndex := state.NextContentBlockIndex
	state.ToolBlocks[idx] = blockIndex
	state.NextContentBlockIndex++
	event := contentBlockStartEvent{
		Type:  "content_block_start",
		Index: blockIndex,
		ContentBlock: claudeContentBlock{
			Type:  "tool_use",
			ID:    id,
			Name:  name,
			Input: map[string]any{},
		},
	}
	b, _ := json.Marshal(event)
	return wrapEvent(b)
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
			event := contentBlockDeltaEvent{
				Type:  "content_block_delta",
				Index: blockIndex,
				Delta: claudeContentDelta{Type: "input_json_delta", PartialJSON: acc.String()},
			}
			d, _ := json.Marshal(event)
			results = append(results, wrapEvent(d))
		}
		results = append(results, buildClaudeContentBlockStop(blockIndex))
	}

	// message_delta with stop_reason + usage
	stopReason := "end_turn"
	if state.SawToolCall || state.FinishReason == "tool_calls" {
		stopReason = "tool_use"
	}
	usage := claudeUsage{InputTokens: state.InputTokens, OutputTokens: state.OutputTokens}
	if state.CachedTokens > 0 {
		usage.CacheReadInputTokens = state.CachedTokens
	}
	msgDelta := messageDeltaEvent{
		Type: "message_delta",
		Delta: messageDeltaInner{
			StopReason: stopReason,
			Usage:      usage,
		},
	}
	md, _ := json.Marshal(msgDelta)
	results = append(results, wrapEvent(md))

	// message_stop
	ms, _ := json.Marshal(messageStopEvent{Type: "message_stop"})
	results = append(results, wrapEvent(ms))

	return results
}
