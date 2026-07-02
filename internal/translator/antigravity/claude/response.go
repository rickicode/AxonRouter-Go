package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// antigravityStreamState holds state for Claude→Antigravity streaming.
type antigravityStreamState struct {
	Model       string
	CurrentText strings.Builder
	ToolCalls   []map[string]interface{}
	ToolIndex   int
	Thinking    bool
}

func getStreamState(param *any) *antigravityStreamState {
	if *param == nil {
		*param = &antigravityStreamState{}
	}
	return (*param).(*antigravityStreamState)
}

// convertClaudeResponseToAntigravityStream converts Claude streaming to Antigravity format.
func convertClaudeResponseToAntigravityStream(_ context.Context, _ string, _, _ []byte, rawChunk []byte, param *any) [][]byte {
	state := getStreamState(param)

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

	switch eventType {
	case "message_start":
		state.Model = root.Get("message.model").String()
		return nil

	case "content_block_start":
		blockType := root.Get("content_block.type").String()
		if blockType == "thinking" {
			state.Thinking = true
		} else if blockType == "tool_use" {
			state.Thinking = false
			name := root.Get("content_block.name").String()
			state.ToolCalls = append(state.ToolCalls, map[string]interface{}{
				"name": name,
				"args": "",
			})
			state.ToolIndex = len(state.ToolCalls)
		}

	case "content_block_delta":
		deltaType := root.Get("delta.type").String()
		if deltaType == "thinking_delta" {
			text := root.Get("delta.thinking").String()
			chunk := buildAntigravityChunk(state, text, nil, true)
			return [][]byte{chunk}
		}
		if deltaType == "text_delta" {
			text := root.Get("delta.text").String()
			state.Thinking = false
			state.CurrentText.WriteString(text)
			chunk := buildAntigravityChunk(state, text, nil, false)
			return [][]byte{chunk}
		}
		if deltaType == "input_json_delta" {
			partialJSON := root.Get("delta.partial_json").String()
			if state.ToolIndex > 0 && state.ToolIndex-1 < len(state.ToolCalls) {
				tc := state.ToolCalls[state.ToolIndex-1]
				if args, ok := tc["args"].(string); ok {
					tc["args"] = args + partialJSON
				}
			}
		}

	case "message_stop":
		chunk := buildAntigravityChunk(state, "", nil, false)
		return [][]byte{chunk}
	}

	return nil
}

// convertClaudeResponseToAntigravityNonStream converts a complete Claude response to Antigravity format.
func convertClaudeResponseToAntigravityNonStream(_ context.Context, _ string, _, _ []byte, rawResponse []byte, _ *any) []byte {
	root := gjson.ParseBytes(rawResponse)

	out := []byte(`{"response":{}}`)
	out, _ = sjson.SetBytes(out, "response.modelVersion", root.Get("model").String())

	var parts []map[string]interface{}

	if content := root.Get("content"); content.Exists() && content.IsArray() {
		content.ForEach(func(_, block gjson.Result) bool {
			bType := block.Get("type").String()
			switch bType {
			case "thinking":
				parts = append(parts, map[string]interface{}{
					"text":    block.Get("thinking").String(),
					"thought": true,
				})
			case "text":
				parts = append(parts, map[string]interface{}{
					"text": block.Get("text").String(),
				})
			case "tool_use":
				name := block.Get("name").String()
				args := block.Get("input").Raw
				var argsMap map[string]interface{}
				if args != "" {
					json.Unmarshal([]byte(args), &argsMap)
				}
				if argsMap == nil {
					argsMap = map[string]interface{}{}
				}
				parts = append(parts, map[string]interface{}{
					"functionCall": map[string]interface{}{
						"name": name,
						"args": argsMap,
					},
				})
			}
			return true
		})
	}

	candidates := []map[string]interface{}{{
		"content": map[string]interface{}{
			"role":  "model",
			"parts": parts,
		},
		"finishReason": "STOP",
	}}
	out, _ = sjson.SetRawBytes(out, "response.candidates", mustMarshal(candidates))

	// Usage
	usage := map[string]interface{}{
		"promptTokenCount":     root.Get("usage.input_tokens").Int(),
		"candidatesTokenCount": root.Get("usage.output_tokens").Int(),
		"totalTokenCount":      root.Get("usage.input_tokens").Int() + root.Get("usage.output_tokens").Int(),
	}
	out, _ = sjson.SetRawBytes(out, "response.usageMetadata", mustMarshal(usage))

	return out
}

func buildAntigravityChunk(state *antigravityStreamState, text string, toolCalls []map[string]interface{}, thought bool) []byte {
	chunk := []byte(`{"response":{}}`)
	chunk, _ = sjson.SetBytes(chunk, "response.modelVersion", state.Model)

	var parts []map[string]interface{}
	if text != "" {
		part := map[string]interface{}{"text": text}
		if thought {
			part["thought"] = true
		}
		parts = append(parts, part)
	}
	if toolCalls != nil {
		for _, tc := range toolCalls {
			parts = append(parts, map[string]interface{}{
				"functionCall": tc,
			})
		}
	}

	candidates := []map[string]interface{}{{
		"content": map[string]interface{}{
			"role":  "model",
			"parts": parts,
		},
	}}
	chunk, _ = sjson.SetRawBytes(chunk, "response.candidates", mustMarshal(candidates))

	b, _ := json.Marshal(chunk)
	return b
}
