package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"github.com/tidwall/gjson"
)

// claudeStreamState holds state for Gemini→Claude streaming.
type claudeStreamState struct {
	MessageID        string
	Model            string
	ContentBlockIdx  int
	ThinkingBlockIdx int
	ThinkingBlockOpen bool
	ToolBlocks       map[int]int
	ToolNames        map[int]string
	ToolArgsAccum    map[int]*strings.Builder
	TextAccum        strings.Builder
	ThinkingAccum    strings.Builder
	MessageStartSent bool
}

func getStreamState(param *any) *claudeStreamState {
	if *param == nil {
		*param = &claudeStreamState{
			ToolBlocks:    make(map[int]int),
			ToolNames:     make(map[int]string),
			ToolArgsAccum: make(map[int]*strings.Builder),
		}
	}
	return (*param).(*claudeStreamState)
}

func claudeSSE(event map[string]interface{}) []byte {
	b, _ := json.Marshal(event)
	return []byte("data: " + string(b) + "\n\n")
}

// convertGeminiResponseToClaudeStream converts Gemini streaming to Claude Messages format.
func convertGeminiResponseToClaudeStream(_ context.Context, _ string, _, _ []byte, rawChunk []byte, param *any) [][]byte {
	state := getStreamState(param)

	raw := bytes.TrimSpace(rawChunk)
	if len(raw) == 0 {
		return nil
	}

	root := gjson.ParseBytes(raw)

	if state.MessageID == "" {
		state.MessageID = root.Get("id").String()
	}
	if state.Model == "" {
		state.Model = root.Get("model").String()
	}

	var results [][]byte

	// Emit message_start on first chunk
	if !state.MessageStartSent {
		results = append(results, claudeSSE(map[string]interface{}{
			"type": "message_start",
			"message": map[string]interface{}{
				"id":            state.MessageID,
				"type":          "message",
				"role":          "assistant",
				"model":         state.Model,
				"stop_sequence": nil,
				"usage": map[string]interface{}{
					"input_tokens":  0,
					"output_tokens": 0,
				},
			},
		}))
		state.MessageStartSent = true
	}

	if candidates := root.Get("candidates"); candidates.Exists() && candidates.IsArray() {
		candidates.ForEach(func(_, candidate gjson.Result) bool {
			if parts := candidate.Get("content.parts"); parts.Exists() && parts.IsArray() {
				parts.ForEach(func(_, part gjson.Result) bool {
					if text := part.Get("text"); text.Exists() {
						if part.Get("thought").Bool() {
							thinkingText := text.String()
							state.ThinkingAccum.WriteString(thinkingText)

							if !state.ThinkingBlockOpen {
								state.ThinkingBlockIdx = state.ContentBlockIdx
								state.ContentBlockIdx++
								state.ThinkingBlockOpen = true
								results = append(results, claudeSSE(map[string]interface{}{
									"type":          "content_block_start",
									"index":         state.ThinkingBlockIdx,
									"content_block": map[string]interface{}{"type": "thinking", "thinking": ""},
								}))
							}

							results = append(results, claudeSSE(map[string]interface{}{
								"type":  "content_block_delta",
								"index": state.ThinkingBlockIdx,
								"delta": map[string]interface{}{
									"type":     "thinking_delta",
									"thinking": thinkingText,
								},
							}))
						} else {
							if state.ThinkingBlockOpen {
								results = append(results, claudeSSE(map[string]interface{}{
									"type":  "content_block_stop",
									"index": state.ThinkingBlockIdx,
								}))
								state.ThinkingBlockOpen = false
							}

							state.TextAccum.WriteString(text.String())
							idx := state.ContentBlockIdx
							state.ContentBlockIdx++

							results = append(results, claudeSSE(map[string]interface{}{
								"type":          "content_block_start",
								"index":         idx,
								"content_block": map[string]interface{}{"type": "text", "text": ""},
							}))
							results = append(results, claudeSSE(map[string]interface{}{
								"type":  "content_block_delta",
								"index": idx,
								"delta": map[string]interface{}{"type": "text_delta", "text": text.String()},
							}))
							results = append(results, claudeSSE(map[string]interface{}{
								"type":  "content_block_stop",
								"index": idx,
							}))
						}
					}

					if fc := part.Get("functionCall"); fc.Exists() {
						if state.ThinkingBlockOpen {
							results = append(results, claudeSSE(map[string]interface{}{
								"type":  "content_block_stop",
								"index": state.ThinkingBlockIdx,
							}))
							state.ThinkingBlockOpen = false
						}

						name := fc.Get("name").String()
						toolIdx := state.ContentBlockIdx
						state.ToolNames[toolIdx] = name
						state.ToolArgsAccum[toolIdx] = &strings.Builder{}
						state.ContentBlockIdx++

						results = append(results, claudeSSE(map[string]interface{}{
							"type":  "content_block_start",
							"index": toolIdx,
							"content_block": map[string]interface{}{
								"type":  "tool_use",
								"id":    "toolu_" + name,
								"name":  name,
								"input": map[string]interface{}{},
							},
						}))

						args := fc.Get("args").Raw
						if args != "" {
							state.ToolArgsAccum[toolIdx].WriteString(args)
							results = append(results, claudeSSE(map[string]interface{}{
								"type":  "content_block_delta",
								"index": toolIdx,
								"delta": map[string]interface{}{
									"type":         "input_json_delta",
									"partial_json": args,
								},
							}))
						}
					}
					return true
				})
			}
			return true
		})
	}

	return results
}

// convertGeminiResponseToClaudeNonStream converts a complete Gemini response to Claude Messages format.
func convertGeminiResponseToClaudeNonStream(_ context.Context, _ string, _, _ []byte, rawResponse []byte, _ *any) []byte {
	root := gjson.ParseBytes(rawResponse)

	out := make(map[string]interface{})
	out["id"] = root.Get("id").String()
	out["type"] = "message"
	out["role"] = "assistant"
	out["model"] = root.Get("model").String()
	out["stop_sequence"] = nil

	var contentBlocks []map[string]interface{}

	if candidates := root.Get("candidates"); candidates.Exists() && candidates.IsArray() {
		candidates.ForEach(func(_, candidate gjson.Result) bool {
			if parts := candidate.Get("content.parts"); parts.Exists() && parts.IsArray() {
				parts.ForEach(func(_, part gjson.Result) bool {
					if text := part.Get("text"); text.Exists() {
						if part.Get("thought").Bool() {
							thinkingBlock := map[string]interface{}{
								"type":     "thinking",
								"thinking": text.String(),
							}
							sig := part.Get("thoughtSignature")
							if !sig.Exists() {
								sig = part.Get("thought_signature")
							}
							if sig.Exists() && sig.String() != "" {
								thinkingBlock["signature"] = sig.String()
							}
							contentBlocks = append(contentBlocks, thinkingBlock)
						} else {
							contentBlocks = append(contentBlocks, map[string]interface{}{
								"type": "text",
								"text": text.String(),
							})
						}
					}
					if fc := part.Get("functionCall"); fc.Exists() {
						name := fc.Get("name").String()
						args := fc.Get("args").Raw
						var input map[string]interface{}
						if args != "" {
							json.Unmarshal([]byte(args), &input)
						}
						if input == nil {
							input = map[string]interface{}{}
						}
						contentBlocks = append(contentBlocks, map[string]interface{}{
							"type":  "tool_use",
							"id":    "toolu_" + name,
							"name":  name,
							"input": input,
						})
					}
					return true
				})
			}
			return true
		})
	}

	out["content"] = contentBlocks

	stopReason := "end_turn"
	if root.Get("candidates.0.finishReason").String() == "STOP" {
		stopReason = "end_turn"
	} else if root.Get("candidates.0.finishReason").String() == "MAX_TOKENS" {
		stopReason = "max_tokens"
	}
	out["stop_reason"] = stopReason

	usage := map[string]interface{}{
		"input_tokens":  root.Get("usageMetadata.promptTokenCount").Int(),
		"output_tokens": root.Get("usageMetadata.candidatesTokenCount").Int(),
	}
	out["usage"] = usage

	result, _ := json.Marshal(out)
	return result
}
