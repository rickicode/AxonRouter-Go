package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
)

// codexStreamState holds state for Gemini→Codex Responses streaming.
type codexStreamState struct {
	MessageID     string
	Model         string
	OutputIndex   int
	ToolIndex     int
	ToolArgsAcc   map[int]*strings.Builder
	ToolNames     map[int]string
	ContentAcc    strings.Builder
	ReasoningOpen bool
	ReasoningAcc  strings.Builder
}

func getCodexState(param *any) *codexStreamState {
	if *param == nil {
		*param = &codexStreamState{
			ToolArgsAcc: make(map[int]*strings.Builder),
			ToolNames:   make(map[int]string),
		}
	}
	return (*param).(*codexStreamState)
}

func (s *codexStreamState) reasoningItemID() string {
	if s.MessageID != "" {
		return "rs_" + s.MessageID
	}
	return "rs_gemini"
}

func (s *codexStreamState) finalizeReasoning() [][]byte {
	if !s.ReasoningOpen {
		return nil
	}
	text := s.ReasoningAcc.String()
	s.ReasoningAcc.Reset()
	s.ReasoningOpen = false

	itemID := s.reasoningItemID()
	outputIndex := s.OutputIndex
	s.OutputIndex++

	item := map[string]interface{}{
		"type": "reasoning",
		"id":   itemID,
		"summary": []map[string]interface{}{
			{"type": "summary_text", "text": text},
		},
	}

	var results [][]byte

	textDone := map[string]interface{}{
		"type":          "response.reasoning_summary_text.done",
		"item_id":       itemID,
		"output_index":  outputIndex,
		"summary_index": 0,
		"text":          text,
	}
	b, _ := json.Marshal(textDone)
	results = append(results, b)

	partDone := map[string]interface{}{
		"type":          "response.reasoning_summary_part.done",
		"item_id":       itemID,
		"output_index":  outputIndex,
		"summary_index": 0,
		"part": map[string]interface{}{
			"type": "summary_text",
			"text": text,
		},
	}
	b, _ = json.Marshal(partDone)
	results = append(results, b)

	outputItemDone := map[string]interface{}{
		"type":         "response.output_item.done",
		"output_index": outputIndex,
		"item":         item,
	}
	b, _ = json.Marshal(outputItemDone)
	results = append(results, b)

	return results
}

// convertGeminiResponseToCodexStream converts Gemini streaming to Codex Responses format.
func convertGeminiResponseToCodexStream(_ context.Context, _ string, _, _ []byte, rawChunk []byte, param *any) [][]byte {
	state := getCodexState(param)

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

	if candidates := root.Get("candidates"); candidates.Exists() && candidates.IsArray() {
		candidates.ForEach(func(_, candidate gjson.Result) bool {
			if parts := candidate.Get("content.parts"); parts.Exists() && parts.IsArray() {
				parts.ForEach(func(_, part gjson.Result) bool {
					if text := part.Get("text"); text.Exists() {
						if part.Get("thought").Bool() {
							if !state.ReasoningOpen {
								state.ReasoningOpen = true
								itemID := state.reasoningItemID()
								added := map[string]interface{}{
									"type":          "response.reasoning_summary_part.added",
									"item_id":       itemID,
									"output_index":  state.OutputIndex,
									"summary_index": 0,
									"part": map[string]interface{}{
										"type": "summary_text",
										"text": "",
									},
								}
								b, _ := json.Marshal(added)
								results = append(results, b)
							}
							state.ReasoningAcc.WriteString(text.String())
							delta := map[string]interface{}{
								"type":          "response.reasoning_summary_text.delta",
								"item_id":       state.reasoningItemID(),
								"output_index":  state.OutputIndex,
								"summary_index": 0,
								"delta":         text.String(),
							}
							b, _ := json.Marshal(delta)
							results = append(results, b)
							return true
						}

						if reasoningEvents := state.finalizeReasoning(); len(reasoningEvents) > 0 {
							results = append(results, reasoningEvents...)
						}
						state.ContentAcc.WriteString(text.String())
						out := map[string]interface{}{
							"type":  "response.output_text.delta",
							"delta": text.String(),
						}
						b, _ := json.Marshal(out)
						results = append(results, b)
						return true
					}

					if fc := part.Get("functionCall"); fc.Exists() {
						if reasoningEvents := state.finalizeReasoning(); len(reasoningEvents) > 0 {
							results = append(results, reasoningEvents...)
						}
						name := fc.Get("name").String()
						callID := fc.Get("id").String()
						if callID == "" {
							callID = fmt.Sprintf("call_%s_%d", name, state.ToolIndex)
						}
						args := fc.Get("args").Raw
						if args == "" || args == "{}" {
							args = "{}"
						}

						item := map[string]interface{}{
							"type": "response.output_item.done",
							"item": map[string]interface{}{
								"type":      "function_call",
								"id":        callID,
								"call_id":   callID,
								"name":      name,
								"arguments": args,
								"status":    "completed",
							},
						}
						state.ToolIndex++
						state.OutputIndex++
						b, _ := json.Marshal(item)
						results = append(results, b)
						return true
					}
					return true
				})
			}
			return true
		})
	}

	// Check if done
	if root.Get("candidates.0.finishReason").String() != "" {
		if reasoningEvents := state.finalizeReasoning(); len(reasoningEvents) > 0 {
			results = append(results, reasoningEvents...)
		}
		completed := map[string]interface{}{
			"type": "response.completed",
		}
		b, _ := json.Marshal(completed)
		results = append(results, b)
	}

	if len(results) > 0 {
		return results
	}
	return nil
}

// convertGeminiResponseToCodexNonStream converts a complete Gemini response to Codex Responses format.
func convertGeminiResponseToCodexNonStream(_ context.Context, _ string, _, _ []byte, rawResponse []byte, _ *any) []byte {
	root := gjson.ParseBytes(rawResponse)

	out := make(map[string]interface{})
	out["id"] = root.Get("id").String()
	out["object"] = "response"
	out["model"] = root.Get("model").String()

	responseID := root.Get("id").String()
	if responseID == "" {
		responseID = "gemini"
	}

	var outputItems []map[string]interface{}
	var textParts []string
	var reasoningParts []string
	toolIdx := 0

	flushText := func() {
		if len(textParts) == 0 {
			return
		}
		outputItems = append(outputItems, map[string]interface{}{
			"type": "message",
			"content": []map[string]interface{}{{
				"type": "output_text",
				"text": strings.Join(textParts, ""),
			}},
		})
		textParts = nil
	}
	flushReasoning := func() {
		if len(reasoningParts) == 0 {
			return
		}
		outputItems = append(outputItems, map[string]interface{}{
			"type": "reasoning",
			"id":   fmt.Sprintf("rs_%s_%d", responseID, len(outputItems)),
			"summary": []map[string]interface{}{{
				"type": "summary_text",
				"text": strings.Join(reasoningParts, ""),
			}},
		})
		reasoningParts = nil
	}

	if candidates := root.Get("candidates"); candidates.Exists() && candidates.IsArray() {
		candidates.ForEach(func(_, candidate gjson.Result) bool {
			if parts := candidate.Get("content.parts"); parts.Exists() && parts.IsArray() {
				parts.ForEach(func(_, part gjson.Result) bool {
					if text := part.Get("text"); text.Exists() {
						if part.Get("thought").Bool() {
							flushText()
							reasoningParts = append(reasoningParts, text.String())
						} else {
							flushReasoning()
							textParts = append(textParts, text.String())
						}
					}
					if fc := part.Get("functionCall"); fc.Exists() {
						flushText()
						flushReasoning()
						name := fc.Get("name").String()
						callID := fc.Get("id").String()
						if callID == "" {
							callID = fmt.Sprintf("call_%s_%d", name, toolIdx)
						}
						toolIdx++
						args := fc.Get("args").Raw
						if args == "" || args == "{}" {
							args = "{}"
						}
						outputItems = append(outputItems, map[string]interface{}{
							"type":      "function_call",
							"id":        callID,
							"call_id":   callID,
							"name":      name,
							"arguments": args,
							"status":    "completed",
						})
					}
					return true
				})
			}
			return true
		})
	}

	flushText()
	flushReasoning()

	out["output"] = outputItems

	// Usage metadata
	usage := map[string]interface{}{
		"input_tokens":    root.Get("usageMetadata.promptTokenCount").Int(),
		"output_tokens":   root.Get("usageMetadata.candidatesTokenCount").Int(),
		"total_tokens":    root.Get("usageMetadata.totalTokenCount").Int(),
		"cached_tokens":   root.Get("usageMetadata.cachedContentTokenCount").Int(),
		"reasoning_tokens": root.Get("usageMetadata.thoughtsTokenCount").Int(),
	}
	out["usage"] = usage

	result, _ := json.Marshal(out)
	return result
}
