package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"github.com/tidwall/gjson"
)

// codexStreamState holds state for Gemini→Codex Responses streaming.
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
						name := fc.Get("name").String()
						callID := fc.Get("id").String()
						if callID == "" {
							callID = "call_" + name
						}
						args := fc.Get("args").Raw

						// Emit function_call item
						item := map[string]interface{}{
							"type":  "response.output_item.done",
							"item": map[string]interface{}{
								"type":       "function_call",
								"id":         callID,
								"call_id":    callID,
								"name":       name,
								"arguments":  args,
								"status":     "completed",
							},
						}
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

	var outputItems []map[string]interface{}
	var textParts []string

	if candidates := root.Get("candidates"); candidates.Exists() && candidates.IsArray() {
		candidates.ForEach(func(_, candidate gjson.Result) bool {
			if parts := candidate.Get("content.parts"); parts.Exists() && parts.IsArray() {
				parts.ForEach(func(_, part gjson.Result) bool {
					if text := part.Get("text"); text.Exists() {
						textParts = append(textParts, text.String())
					}
					if fc := part.Get("functionCall"); fc.Exists() {
						// Flush text
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
						name := fc.Get("name").String()
						callID := fc.Get("id").String()
						if callID == "" {
							callID = "call_" + name
						}
						outputItems = append(outputItems, map[string]interface{}{
							"type":      "function_call",
							"id":        callID,
							"call_id":   callID,
							"name":      name,
							"arguments": fc.Get("args").Raw,
							"status":    "completed",
						})
					}
					return true
				})
			}
			return true
		})
	}

	if len(textParts) > 0 {
		outputItems = append(outputItems, map[string]interface{}{
			"type": "message",
			"content": []map[string]interface{}{{
				"type": "output_text",
				"text": strings.Join(textParts, ""),
			}},
		})
	}

	out["output"] = outputItems

	// Usage
	usage := map[string]interface{}{
		"input_tokens":  root.Get("usageMetadata.promptTokenCount").Int(),
		"output_tokens": root.Get("usageMetadata.candidatesTokenCount").Int(),
		"total_tokens":  root.Get("usageMetadata.totalTokenCount").Int(),
	}
	out["usage"] = usage

	result, _ := json.Marshal(out)
	return result
}
