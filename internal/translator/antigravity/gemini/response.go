package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// antigravityStreamState holds state for Gemini→Antigravity streaming.
type antigravityStreamState struct {
	Model       string
	CurrentText strings.Builder
	ToolCalls   []map[string]interface{}
	ToolIndex   int
}

func getStreamState(param *any) *antigravityStreamState {
	if *param == nil {
		*param = &antigravityStreamState{}
	}
	return (*param).(*antigravityStreamState)
}

// convertGeminiResponseToAntigravityStream converts Gemini streaming to Antigravity format.
func convertGeminiResponseToAntigravityStream(_ context.Context, _ string, _, _ []byte, rawChunk []byte, param *any) [][]byte {
	state := getStreamState(param)

	raw := bytes.TrimSpace(rawChunk)
	if len(raw) == 0 {
		return nil
	}

	root := gjson.ParseBytes(raw)

	if state.Model == "" {
		state.Model = root.Get("model").String()
	}

	var results [][]byte

	if candidates := root.Get("candidates"); candidates.Exists() && candidates.IsArray() {
		candidates.ForEach(func(_, candidate gjson.Result) bool {
			if parts := candidate.Get("content.parts"); parts.Exists() && parts.IsArray() {
				parts.ForEach(func(_, part gjson.Result) bool {
					if text := part.Get("text"); text.Exists() {
						state.CurrentText.WriteString(text.String())
						chunk := buildAntigravityChunk(state, text.String(), nil)
						results = append(results, chunk)
						return true
					}

					if fc := part.Get("functionCall"); fc.Exists() {
						name := fc.Get("name").String()
						args := fc.Get("args").Raw
						state.ToolCalls = append(state.ToolCalls, map[string]interface{}{
							"name": name,
							"args": json.RawMessage(args),
						})
						state.ToolIndex = len(state.ToolCalls)
						chunk := buildAntigravityChunk(state, "", state.ToolCalls)
						results = append(results, chunk)
						return true
					}
					return true
				})
			}
			return true
		})
	}

	if len(results) > 0 {
		return results
	}
	return nil
}

// convertGeminiResponseToAntigravityNonStream converts a complete Gemini response to Antigravity format.
func convertGeminiResponseToAntigravityNonStream(_ context.Context, _ string, _, _ []byte, rawResponse []byte, _ *any) []byte {
	root := gjson.ParseBytes(rawResponse)

	out := []byte(`{"response":{}}`)
	out, _ = sjson.SetBytes(out, "response.modelVersion", root.Get("model").String())

	var parts []map[string]interface{}

	if candidates := root.Get("candidates"); candidates.Exists() && candidates.IsArray() {
		candidates.ForEach(func(_, candidate gjson.Result) bool {
			if content := candidate.Get("content.parts"); content.Exists() && content.IsArray() {
				content.ForEach(func(_, part gjson.Result) bool {
					if text := part.Get("text"); text.Exists() {
						parts = append(parts, map[string]interface{}{
							"text": text.String(),
						})
					}
					if fc := part.Get("functionCall"); fc.Exists() {
						name := fc.Get("name").String()
						args := fc.Get("args").Raw
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
			return true
		})
	}

	candidatesOut := []map[string]interface{}{{
		"content": map[string]interface{}{
			"role":  "model",
			"parts": parts,
		},
		"finishReason": "STOP",
	}}
	out, _ = sjson.SetRawBytes(out, "response.candidates", mustMarshal(candidatesOut))

	// Usage
	usage := map[string]interface{}{
		"promptTokenCount":     root.Get("usageMetadata.promptTokenCount").Int(),
		"candidatesTokenCount": root.Get("usageMetadata.candidatesTokenCount").Int(),
		"totalTokenCount":      root.Get("usageMetadata.totalTokenCount").Int(),
	}
	out, _ = sjson.SetRawBytes(out, "response.usageMetadata", mustMarshal(usage))

	return out
}

func buildAntigravityChunk(state *antigravityStreamState, text string, toolCalls []map[string]interface{}) []byte {
	chunk := []byte(`{"response":{}}`)
	chunk, _ = sjson.SetBytes(chunk, "response.modelVersion", state.Model)

	var parts []map[string]interface{}
	if text != "" {
		parts = append(parts, map[string]interface{}{"text": text})
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


