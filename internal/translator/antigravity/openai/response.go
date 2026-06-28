package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// antigravityStreamState holds state for Antigravity→OpenAI streaming.
type antigravityStreamState struct {
	MessageID   string
	Model       string
	ToolIndex   int
	ToolArgsAcc map[int]*strings.Builder
	ToolNames   map[int]string
	ContentAcc  strings.Builder
}

func getAntigravityState(param *any) *antigravityStreamState {
	if *param == nil {
		*param = &antigravityStreamState{
			ToolArgsAcc: make(map[int]*strings.Builder),
			ToolNames:   make(map[int]string),
		}
	}
	return (*param).(*antigravityStreamState)
}

// convertAntigravityResponseToOpenAIStream converts Antigravity streaming chunks to OpenAI format.
func convertAntigravityResponseToOpenAIStream(_ context.Context, _ string, _, _ []byte, rawChunk []byte, param *any) [][]byte {
	state := getAntigravityState(param)

	raw := bytes.TrimSpace(rawChunk)
	if bytes.HasPrefix(raw, []byte("data:")) {
		raw = bytes.TrimSpace(raw[5:])
	}
	if len(raw) == 0 {
		return nil
	}

	root := gjson.ParseBytes(raw)

	if state.MessageID == "" {
		state.MessageID = fmt.Sprintf("ag-%d", root.Get("createTimeMillis").Int())
	}
	if state.Model == "" {
		state.Model = root.Get("modelVersion").String()
	}

	var results [][]byte

	if candidates := root.Get("candidates"); candidates.Exists() && candidates.IsArray() {
		candidates.ForEach(func(_, candidate gjson.Result) bool {
			if parts := candidate.Get("content.parts"); parts.Exists() && parts.IsArray() {
				parts.ForEach(func(_, part gjson.Result) bool {
					if text := part.Get("text"); text.Exists() {
						chunk := buildOpenAIChunkFromAG(state.MessageID, state.Model, text.String(), nil)
						results = append(results, chunk)
						state.ContentAcc.WriteString(text.String())
					}
					return true
				})
			}
			return true
		})
	}

	return results
}

// convertAntigravityResponseToOpenAINonStream converts a complete Antigravity response to OpenAI format.
func convertAntigravityResponseToOpenAINonStream(_ context.Context, _ string, _, _ []byte, rawResponse []byte, _ *any) []byte {
	root := gjson.ParseBytes(rawResponse)

	out := make(map[string]interface{})
	out["id"] = "chatcmpl-ag-" + root.Get("createTimeMillis").String()
	out["object"] = "chat.completion"
	out["model"] = root.Get("modelVersion").String()
	out["created"] = root.Get("createTimeMillis").Int() / 1000

	var textParts []string
	if candidates := root.Get("candidates"); candidates.Exists() && candidates.IsArray() {
		candidates.ForEach(func(_, candidate gjson.Result) bool {
			if parts := candidate.Get("content.parts"); parts.Exists() && parts.IsArray() {
				parts.ForEach(func(_, part gjson.Result) bool {
					if text := part.Get("text"); text.Exists() {
						textParts = append(textParts, text.String())
					}
					return true
				})
			}
			return true
		})
	}

	msg := map[string]interface{}{
		"role":          "assistant",
		"finish_reason": "stop",
	}
	if len(textParts) > 0 {
		msg["content"] = strings.Join(textParts, "")
	}
	out["choices"] = []map[string]interface{}{{"index": 0, "message": msg}}

	if usage := root.Get("usageMetadata"); usage.Exists() {
		out["usage"] = map[string]interface{}{
			"prompt_tokens":     usage.Get("promptTokenCount").Int(),
			"completion_tokens": usage.Get("candidatesTokenCount").Int(),
			"total_tokens":      usage.Get("totalTokenCount").Int(),
		}
	}

	result, _ := json.Marshal(out)
	return result
}

func buildOpenAIChunkFromAG(id, model string, content string, toolCalls []map[string]interface{}) []byte {
	chunk := []byte(`{"object":"chat.completion.chunk","choices":[{"index":0,"delta":{}}]}`)
	chunk, _ = sjson.SetBytes(chunk, "id", "chatcmpl-"+id)
	chunk, _ = sjson.SetBytes(chunk, "model", model)
	if content != "" {
		chunk, _ = sjson.SetBytes(chunk, "choices.0.delta.content", content)
	}
	if toolCalls != nil {
		b, _ := json.Marshal(toolCalls)
		chunk, _ = sjson.SetRawBytes(chunk, "choices.0.delta.tool_calls", b)
	}
	return []byte(fmt.Sprintf("data: %s\n\n", string(chunk)))
}
