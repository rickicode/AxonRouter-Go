package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
)

// geminiStreamState holds state for OpenAI→Gemini streaming.
type geminiStreamState struct {
	Model       string
	Candidates  []map[string]interface{}
	CurrentText strings.Builder
	ToolCalls   []map[string]interface{}
	ToolIndex   int
}

func getGeminiState(param *any) *geminiStreamState {
	if *param == nil {
		*param = &geminiStreamState{}
	}
	return (*param).(*geminiStreamState)
}

// convertOpenAIResponseToGeminiStream converts OpenAI streaming chunks to Gemini format.
func convertOpenAIResponseToGeminiStream(_ context.Context, _ string, _, _ []byte, rawChunk []byte, param *any) [][]byte {
	state := getGeminiState(param)

	raw := bytes.TrimSpace(rawChunk)
	if !bytes.HasPrefix(raw, []byte("data:")) {
		return nil
	}
	raw = bytes.TrimSpace(raw[5:])
	if bytes.Equal(raw, []byte("[DONE]")) {
		return handleGeminiDone(state)
	}

	root := gjson.ParseBytes(raw)

	if state.Model == "" {
		state.Model = root.Get("model").String()
	}

	var results [][]byte

	if choices := root.Get("choices"); choices.Exists() && choices.IsArray() {
		choices.ForEach(func(_, choice gjson.Result) bool {
			delta := choice.Get("delta")

			if content := delta.Get("content"); content.Exists() {
				state.CurrentText.WriteString(content.String())
			}

			if toolCalls := delta.Get("tool_calls"); toolCalls.Exists() && toolCalls.IsArray() {
				toolCalls.ForEach(func(_, tc gjson.Result) bool {
					state.ToolIndex++
					name := tc.Get("function.name").String()
					args := tc.Get("function.arguments").String()
					state.ToolCalls = append(state.ToolCalls, map[string]interface{}{
						"name": name,
						"args": json.RawMessage(args),
					})
					return true
				})
			}

			if fr := choice.Get("finish_reason"); fr.Exists() && fr.String() != "" {
				// Emit final chunk
				chunk := buildGeminiChunk(state, fr.String())
				results = append(results, chunk)
			}
			return true
		})
	}

	return results
}

// convertOpenAIResponseToGeminiNonStream converts a complete OpenAI response to Gemini format.
func convertOpenAIResponseToGeminiNonStream(_ context.Context, _ string, _, _ []byte, rawResponse []byte, _ *any) []byte {
	root := gjson.ParseBytes(rawResponse)

	out := make(map[string]interface{})

	var parts []map[string]interface{}
	if choices := root.Get("candidates"); choices.Exists() && choices.IsArray() {
		choices.ForEach(func(_, choice gjson.Result) bool {
			msg := choice.Get("message")
			if text := msg.Get("content"); text.Exists() && text.String() != "" {
				parts = append(parts, map[string]interface{}{"text": text.String()})
			}
			if toolCalls := msg.Get("tool_calls"); toolCalls.Exists() && toolCalls.IsArray() {
				toolCalls.ForEach(func(_, tc gjson.Result) bool {
					parts = append(parts, map[string]interface{}{
						"functionCall": map[string]interface{}{
							"name": tc.Get("function.name").String(),
							"args": json.RawMessage(tc.Get("function.arguments").String()),
						},
					})
					return true
				})
			}
			return true
		})
	}

	if len(parts) > 0 {
		out["candidates"] = []map[string]interface{}{{
			"content": map[string]interface{}{
				"role":  "model",
				"parts": parts,
			},
			"finishReason": "STOP",
		}}
	}

	if usage := root.Get("usage"); usage.Exists() {
		out["usageMetadata"] = map[string]interface{}{
			"promptTokenCount":     usage.Get("prompt_tokens").Int(),
			"candidatesTokenCount": usage.Get("completion_tokens").Int(),
			"totalTokenCount":      usage.Get("total_tokens").Int(),
		}
	}

	result, _ := json.Marshal(out)
	return result
}

func handleGeminiDone(state *geminiStreamState) [][]byte {
	chunk := buildGeminiChunk(state, "STOP")
	return [][]byte{chunk}
}

func buildGeminiChunk(state *geminiStreamState, finishReason string) []byte {
	out := make(map[string]interface{})

	var parts []map[string]interface{}
	if state.CurrentText.Len() > 0 {
		parts = append(parts, map[string]interface{}{"text": state.CurrentText.String()})
	}
	for _, tc := range state.ToolCalls {
		parts = append(parts, map[string]interface{}{
			"functionCall": tc,
		})
	}

	candidate := map[string]interface{}{
		"finishReason": finishReason,
	}
	if len(parts) > 0 {
		candidate["content"] = map[string]interface{}{
			"role":  "model",
			"parts": parts,
		}
	}
	out["candidates"] = []map[string]interface{}{candidate}

	b, _ := json.Marshal(out)
	return []byte(fmt.Sprintf("data: %s\n\n", string(b)))
}
