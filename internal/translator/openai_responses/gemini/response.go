package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

var dataTag = []byte("data:")

// geminiToResponsesState accumulates Gemini streaming chunks and rebuilds the
// final OpenAI Responses API output array.
type geminiToResponsesState struct {
	ResponseID   string
	CreatedAt    int64
	Model        string
	OutputItems  []map[string]interface{}
	TextAcc      strings.Builder
	ReasoningAcc strings.Builder
	ToolArgsAcc  map[int]*strings.Builder
	ToolNames    map[int]string
	ToolCallIDs  map[int]string
	ToolIndex    int
	OutputIndex  int
	DoneSent     bool
}

func getGeminiToResponsesState(param *any) *geminiToResponsesState {
	if *param == nil {
		*param = &geminiToResponsesState{
			ToolArgsAcc: make(map[int]*strings.Builder),
			ToolNames:   make(map[int]string),
			ToolCallIDs: make(map[int]string),
		}
	}
	return (*param).(*geminiToResponsesState)
}

func (s *geminiToResponsesState) nextCallID() string {
	s.ToolIndex++
	return fmt.Sprintf("call_%s_%d", s.ResponseID, s.ToolIndex)
}

func (s *geminiToResponsesState) flushText() map[string]interface{} {
	if s.TextAcc.Len() == 0 {
		return nil
	}
	text := s.TextAcc.String()
	s.TextAcc.Reset()
	return map[string]interface{}{
		"type":    "message",
		"role":    "assistant",
		"content": []map[string]interface{}{{"type": "output_text", "text": text}},
	}
}

func (s *geminiToResponsesState) flushReasoning() map[string]interface{} {
	if s.ReasoningAcc.Len() == 0 {
		return nil
	}
	text := s.ReasoningAcc.String()
	s.ReasoningAcc.Reset()
	return map[string]interface{}{
		"type": "reasoning",
		"id":   fmt.Sprintf("rs_%s_%d", s.ResponseID, s.OutputIndex),
		"summary": []map[string]interface{}{
			{"type": "summary_text", "text": text},
		},
	}
}

func (s *geminiToResponsesState) flushToolCall(idx int) map[string]interface{} {
	acc := s.ToolArgsAcc[idx]
	if acc == nil {
		return nil
	}
	args := acc.String()
	item := map[string]interface{}{
		"type":      "function_call",
		"id":        s.ToolCallIDs[idx],
		"call_id":   s.ToolCallIDs[idx],
		"name":      s.ToolNames[idx],
		"arguments": args,
	}
	delete(s.ToolArgsAcc, idx)
	delete(s.ToolNames, idx)
	delete(s.ToolCallIDs, idx)
	return item
}

func (s *geminiToResponsesState) appendItem(item map[string]interface{}) {
	if item == nil {
		return
	}
	item["output_index"] = s.OutputIndex
	s.OutputIndex++
	s.OutputItems = append(s.OutputItems, item)
}

func buildResponsesEvent(state *geminiToResponsesState, eventType string, body map[string]interface{}) []byte {
	event := map[string]interface{}{
		"type": eventType,
	}
	if eventType == "response.completed" {
		response := map[string]interface{}{
			"id":         state.ResponseID,
			"object":     "response",
			"created_at": state.CreatedAt,
			"model":      state.Model,
		}
		for k, v := range body {
			response[k] = v
		}
		event["response"] = response
	} else {
		event["response"] = map[string]interface{}{
			"id":         state.ResponseID,
			"object":     "response",
			"created_at": state.CreatedAt,
			"model":      state.Model,
		}
		for k, v := range body {
			event[k] = v
		}
	}
	b, _ := json.Marshal(event)
	return []byte(fmt.Sprintf("data: %s\n\n", string(b)))
}

func buildResponsesCompleted(state *geminiToResponsesState) []byte {
	response := map[string]interface{}{
		"id":         state.ResponseID,
		"object":     "response",
		"created_at": state.CreatedAt,
		"model":      state.Model,
		"status":     "completed",
		"output":     state.OutputItems,
	}
	event := map[string]interface{}{
		"type":     "response.completed",
		"response": response,
	}
	b, _ := json.Marshal(event)
	return []byte(fmt.Sprintf("data: %s\n\n", string(b)))
}

func buildResponsesUsageEvent(state *geminiToResponsesState, usage gjson.Result) []byte {
	response := map[string]interface{}{
		"id":         state.ResponseID,
		"object":     "response",
		"created_at": state.CreatedAt,
		"model":      state.Model,
		"status":     "completed",
		"output":     state.OutputItems,
	}
	response = setUsageMap(response, usage)
	event := map[string]interface{}{
		"type":     "response.completed",
		"response": response,
	}
	b, _ := json.Marshal(event)
	return []byte(fmt.Sprintf("data: %s\n\n", string(b)))
}

func setUsage(event []byte, usage gjson.Result) []byte {
	out, _ := sjson.SetBytes(event, "response.usage.input_tokens", usage.Get("promptTokenCount").Int())
	out, _ = sjson.SetBytes(out, "response.usage.output_tokens", usage.Get("candidatesTokenCount").Int())
	out, _ = sjson.SetBytes(out, "response.usage.total_tokens", usage.Get("totalTokenCount").Int())
	if thoughts := usage.Get("thoughtsTokenCount"); thoughts.Exists() && thoughts.Int() > 0 {
		out, _ = sjson.SetBytes(out, "response.usage.output_tokens_details.reasoning_tokens", thoughts.Int())
	}
	if cached := usage.Get("cachedContentTokenCount"); cached.Exists() && cached.Int() > 0 {
		out, _ = sjson.SetBytes(out, "response.usage.input_tokens_details.cached_tokens", cached.Int())
	}
	return out
}

func setUsageMap(response map[string]interface{}, usage gjson.Result) map[string]interface{} {
	response["usage"] = map[string]interface{}{
		"input_tokens":  usage.Get("promptTokenCount").Int(),
		"output_tokens": usage.Get("candidatesTokenCount").Int(),
		"total_tokens":  usage.Get("totalTokenCount").Int(),
	}
	usageMap := response["usage"].(map[string]interface{})
	if thoughts := usage.Get("thoughtsTokenCount"); thoughts.Exists() && thoughts.Int() > 0 {
		usageMap["output_tokens_details"] = map[string]interface{}{
			"reasoning_tokens": thoughts.Int(),
		}
	}
	if cached := usage.Get("cachedContentTokenCount"); cached.Exists() && cached.Int() > 0 {
		usageMap["input_tokens_details"] = map[string]interface{}{
			"cached_tokens": cached.Int(),
		}
	}
	return response
}

// convertGeminiResponseToOpenAIResponsesNonStream converts a complete Gemini
// response into the OpenAI Responses API non-streaming JSON shape.
func convertGeminiResponseToOpenAIResponsesNonStream(_ context.Context, _ string, _, _ []byte, rawResponse []byte, _ *any) []byte {
	root := gjson.ParseBytes(rawResponse)

	responseID := "resp_" + root.Get("createTimeMillis").String()
	createdAt := root.Get("createTimeMillis").Int() / 1000
	if createdAt == 0 {
		createdAt = time.Now().Unix()
	}
	model := root.Get("modelVersion").String()

	out := map[string]interface{}{
		"id":         responseID,
		"object":     "response",
		"created_at": createdAt,
		"model":      model,
		"status":     "completed",
		"output":     []map[string]interface{}{},
	}

	outputIndex := 0
	var output []map[string]interface{}

	if candidates := root.Get("candidates"); candidates.Exists() && candidates.IsArray() {
		candidates.ForEach(func(_, candidate gjson.Result) bool {
			if parts := candidate.Get("content.parts"); parts.Exists() && parts.IsArray() {
				var textParts []string
				var reasoningParts []string
				var toolCalls []map[string]interface{}

				parts.ForEach(func(_, part gjson.Result) bool {
					if text := part.Get("text"); text.Exists() {
						if part.Get("thought").Bool() {
							reasoningParts = append(reasoningParts, text.String())
						} else {
							textParts = append(textParts, text.String())
						}
					}
					if fc := part.Get("functionCall"); fc.Exists() {
						argsStr := fc.Get("args").Raw
						if argsStr == "" {
							argsStr = "{}"
						}
						callID := fmt.Sprintf("call_%s_%d", responseID, len(toolCalls)+1)
						toolCalls = append(toolCalls, map[string]interface{}{
							"type":      "function_call",
							"id":        callID,
							"call_id":   callID,
							"name":      fc.Get("name").String(),
							"arguments": argsStr,
						})
					}
					return true
				})

				if len(reasoningParts) > 0 {
					item := map[string]interface{}{
						"type": "reasoning",
						"id":   fmt.Sprintf("rs_%s_%d", responseID, outputIndex),
						"summary": []map[string]interface{}{
							{"type": "summary_text", "text": strings.Join(reasoningParts, "")},
						},
						"output_index": outputIndex,
					}
					output = append(output, item)
					outputIndex++
				}
				if len(textParts) > 0 {
					item := map[string]interface{}{
						"type": "message",
						"role": "assistant",
						"content": []map[string]interface{}{
							{"type": "output_text", "text": strings.Join(textParts, "")},
						},
						"output_index": outputIndex,
					}
					output = append(output, item)
					outputIndex++
				}
				for _, tc := range toolCalls {
					tc["output_index"] = outputIndex
					output = append(output, tc)
					outputIndex++
				}
			}
			return true
		})
	}

	out["output"] = output
	out = setUsageMap(out, root.Get("usageMetadata"))

	result, _ := json.Marshal(out)
	return result
}

// convertGeminiResponseToOpenAIResponsesStream converts Gemini SSE chunks to
// OpenAI Responses API streaming events.
func convertGeminiResponseToOpenAIResponsesStream(_ context.Context, _ string, _, _ []byte, rawChunk []byte, param *any) [][]byte {
	state := getGeminiToResponsesState(param)

	raw := bytes.TrimSpace(rawChunk)
	if bytes.HasPrefix(raw, dataTag) {
		raw = bytes.TrimSpace(raw[5:])
	}
	if len(raw) == 0 || bytes.Equal(raw, []byte("[DONE]")) {
		return nil
	}

	root := gjson.ParseBytes(raw)

	if state.ResponseID == "" {
		state.ResponseID = "resp_" + root.Get("createTimeMillis").String()
	}
	if state.CreatedAt == 0 {
		state.CreatedAt = root.Get("createTimeMillis").Int() / 1000
		if state.CreatedAt == 0 {
			state.CreatedAt = time.Now().Unix()
		}
	}
	if state.Model == "" {
		state.Model = root.Get("modelVersion").String()
	}

	var events [][]byte

	if candidates := root.Get("candidates"); candidates.Exists() && candidates.IsArray() {
		candidates.ForEach(func(_, candidate gjson.Result) bool {
			if parts := candidate.Get("content.parts"); parts.Exists() && parts.IsArray() {
				parts.ForEach(func(_, part gjson.Result) bool {
					if text := part.Get("text"); text.Exists() {
						if part.Get("thought").Bool() {
							state.ReasoningAcc.WriteString(text.String())
						} else {
							if state.ReasoningAcc.Len() > 0 {
								if item := state.flushReasoning(); item != nil {
									state.appendItem(item)
									events = append(events, buildResponsesEvent(state, "response.output_item.added", map[string]interface{}{"item": item}))
									events = append(events, buildResponsesEvent(state, "response.output_item.done", map[string]interface{}{"output_index": item["output_index"], "item": item}))
								}
							}
							if state.TextAcc.Len() == 0 {

								// First text for this message block: announce it.
								item := map[string]interface{}{
									"type": "message",
									"role": "assistant",
									"content": []map[string]interface{}{
										{"type": "output_text", "text": ""},
									},
								}
								events = append(events, buildResponsesEvent(state, "response.output_item.added", map[string]interface{}{"item": item}))
							}
							state.TextAcc.WriteString(text.String())
							events = append(events, buildResponsesEvent(state, "response.output_text.delta", map[string]interface{}{
								"item_id": fmt.Sprintf("msg_%s", state.ResponseID),
								"delta":   text.String(),
							}))
						}
					}

					if fc := part.Get("functionCall"); fc.Exists() {
						// Flush any in-progress text/reasoning before emitting the tool call.
						if textItem := state.flushText(); textItem != nil {
							state.appendItem(textItem)
							events = append(events, buildResponsesEvent(state, "response.output_item.added", map[string]interface{}{"item": textItem}))
							events = append(events, buildResponsesEvent(state, "response.output_item.done", map[string]interface{}{"output_index": textItem["output_index"], "item": textItem}))
						}
						if reasoningItem := state.flushReasoning(); reasoningItem != nil {
							state.appendItem(reasoningItem)
							events = append(events, buildResponsesEvent(state, "response.output_item.added", map[string]interface{}{"item": reasoningItem}))
							events = append(events, buildResponsesEvent(state, "response.output_item.done", map[string]interface{}{"output_index": reasoningItem["output_index"], "item": reasoningItem}))
						}

						state.ToolIndex++
						idx := state.ToolIndex
						state.ToolNames[idx] = fc.Get("name").String()
						state.ToolCallIDs[idx] = state.nextCallID()
						argsStr := fc.Get("args").Raw
						if argsStr == "" {
							argsStr = "{}"
						}
						state.ToolArgsAcc[idx] = &strings.Builder{}
						state.ToolArgsAcc[idx].WriteString(argsStr)

						item := map[string]interface{}{
							"type":         "function_call",
							"id":           state.ToolCallIDs[idx],
							"call_id":      state.ToolCallIDs[idx],
							"name":         state.ToolNames[idx],
							"arguments":    "",
							"output_index": state.OutputIndex,
						}
						events = append(events, buildResponsesEvent(state, "response.output_item.added", map[string]interface{}{"item": item}))
						if argsStr != "" {
							events = append(events, buildResponsesEvent(state, "response.function_call_arguments.delta", map[string]interface{}{
								"item_id": state.ToolCallIDs[idx],
								"delta":   argsStr,
							}))
						}
						doneItem := state.flushToolCall(idx)
						events = append(events, buildResponsesEvent(state, "response.output_item.done", map[string]interface{}{"output_index": doneItem["output_index"], "item": doneItem}))
						state.appendItem(doneItem)
					}
					return true
				})
			}

			// Finish reason: flush pending items and emit response.completed.
			if fr := candidate.Get("finishReason"); fr.Exists() {
				if reasoningItem := state.flushReasoning(); reasoningItem != nil {
					state.appendItem(reasoningItem)
					events = append(events, buildResponsesEvent(state, "response.output_item.added", map[string]interface{}{"item": reasoningItem}))
					events = append(events, buildResponsesEvent(state, "response.output_item.done", map[string]interface{}{"output_index": reasoningItem["output_index"], "item": reasoningItem}))
				}
				if textItem := state.flushText(); textItem != nil {
					state.appendItem(textItem)
					events = append(events, buildResponsesEvent(state, "response.output_item.added", map[string]interface{}{"item": textItem}))
					events = append(events, buildResponsesEvent(state, "response.output_item.done", map[string]interface{}{"output_index": textItem["output_index"], "item": textItem}))
				}

				completed := buildResponsesCompleted(state)
				completed = setUsage(completed, root.Get("usageMetadata"))
				events = append(events, completed)
			}
			return true
		})
	}

	// Usage may arrive on a separate chunk; emit as a usage-only response.completed precursor.
	if usage := root.Get("usageMetadata"); usage.Exists() && len(events) == 0 {
		events = append(events, buildResponsesUsageEvent(state, usage))
	}

	return events
}
