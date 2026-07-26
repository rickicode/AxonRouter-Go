package interactions

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

// interactionsStreamState tracks accumulated text for Interactions -> OpenAI streaming.
type interactionsStreamState struct {
	Model        string
	MessageID    string
	Started      bool
	HasReasoning bool
}

func getInteractionsStreamState(param *any) *interactionsStreamState {
	if *param == nil {
		*param = &interactionsStreamState{}
	}
	return (*param).(*interactionsStreamState)
}

// convertInteractionsResponseToOpenAIStream converts Interactions SSE to OpenAI chat SSE.
func convertInteractionsResponseToOpenAIStream(_ context.Context, modelName string, _, _ []byte, rawChunk []byte, param *any) [][]byte {
	state := getInteractionsStreamState(param)

	raw := bytes.TrimSpace(rawChunk)
	if bytes.HasPrefix(raw, []byte("data:")) {
		raw = bytes.TrimSpace(raw[5:])
	}
	if len(raw) == 0 {
		return nil
	}
	if bytes.Equal(raw, []byte("[DONE]")) {
		return [][]byte{[]byte("data: [DONE]\n\n")}
	}

	root := gjson.ParseBytes(raw)
	eventType := root.Get("event_type").String()

	if state.Model == "" {
		state.Model = firstNonEmpty(modelName, root.Get("interaction.model").String(), root.Get("model").String())
	}
	if state.MessageID == "" {
		state.MessageID = firstNonEmpty(root.Get("interaction.id").String(), root.Get("id").String(), fmt.Sprintf("chatcmpl-%d", time.Now().UnixMilli()))
	}

	var results [][]byte

	switch eventType {
	case "step.delta":
		delta := root.Get("delta")
		stepType := root.Get("step.type").String()
		if stepType == "thought" || delta.Get("content.text").Exists() {
			text := firstNonEmpty(delta.Get("content.text").String(), delta.Get("text").String())
			if text != "" {
				state.HasReasoning = true
				chunk := baseOpenAIChatChunk(state.MessageID, state.Model)
				if !state.Started {
					state.Started = true
					chunk, _ = sjson.SetBytes(chunk, "choices.0.delta.role", "assistant")
				}
				chunk, _ = sjson.SetBytes(chunk, "choices.0.delta.reasoning_content", text)
				results = append(results, wrapSSE(chunk))
			}
		} else {
			text := delta.Get("text").String()
			if text != "" {
				state.Started = true
				chunk := baseOpenAIChatChunk(state.MessageID, state.Model)
				chunk, _ = sjson.SetBytes(chunk, "choices.0.delta.content", text)
				results = append(results, wrapSSE(chunk))
			}
		}
	case "step.stop", "finish":
		chunk := baseOpenAIChatChunk(state.MessageID, state.Model)
		chunk, _ = sjson.SetBytes(chunk, "choices.0.finish_reason", "stop")
		results = append(results, wrapSSE(chunk))
	}

	return results
}

func baseOpenAIChatChunk(id, model string) []byte {
	return []byte(fmt.Sprintf(`{"id":"%s","object":"chat.completion.chunk","created":%d,"model":"%s","choices":[{"index":0,"delta":{}}]}`, id, time.Now().Unix(), model))
}

func wrapSSE(chunk []byte) []byte {
	return []byte("data: " + string(chunk) + "\n\n")
}

// convertInteractionsResponseToOpenAINonStream converts an Interactions response
// to OpenAI Chat Completions format.
func convertInteractionsResponseToOpenAINonStream(_ context.Context, modelName string, _, _ []byte, rawResponse []byte, _ *any) []byte {
	root := gjson.ParseBytes(rawResponse)

	id := firstNonEmpty(root.Get("id").String(), root.Get("interaction.id").String(), fmt.Sprintf("chatcmpl-%d", time.Now().UnixMilli()))
	model := firstNonEmpty(modelName, root.Get("model").String(), root.Get("interaction.model").String())

	var textParts []string
	var reasoningParts []string
	var toolCalls []map[string]interface{}

	steps := root.Get("steps")
	if !steps.Exists() {
		steps = root.Get("interaction.steps")
	}
	if steps.Exists() && steps.IsArray() {
		steps.ForEach(func(_, step gjson.Result) bool {
			switch step.Get("type").String() {
			case "model_output":
				collectText(step.Get("content"), &textParts)
			case "thought":
				collectText(step.Get("content"), &reasoningParts)
			case "function_call":
				callID := firstNonEmpty(step.Get("call_id").String(), step.Get("id").String(), fmt.Sprintf("call_%d", len(toolCalls)))
				toolCalls = append(toolCalls, map[string]interface{}{
					"id":   callID,
					"type": "function",
					"function": map[string]interface{}{
						"name":      step.Get("name").String(),
						"arguments": jsonStringValue(step.Get("arguments"), "{}"),
					},
				})
			}
			return true
		})
	}

	msg := map[string]interface{}{"role": "assistant"}
	if len(textParts) > 0 {
		msg["content"] = strings.Join(textParts, "")
	}
	if len(reasoningParts) > 0 {
		msg["reasoning_content"] = strings.Join(reasoningParts, "")
	}
	if len(toolCalls) > 0 {
		msg["tool_calls"] = toolCalls
	}

	finishReason := "stop"
	if len(toolCalls) > 0 {
		finishReason = "tool_calls"
	}
	choice := map[string]interface{}{
		"index":         0,
		"message":       msg,
		"finish_reason": finishReason,
	}

	out := map[string]interface{}{
		"id":      id,
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]interface{}{choice},
	}

	out = setInteractionsUsageToOpenAI(out, root.Get("usage"))

	result, _ := json.Marshal(out)
	return result
}

func collectText(content gjson.Result, dest *[]string) {
	if content.Type == gjson.String {
		*dest = append(*dest, content.String())
		return
	}
	if !content.IsArray() {
		return
	}
	content.ForEach(func(_, part gjson.Result) bool {
		if part.Get("type").String() == "text" {
			*dest = append(*dest, part.Get("text").String())
		}
		return true
	})
}

func setInteractionsUsageToOpenAI(out map[string]interface{}, usage gjson.Result) map[string]interface{} {
	if !usage.Exists() {
		return out
	}
	u := map[string]interface{}{
		"prompt_tokens":     usage.Get("input_tokens").Int(),
		"completion_tokens": usage.Get("output_tokens").Int(),
		"total_tokens":      usage.Get("total_tokens").Int(),
	}
	if thoughts := usage.Get("reasoning_tokens"); thoughts.Exists() && thoughts.Int() > 0 {
		u["completion_tokens_details"] = map[string]interface{}{"reasoning_tokens": thoughts.Int()}
	}
	if cached := usage.Get("cached_tokens"); cached.Exists() && cached.Int() > 0 {
		u["prompt_tokens_details"] = map[string]interface{}{"cached_tokens": cached.Int()}
	}
	out["usage"] = u
	return out
}

// openAIStreamState tracks content for OpenAI -> Interactions streaming.
type openAIStreamState struct {
	ID           string
	TextAcc      strings.Builder
	ReasoningAcc strings.Builder
	ToolName     string
	ToolArgs     strings.Builder
	Started      bool
	StepOpen     bool
	Done         bool
	StepIndex    int
}

func getOpenAIStreamState(param *any) *openAIStreamState {
	if *param == nil {
		*param = &openAIStreamState{}
	}
	return (*param).(*openAIStreamState)
}

// convertOpenAIResponseToInteractionsStream converts OpenAI chat SSE to Interactions SSE.
func convertOpenAIResponseToInteractionsStream(_ context.Context, modelName string, _, _ []byte, rawChunk []byte, param *any) [][]byte {
	state := getOpenAIStreamState(param)

	raw := bytes.TrimSpace(rawChunk)
	if bytes.HasPrefix(raw, []byte("data:")) {
		raw = bytes.TrimSpace(raw[5:])
	}
	if len(raw) == 0 {
		return nil
	}
	if bytes.Equal(raw, []byte("[DONE]")) {
		return appendInteractionsDone(state)
	}

	root := gjson.ParseBytes(raw)
	if state.ID == "" {
		state.ID = firstNonEmpty(root.Get("id").String(), fmt.Sprintf("interaction_%d", time.Now().UnixMilli()))
	}

	var results [][]byte

	if !state.Started {
		state.Started = true
		created := map[string]interface{}{
			"interaction": map[string]interface{}{
				"id":     state.ID,
				"status": "in_progress",
				"object": "interaction",
				"model":  firstNonEmpty(modelName, root.Get("model").String()),
			},
			"event_type": "interaction.created",
		}
		results = append(results, emitInteractionsEvent("interaction.created", created))
	}

	if choices := root.Get("choices"); choices.Exists() && choices.IsArray() {
		choices.ForEach(func(_, choice gjson.Result) bool {
			delta := choice.Get("delta")

			if reasoning := delta.Get("reasoning_content"); reasoning.Exists() {
				text := reasoning.String()
				if text != "" {
					if !state.StepOpen || state.TextAcc.Len()+state.ToolArgs.Len() > 0 {
						results = append(results, emitStepStop(state))
						results = append(results, emitStepStart(state, "thought"))
					}
					state.StepOpen = true
					state.ReasoningAcc.WriteString(text)
					results = append(results, emitReasoningDelta(state, text))
				}
			}

			if content := delta.Get("content"); content.Exists() {
				text := content.String()
				if text != "" {
					if !state.StepOpen || state.ReasoningAcc.Len()+state.ToolArgs.Len() > 0 {
						results = append(results, emitStepStop(state))
						results = append(results, emitStepStart(state, "model_output"))
					}
					state.StepOpen = true
					state.TextAcc.WriteString(text)
					results = append(results, emitTextDelta(state, text))
				}
			}

			if toolCalls := delta.Get("tool_calls"); toolCalls.Exists() && toolCalls.IsArray() {
				toolCalls.ForEach(func(_, tc gjson.Result) bool {
					if name := tc.Get("function.name").String(); name != "" {
						state.ToolName = name
					}
					if args := tc.Get("function.arguments").String(); args != "" {
						state.ToolArgs.WriteString(args)
					}
					if state.ToolName != "" {
						if !state.StepOpen || state.TextAcc.Len()+state.ReasoningAcc.Len() > 0 {
							results = append(results, emitStepStop(state))
						}
						results = append(results, emitFunctionCallStart(state))
						state.StepOpen = true
					}
					return true
				})
			}

			if fr := choice.Get("finish_reason"); fr.Exists() && fr.String() != "" {
				results = append(results, emitStepStop(state))
			}

			return true
		})
	}

	return results
}

// convertOpenAIResponseToInteractionsNonStream converts an OpenAI chat completion
// response to the Interactions shape.
func convertOpenAIResponseToInteractionsNonStream(_ context.Context, modelName string, _, _ []byte, rawResponse []byte, _ *any) []byte {
	root := gjson.ParseBytes(rawResponse)

	id := firstNonEmpty(root.Get("id").String(), fmt.Sprintf("interaction_%d", time.Now().UnixMilli()))
	model := firstNonEmpty(modelName, root.Get("model").String())

	var steps []map[string]interface{}

	if choices := root.Get("choices"); choices.Exists() && choices.IsArray() {
		choices.ForEach(func(_, choice gjson.Result) bool {
			msg := choice.Get("message")

			if reasoning := msg.Get("reasoning_content"); reasoning.Exists() {
				steps = append(steps, map[string]interface{}{
					"type":    "thought",
					"content": []map[string]interface{}{{"type": "text", "text": reasoning.String()}},
				})
			}

			if content := msg.Get("content"); content.Exists() && content.String() != "" {
				steps = append(steps, map[string]interface{}{
					"type":    "model_output",
					"content": []map[string]interface{}{{"type": "text", "text": content.String()}},
				})
			}

			if toolCalls := msg.Get("tool_calls"); toolCalls.Exists() && toolCalls.IsArray() {
				toolCalls.ForEach(func(_, tc gjson.Result) bool {
					callID := firstNonEmpty(tc.Get("id").String(), fmt.Sprintf("call_%d", len(steps)))
					args := tc.Get("function.arguments").Raw
					if args == "" {
						args = "{}"
					}
					steps = append(steps, map[string]interface{}{
						"type":      "function_call",
						"id":        callID,
						"call_id":   callID,
						"name":      tc.Get("function.name").String(),
						"arguments": args,
					})
					return true
				})
			}

			return true
		})
	}

	out := map[string]interface{}{
		"id":     id,
		"object": "interaction",
		"status": "completed",
		"model":  model,
		"steps":  steps,
	}

	if usage := root.Get("usage"); usage.Exists() {
		out["usage"] = map[string]interface{}{
			"input_tokens":        usage.Get("prompt_tokens").Int(),
			"output_tokens":       usage.Get("completion_tokens").Int(),
			"total_tokens":        usage.Get("total_tokens").Int(),
			"reasoning_tokens":    usage.Get("completion_tokens_details.reasoning_tokens").Int(),
			"cached_tokens":       usage.Get("prompt_tokens_details.cached_tokens").Int(),
			"total_input_tokens":  usage.Get("prompt_tokens").Int(),
			"total_output_tokens": usage.Get("completion_tokens").Int(),
		}
	}

	result, _ := json.Marshal(out)
	return result
}

func emitInteractionsEvent(eventType string, payload map[string]interface{}) []byte {
	payload["event_type"] = eventType
	b, _ := json.Marshal(payload)
	return []byte("data: " + string(b) + "\n\n")
}

func emitStepStart(state *openAIStreamState, stepType string) []byte {
	state.StepOpen = true
	state.StepIndex++
	return emitInteractionsEvent("step.start", map[string]interface{}{
		"index": state.StepIndex,
		"step":  map[string]interface{}{"type": stepType},
	})
}

func emitStepStop(state *openAIStreamState) []byte {
	if !state.StepOpen {
		return nil
	}
	state.StepOpen = false
	return emitInteractionsEvent("step.stop", map[string]interface{}{
		"index": state.StepIndex,
	})
}

func emitTextDelta(state *openAIStreamState, text string) []byte {
	return emitInteractionsEvent("step.delta", map[string]interface{}{
		"index": state.StepIndex,
		"delta": map[string]interface{}{"type": "text", "text": text},
	})
}

func emitReasoningDelta(state *openAIStreamState, text string) []byte {
	return emitInteractionsEvent("step.delta", map[string]interface{}{
		"index": state.StepIndex,
		"delta": map[string]interface{}{
			"type": "thought_summary",
			"content": map[string]interface{}{
				"type": "text",
				"text": text,
			},
		},
	})
}

func emitFunctionCallStart(state *openAIStreamState) []byte {
	state.StepOpen = true
	callID := fmt.Sprintf("call_%s_%d", state.ID, state.StepIndex)
	return emitInteractionsEvent("step.start", map[string]interface{}{
		"index": state.StepIndex,
		"step": map[string]interface{}{
			"type":      "function_call",
			"id":        callID,
			"call_id":   callID,
			"name":      state.ToolName,
			"arguments": map[string]interface{}{},
		},
	})
}

func appendInteractionsDone(state *openAIStreamState) [][]byte {
	if state.Done {
		return nil
	}
	state.Done = true
	completed := map[string]interface{}{
		"interaction": map[string]interface{}{
			"id":     state.ID,
			"status": "completed",
			"object": "interaction",
		},
		"event_type": "interaction.completed",
	}
	return [][]byte{
		emitInteractionsEvent("interaction.completed", completed),
		emitInteractionsEvent("done", map[string]interface{}{}),
		[]byte("data: [DONE]\n\n"),
	}
}
