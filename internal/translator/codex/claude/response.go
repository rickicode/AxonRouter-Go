package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"github.com/tidwall/gjson"
)

// codexStreamState holds state for Claude→Codex Responses streaming.
type codexStreamState struct {
	MessageID   string
	Model       string
	OutputIndex int
	ToolIndex   int
	ToolArgsAcc map[int]*strings.Builder
	ToolNames   map[int]string
	ContentAcc  strings.Builder
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

// convertClaudeResponseToCodexStream converts Claude streaming events to Codex Responses format.
func convertClaudeResponseToCodexStream(_ context.Context, _ string, _, _ []byte, rawChunk []byte, param *any) [][]byte {
	state := getCodexState(param)

	raw := bytes.TrimSpace(rawChunk)
	if !bytes.HasPrefix(raw, []byte("data:")) {
		return nil
	}
	raw = bytes.TrimSpace(raw[5:])
	if len(raw) == 0 {
		return nil
	}

	root := gjson.ParseBytes(raw)
	eventType := root.Get("type").String()

	if state.MessageID == "" {
		state.MessageID = root.Get("message.id").String()
	}
	if state.Model == "" {
		state.Model = root.Get("message.model").String()
	}

	switch eventType {
	case "content_block_delta":
		deltaType := root.Get("delta.type").String()
		if deltaType == "text_delta" {
			text := root.Get("delta.text").String()
			state.ContentAcc.WriteString(text)
			out := map[string]interface{}{
				"type":  "response.output_text.delta",
				"delta": text,
			}
			b, _ := json.Marshal(out)
			return [][]byte{b}
		}
		if deltaType == "input_json_delta" {
			partialJSON := root.Get("delta.partial_json").String()
			idx := root.Get("index").Int()
			if state.ToolArgsAcc[int(idx)] == nil {
				state.ToolArgsAcc[int(idx)] = &strings.Builder{}
			}
			state.ToolArgsAcc[int(idx)].WriteString(partialJSON)
		}

	case "content_block_start":
		blockType := root.Get("content_block.type").String()
		if blockType == "tool_use" {
			name := root.Get("content_block.name").String()
			callID := root.Get("content_block.id").String()
			idx := root.Get("index").Int()
			state.ToolNames[int(idx)] = name
			state.ToolArgsAcc[int(idx)] = &strings.Builder{}
			state.ToolIndex = int(idx)

			// Emit function_call start
			item := map[string]interface{}{
				"type":       "response.output_item.done",
				"output_index": state.OutputIndex,
				"item": map[string]interface{}{
					"type":       "function_call",
					"id":         callID,
					"call_id":    callID,
					"name":       name,
					"arguments":  "",
					"status":     "completed",
				},
			}
			state.OutputIndex++
			b, _ := json.Marshal(item)
			return [][]byte{b}
		}

	case "content_block_stop":
		idx := root.Get("index").Int()
		if args, ok := state.ToolArgsAcc[int(idx)]; ok && args.Len() > 0 {
			// Update arguments
			item := map[string]interface{}{
				"type":  "response.function_call_arguments.delta",
				"delta": args.String(),
			}
			b, _ := json.Marshal(item)
			return [][]byte{b}
		}

	case "message_stop":
		completed := map[string]interface{}{
			"type": "response.completed",
		}
		b, _ := json.Marshal(completed)
		return [][]byte{b}
	}

	return nil
}

// convertClaudeResponseToCodexNonStream converts a complete Claude response to Codex Responses format.
func convertClaudeResponseToCodexNonStream(_ context.Context, _ string, _, _ []byte, rawResponse []byte, _ *any) []byte {
	root := gjson.ParseBytes(rawResponse)

	out := make(map[string]interface{})
	out["id"] = root.Get("id").String()
	out["object"] = "response"
	out["model"] = root.Get("model").String()

	var outputItems []map[string]interface{}

	if content := root.Get("content"); content.Exists() && content.IsArray() {
		var textParts []string
		content.ForEach(func(_, block gjson.Result) bool {
			bType := block.Get("type").String()
			switch bType {
			case "text":
				textParts = append(textParts, block.Get("text").String())
			case "tool_use":
				// Flush any accumulated text
				if len(textParts) > 0 {
					outputItems = append(outputItems, map[string]interface{}{
						"type": "message",
						"content": []map[string]interface{}{{
							"type": "output_text",
							"text": strings.Join(textParts, ""),
						}},
					})
					textParts = nil
				}
				outputItems = append(outputItems, map[string]interface{}{
					"type":      "function_call",
					"id":        block.Get("id").String(),
					"call_id":   block.Get("id").String(),
					"name":      block.Get("name").String(),
					"arguments": block.Get("input").Raw,
					"status":    "completed",
				})
			}
			return true
		})

		if len(textParts) > 0 {
			outputItems = append(outputItems, map[string]interface{}{
				"type": "message",
				"content": []map[string]interface{}{{
					"type": "output_text",
					"text": strings.Join(textParts, ""),
				}},
			})
		}
	}

	out["output"] = outputItems

	// Usage
	usage := map[string]interface{}{
		"input_tokens":  root.Get("usage.input_tokens").Int(),
		"output_tokens": root.Get("usage.output_tokens").Int(),
		"total_tokens":  root.Get("usage.input_tokens").Int() + root.Get("usage.output_tokens").Int(),
	}
	out["usage"] = usage

	result, _ := json.Marshal(out)
	return result
}
