package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// geminiStreamState holds accumulated state for Gemini→OpenAI streaming.
type geminiStreamState struct {
	MessageID   string
	Model       string
	ToolIndex   int
	ToolArgsAcc map[int]*strings.Builder
	ToolNames   map[int]string
	ContentAcc  strings.Builder
}

func getGeminiState(param *any) *geminiStreamState {
	if *param == nil {
		*param = &geminiStreamState{
			ToolArgsAcc: make(map[int]*strings.Builder),
			ToolNames:   make(map[int]string),
		}
	}
	return (*param).(*geminiStreamState)
}

// convertGeminiResponseToOpenAIStream converts Gemini streaming chunks to OpenAI format.
func convertGeminiResponseToOpenAIStream(_ context.Context, _ string, _, _ []byte, rawChunk []byte, param *any) [][]byte {
	state := getGeminiState(param)

	// Gemini streams JSON objects, may be wrapped in SSE
	raw := bytes.TrimSpace(rawChunk)
	if bytes.HasPrefix(raw, []byte("data:")) {
		raw = bytes.TrimSpace(raw[5:])
	}
	if len(raw) == 0 {
		return nil
	}

	root := gjson.ParseBytes(raw)

	if state.MessageID == "" {
		state.MessageID = fmt.Sprintf("gemini-%d", root.Get("createTimeMillis").Int())
	}

	var results [][]byte

	if candidates := root.Get("candidates"); candidates.Exists() && candidates.IsArray() {
		candidates.ForEach(func(_, candidate gjson.Result) bool {
			if parts := candidate.Get("content.parts"); parts.Exists() && parts.IsArray() {
				parts.ForEach(func(_, part gjson.Result) bool {
					if text := part.Get("text"); text.Exists() {
						if part.Get("thought").Bool() {
							reasoningText := text.String()
							chunk := buildOpenAIFromGemini(state.MessageID, state.Model, nil, nil, &reasoningText)
							results = append(results, chunk)
						} else {
							chunk := buildOpenAIFromGemini(state.MessageID, state.Model, nil, &text, nil)
							results = append(results, chunk)
							state.ContentAcc.WriteString(text.String())
						}
					}

					if fc := part.Get("functionCall"); fc.Exists() {
						state.ToolIndex++
						idx := state.ToolIndex
						state.ToolNames[idx] = fc.Get("name").String()
						argsStr := fc.Get("args").Raw
						if argsStr == "" {
							argsStr = "{}"
						}
						chunk := buildOpenAIFromGemini(state.MessageID, state.Model, []map[string]interface{}{{
							"index": idx,
							"id":    fmt.Sprintf("call_%s_%d", state.MessageID, idx),
							"type":  "function",
							"function": map[string]interface{}{
								"name":      fc.Get("name").String(),
								"arguments": argsStr,
							},
						}}, nil, nil)
						results = append(results, chunk)
					}
					return true
				})
			}

			// Usage metadata
			if usage := root.Get("usageMetadata"); usage.Exists() {
				chunk := buildOpenAIFromGemini(state.MessageID, state.Model, nil, nil, nil)
				if promptTokens := usage.Get("promptTokenCount"); promptTokens.Exists() {
					chunk, _ = sjson.SetBytes(chunk, "usage.prompt_tokens", promptTokens.Int())
				}
				if completionTokens := usage.Get("candidatesTokenCount"); completionTokens.Exists() {
					chunk, _ = sjson.SetBytes(chunk, "usage.completion_tokens", completionTokens.Int())
				}
				if totalTokens := usage.Get("totalTokenCount"); totalTokens.Exists() {
					chunk, _ = sjson.SetBytes(chunk, "usage.total_tokens", totalTokens.Int())
				}
				if thoughtsTokens := usage.Get("thoughtsTokenCount"); thoughtsTokens.Exists() && thoughtsTokens.Int() > 0 {
					chunk, _ = sjson.SetBytes(chunk, "usage.completion_tokens_details.reasoning_tokens", thoughtsTokens.Int())
				}
				if cachedTokens := usage.Get("cachedContentTokenCount"); cachedTokens.Exists() && cachedTokens.Int() > 0 {
					chunk, _ = sjson.SetBytes(chunk, "usage.prompt_tokens_details.cached_tokens", cachedTokens.Int())
				}
				results = append(results, chunk)
			}

			// Finish reason
			if fr := candidate.Get("finishReason"); fr.Exists() {
				finishReason := "stop"
				switch fr.String() {
				case "STOP":
					finishReason = "stop"
				case "MAX_TOKENS":
					finishReason = "length"
				case "SAFETY":
					finishReason = "content_filter"
				}
				chunk := buildOpenAIFromGemini(state.MessageID, state.Model, nil, nil, nil)
				chunk, _ = sjson.SetBytes(chunk, "choices.0.finish_reason", finishReason)
				results = append(results, chunk)
			}
			return true
		})
	}

	return results
}

// convertGeminiResponseToOpenAINonStream converts a complete Gemini response to OpenAI format.
func convertGeminiResponseToOpenAINonStream(_ context.Context, _ string, _, _ []byte, rawResponse []byte, _ *any) []byte {
	root := gjson.ParseBytes(rawResponse)

	out := make(map[string]interface{})
	out["id"] = "chatcmpl-gemini-" + root.Get("createTimeMillis").String()
	out["object"] = "chat.completion"
	out["model"] = root.Get("modelVersion").String()
	out["created"] = root.Get("createTimeMillis").Int() / 1000

	var textParts []string
	var reasoningParts []string
	var toolCalls []map[string]interface{}
	toolIdx := 0

	if candidates := root.Get("candidates"); candidates.Exists() && candidates.IsArray() {
		candidates.ForEach(func(_, candidate gjson.Result) bool {
			if parts := candidate.Get("content.parts"); parts.Exists() && parts.IsArray() {
				parts.ForEach(func(_, part gjson.Result) bool {
					if text := part.Get("text"); text.Exists() {
						if part.Get("thought").Bool() {
							reasoningParts = append(reasoningParts, text.String())
						} else {
							textParts = append(textParts, text.String())
						}
					}
					if fc := part.Get("functionCall"); fc.Exists() {
						toolIdx++
						toolCalls = append(toolCalls, map[string]interface{}{
							"id":   fmt.Sprintf("call_gemini_%d", toolIdx),
							"type": "function",
							"function": map[string]interface{}{
								"name":      fc.Get("name").String(),
								"arguments": fc.Get("args").Raw,
							},
						})
					}
					return true
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
	if len(reasoningParts) > 0 {
		msg["reasoning_content"] = strings.Join(reasoningParts, "")
	}
	if len(toolCalls) > 0 {
		msg["tool_calls"] = toolCalls
		msg["finish_reason"] = "tool_calls"
	} else {
		msg["finish_reason"] = "stop"
	}
	out["choices"] = []map[string]interface{}{{"index": 0, "message": msg}}

	if usage := root.Get("usageMetadata"); usage.Exists() {
		usageMap := map[string]interface{}{
			"prompt_tokens":     usage.Get("promptTokenCount").Int(),
			"completion_tokens": usage.Get("candidatesTokenCount").Int(),
			"total_tokens":      usage.Get("totalTokenCount").Int(),
		}
		if thoughtsTokens := usage.Get("thoughtsTokenCount"); thoughtsTokens.Exists() && thoughtsTokens.Int() > 0 {
			usageMap["completion_tokens_details"] = map[string]interface{}{
				"reasoning_tokens": thoughtsTokens.Int(),
			}
		}
		if cachedTokens := usage.Get("cachedContentTokenCount"); cachedTokens.Exists() && cachedTokens.Int() > 0 {
			usageMap["prompt_tokens_details"] = map[string]interface{}{
				"cached_tokens": cachedTokens.Int(),
			}
		}
		out["usage"] = usageMap
	}

	result, _ := json.Marshal(out)
	return result
}

func buildOpenAIFromGemini(id, model string, toolCalls []map[string]interface{}, content *gjson.Result, reasoningContent *string) []byte {
	chunk := []byte(`{"object":"chat.completion.chunk","choices":[{"index":0,"delta":{}}]}`)
	chunk, _ = sjson.SetBytes(chunk, "id", "chatcmpl-"+id)
	chunk, _ = sjson.SetBytes(chunk, "model", model)
	if content != nil && content.Exists() {
		chunk, _ = sjson.SetBytes(chunk, "choices.0.delta.content", content.String())
	}
	if reasoningContent != nil {
		chunk, _ = sjson.SetBytes(chunk, "choices.0.delta.reasoning_content", *reasoningContent)
	}
	if toolCalls != nil {
		b, _ := json.Marshal(toolCalls)
		chunk, _ = sjson.SetRawBytes(chunk, "choices.0.delta.tool_calls", b)
	}
	return []byte("data: " + string(chunk) + "\n\n")
}
