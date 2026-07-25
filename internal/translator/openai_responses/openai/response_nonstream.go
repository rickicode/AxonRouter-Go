package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

func convertOpenAINonStreamToResponses(_ context.Context, _ string, _, _ []byte, rawResponse []byte, _ *any) []byte {
	root := gjson.ParseBytes(rawResponse)

	responseID := root.Get("id").String()
	if responseID == "" {
		responseID = fmt.Sprintf("resp_%d", time.Now().UnixNano())
	}
	createdAt := root.Get("created").Int()
	if createdAt == 0 {
		createdAt = time.Now().Unix()
	}
	model := root.Get("model").String()

	out := map[string]interface{}{
		"id":         responseID,
		"object":     "response",
		"created_at": createdAt,
		"model":      model,
		"status":     "completed",
		"output":     []map[string]interface{}{},
	}

	var output []map[string]interface{}
	outputIndex := 0

	choice := root.Get("choices.0")
	message := choice.Get("message")
	if message.Exists() {
		toolCalls := message.Get("tool_calls")
		hasToolCalls := toolCalls.Exists() && toolCalls.IsArray()
		if hasToolCalls {
			toolCalls.ForEach(func(_, tc gjson.Result) bool {
				callID := tc.Get("id").String()
				output = append(output, map[string]interface{}{
					"type":         "function_call",
					"id":           callID,
					"call_id":      callID,
					"name":         tc.Get("function.name").String(),
					"arguments":    tc.Get("function.arguments").String(),
					"output_index": outputIndex,
				})
				outputIndex++
				return true
			})
		}

		content := message.Get("content")
		var text string
		if content.Exists() {
			if content.Type == gjson.String {
				text = content.String()
			} else if content.IsArray() {
				var parts []string
				content.ForEach(func(_, p gjson.Result) bool {
					if p.Get("type").String() == "text" {
						parts = append(parts, p.Get("text").String())
					}
					return true
				})
				text = strings.Join(parts, "")
			}
		}

		if hasToolCalls && text == "" {
			// keep output limited to tool calls
		} else {
			output = append(output, map[string]interface{}{
				"type": "message",
				"role": "assistant",
				"id":   fmt.Sprintf("msg_%s", responseID),
				"content": []map[string]interface{}{
					{"type": "output_text", "text": text},
				},
				"output_index": outputIndex,
			})
			outputIndex++
		}
	}

	out["output"] = output

	if usage := root.Get("usage"); usage.Exists() {
		out["usage"] = map[string]interface{}{
			"input_tokens":  usage.Get("prompt_tokens").Int(),
			"output_tokens": usage.Get("completion_tokens").Int(),
			"total_tokens":  usage.Get("total_tokens").Int(),
		}
	}

	result, _ := json.Marshal(out)
	return result
}
