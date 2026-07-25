// Package claude provides Antigravity (Gemini Cloud Code Assist) → Claude response
// translation. It emits the standard Claude SSE event sequence
// (message_start, content_block_start/delta/stop, message_delta, message_stop)
// from Antigravity streaming chunks and a single Claude Messages response object
// from non-streaming responses.
package claude

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/cache"
	"github.com/rickicode/AxonRouter-Go/internal/signature"
	"github.com/rickicode/AxonRouter-Go/internal/translator/antigravity"
	"github.com/rickicode/AxonRouter-Go/internal/translator/antigravity/openai"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// streamState tracks the currently-open Claude content_block across chunks.
type streamState struct {
	DoneFirst           bool
	BlockType           string // "text" | "thinking" | "tool_use"
	BlockIndex          int
	ToolNameMap         map[string]string // cloaked -> original
	SawToolUse          bool
	HasContent          bool
	SentFinish          bool
	FinishReason        string
	PromptTokens        int64
	CandidateTokens     int64
	ThoughtTokens       int64
	TotalTokens         int64
	CachedTokens        int64
	CurrentThinkingText strings.Builder
	CurrentThinkingSig  string
}

var toolUseIDCounter uint64

func getState(param *any) *streamState {
	if *param == nil {
		*param = &streamState{}
	}
	return (*param).(*streamState)
}

// convertAntigravityResponseToClaudeStream translates one upstream Antigravity SSE
// chunk into zero or more Claude SSE events.
func convertAntigravityResponseToClaudeStream(_ context.Context, _ string, originalRequestRawJSON, _ []byte, rawChunk []byte, param *any) [][]byte {
	state := getState(param)
	if state.ToolNameMap == nil {
		state.ToolNameMap = buildToolNameMap(originalRequestRawJSON)
	}

	data := bytes.TrimSpace(rawChunk)
	if bytes.HasPrefix(data, []byte("data:")) {
		data = bytes.TrimSpace(data[5:])
	}
	if len(data) == 0 {
		return nil
	}
	if bytes.Equal(data, []byte("[DONE]")) {
		out := finishStream(state, true)
		if len(out) == 0 {
			return nil
		}
		return [][]byte{out}
	}

	var output []byte
	appendEvent := func(event string, payload []byte) {
		output = append(output, "event: "...)
		output = append(output, event...)
		output = append(output, '\n')
		output = append(output, "data: "...)
		output = append(output, payload...)
		output = append(output, "\n\n"...)
	}

	root := gjson.ParseBytes(data)

	if !state.DoneFirst {
		msgStart := []byte(`{"type":"message_start","message":{"id":"msg_01axon","type":"message","role":"assistant","content":[],"model":"claude-3-5-sonnet-20241022","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":0,"output_tokens":0}}}`)
		if model := root.Get("response.modelVersion"); model.Exists() {
			msgStart, _ = sjson.SetBytes(msgStart, "message.model", model.String())
		}
		if id := root.Get("response.responseId"); id.Exists() {
			msgStart, _ = sjson.SetBytes(msgStart, "message.id", id.String())
		}
		readUsageIntoStart(root, msgStart, appendEvent)
		state.DoneFirst = true
	}

	if usage := root.Get("response.usageMetadata"); usage.Exists() {
		state.PromptTokens = usage.Get("promptTokenCount").Int()
		state.CandidateTokens = usage.Get("candidatesTokenCount").Int()
		state.ThoughtTokens = usage.Get("thoughtsTokenCount").Int()
		state.TotalTokens = usage.Get("totalTokenCount").Int()
		state.CachedTokens = usage.Get("cachedContentTokenCount").Int()
	}
	if fr := root.Get("response.candidates.0.finishReason"); fr.Exists() {
		state.FinishReason = fr.String()
	}

	modelName := root.Get("response.modelVersion").String()

	parts := root.Get("response.candidates.0.content.parts")
	if parts.IsArray() {
		for _, part := range parts.Array() {
			textResult := part.Get("text")
			fcResult := part.Get("functionCall")
			sigResult := part.Get("thoughtSignature")
			if !sigResult.Exists() {
				sigResult = part.Get("thought_signature")
			}
			hasSig := sigResult.Exists() && sigResult.String() != ""
			isThought := part.Get("thought").Bool()

			if isThought && textResult.Exists() {
				if state.BlockType != "thinking" {
					closeBlock(state, appendEvent)
					startBlock(state, "thinking", nil, appendEvent)
				}
				thinkingText := textResult.String()
				state.CurrentThinkingText.WriteString(thinkingText)
				delta := []byte(fmt.Sprintf(`{"type":"content_block_delta","index":%d,"delta":{"type":"thinking_delta","thinking":""}}`, state.BlockIndex))
				delta, _ = sjson.SetBytes(delta, "delta.thinking", thinkingText)
				appendEvent("content_block_delta", delta)
				state.HasContent = true
				if hasSig {
					state.CurrentThinkingSig = sigResult.String()
				}
				continue
			}

			if textResult.Exists() {
				if state.BlockType != "text" {
					closeBlock(state, appendEvent)
					startBlock(state, "text", nil, appendEvent)
				}
				text := textResult.String()
				delta := []byte(fmt.Sprintf(`{"type":"content_block_delta","index":%d,"delta":{"type":"text_delta","text":""}}`, state.BlockIndex))
				delta, _ = sjson.SetBytes(delta, "delta.text", text)
				appendEvent("content_block_delta", delta)
				state.HasContent = true
				continue
			}

			if fcResult.Exists() {
				closeBlock(state, appendEvent)
				state.SawToolUse = true
				name := antigravity.UncloakName(restoreToolName(state.ToolNameMap, fcResult.Get("name").String()))
				toolID := newToolUseID(name)
				inputData := fcResult.Get("args").Raw
				if inputData == "" {
					inputData = "{}"
				}
				startBlock(state, "tool_use", map[string]any{
					"id":    toolID,
					"name":  name,
					"input": json.RawMessage(inputData),
				}, appendEvent)
				if inputData != "{}" {
					delta := []byte(fmt.Sprintf(`{"type":"content_block_delta","index":%d,"delta":{"type":"input_json_delta","partial_json":""}}`, state.BlockIndex))
					delta, _ = sjson.SetBytes(delta, "delta.partial_json", inputData)
					appendEvent("content_block_delta", delta)
				}
				state.HasContent = true
				continue
			}

			if hasSig && state.BlockType == "thinking" {
				emitSignatureDelta(state, modelName, sigResult.String(), appendEvent)
			}
		}
	}

	if state.FinishReason != "" {
		out := finishStream(state, false)
		if len(out) > 0 {
			output = append(output, out...)
		}
	}

	if len(output) == 0 {
		return nil
	}
	return [][]byte{output}
}

// convertAntigravityResponseToClaudeNonStream converts a complete Antigravity
// generateContent response into a single Claude Messages response object.
func convertAntigravityResponseToClaudeNonStream(_ context.Context, _ string, originalRequestRawJSON, _ []byte, rawJSON []byte, _ *any) []byte {
	root := gjson.ParseBytes(rawJSON)
	modelName := root.Get("response.modelVersion").String()

	toolNameMap := buildToolNameMap(originalRequestRawJSON)

	usage := root.Get("response.usageMetadata")
	promptTokens := usage.Get("promptTokenCount").Int()
	candidateTokens := usage.Get("candidatesTokenCount").Int()
	thoughtTokens := usage.Get("thoughtsTokenCount").Int()
	totalTokens := usage.Get("totalTokenCount").Int()
	cachedTokens := usage.Get("cachedContentTokenCount").Int()
	outputTokens := candidateTokens + thoughtTokens
	if outputTokens == 0 && totalTokens > 0 {
		outputTokens = totalTokens - promptTokens
		if outputTokens < 0 {
			outputTokens = 0
		}
	}

	finish := root.Get("response.candidates.0.finishReason").String()
	hasToolCall := false

	resp := []byte(`{"id":"","type":"message","role":"assistant","model":"","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":0,"output_tokens":0}}`)
	resp, _ = sjson.SetBytes(resp, "id", root.Get("response.responseId").String())
	resp, _ = sjson.SetBytes(resp, "model", modelName)
	resp, _ = sjson.SetBytes(resp, "usage.input_tokens", promptTokens)
	resp, _ = sjson.SetBytes(resp, "usage.output_tokens", outputTokens)
	if cachedTokens > 0 {
		resp, _ = sjson.SetBytes(resp, "usage.cache_read_input_tokens", cachedTokens)
	}

	var contentBlocks []map[string]any
	parts := root.Get("response.candidates.0.content.parts")
	if parts.IsArray() {
		for _, part := range parts.Array() {
			textResult := part.Get("text")
			fcResult := part.Get("functionCall")
			sigResult := part.Get("thoughtSignature")
			if !sigResult.Exists() {
				sigResult = part.Get("thought_signature")
			}
			isThought := part.Get("thought").Bool()

			if isThought && textResult.Exists() {
				block := map[string]any{
					"type":     "thinking",
					"thinking": textResult.String(),
				}
				if sig := sigResult.String(); sig != "" {
					block["signature"] = formatClaudeSignatureValue(modelName, sig)
				}
				contentBlocks = append(contentBlocks, block)
				continue
			}
			if textResult.Exists() {
				contentBlocks = append(contentBlocks, map[string]any{
					"type": "text",
					"text": textResult.String(),
				})
				continue
			}
			if fcResult.Exists() {
				hasToolCall = true
				name := antigravity.UncloakName(restoreToolName(toolNameMap, fcResult.Get("name").String()))
				inputData := fcResult.Get("args").Raw
				if inputData == "" {
					inputData = "{}"
				}
				var inputMap map[string]any
				_ = json.Unmarshal([]byte(inputData), &inputMap)
				if inputMap == nil {
					inputMap = map[string]any{}
				}
				contentBlocks = append(contentBlocks, map[string]any{
					"type":  "tool_use",
					"id":    newToolUseID(name),
					"name":  name,
					"input": inputMap,
				})
			}
		}
	}
	if len(contentBlocks) > 0 {
		resp, _ = sjson.SetRawBytes(resp, "content", mustMarshalJSON(contentBlocks))
	}

	stopReason := resolveStopReason(finish)
	if hasToolCall {
		stopReason = "tool_use"
	}
	resp, _ = sjson.SetBytes(resp, "stop_reason", stopReason)
	return resp
}

func readUsageIntoStart(root gjson.Result, msgStart []byte, appendEvent func(string, []byte)) {
	if usage := root.Get("response.cpaUsageMetadata"); usage.Exists() {
		if pt := usage.Get("promptTokenCount"); pt.Exists() {
			msgStart, _ = sjson.SetBytes(msgStart, "message.usage.input_tokens", pt.Int())
		}
		if ct := usage.Get("candidatesTokenCount"); ct.Exists() {
			msgStart, _ = sjson.SetBytes(msgStart, "message.usage.output_tokens", ct.Int())
		}
	} else if usage = root.Get("response.usageMetadata"); usage.Exists() {
		if pt := usage.Get("promptTokenCount"); pt.Exists() {
			msgStart, _ = sjson.SetBytes(msgStart, "message.usage.input_tokens", pt.Int())
		}
		if ct := usage.Get("candidatesTokenCount"); ct.Exists() {
			msgStart, _ = sjson.SetBytes(msgStart, "message.usage.output_tokens", ct.Int())
		}
	}
	appendEvent("message_start", msgStart)
}

func startBlock(state *streamState, blockType string, blockData map[string]any, appendEvent func(string, []byte)) {
	state.BlockType = blockType
	state.BlockIndex++
	state.CurrentThinkingText.Reset()
	state.CurrentThinkingSig = ""
	payload := []byte(fmt.Sprintf(`{"type":"content_block_start","index":%d,"content_block":{"type":"text","text":""}}`, state.BlockIndex))
	switch blockType {
	case "text":
		// already text
	case "thinking":
		payload, _ = sjson.SetBytes(payload, "content_block.type", "thinking")
		payload, _ = sjson.SetBytes(payload, "content_block.thinking", "")
		payload, _ = sjson.DeleteBytes(payload, "content_block.text")
	case "tool_use":
		payload, _ = sjson.SetBytes(payload, "content_block.type", "tool_use")
		payload, _ = sjson.SetBytes(payload, "content_block.id", "")
		payload, _ = sjson.SetBytes(payload, "content_block.name", "")
		payload, _ = sjson.SetRawBytes(payload, "content_block.input", []byte("{}"))
		payload, _ = sjson.DeleteBytes(payload, "content_block.text")
		if blockData != nil {
			if id, ok := blockData["id"].(string); ok {
				payload, _ = sjson.SetBytes(payload, "content_block.id", id)
			}
			if name, ok := blockData["name"].(string); ok {
				payload, _ = sjson.SetBytes(payload, "content_block.name", name)
			}
			if input, ok := blockData["input"]; ok {
				payload, _ = sjson.SetRawBytes(payload, "content_block.input", mustMarshalJSON(input))
			}
		}
	}
	appendEvent("content_block_start", payload)
}

func closeBlock(state *streamState, appendEvent func(string, []byte)) {
	if state.BlockType == "" {
		return
	}
	if state.BlockType == "thinking" && state.CurrentThinkingSig != "" {
		emitSignatureDelta(state, "", state.CurrentThinkingSig, appendEvent)
	}
	appendEvent("content_block_stop", []byte(fmt.Sprintf(`{"type":"content_block_stop","index":%d}`, state.BlockIndex)))
	state.BlockType = ""
}

func emitSignatureDelta(state *streamState, modelName, sig string, appendEvent func(string, []byte)) {
	if sig == "" {
		return
	}
	if state.CurrentThinkingText.Len() > 0 {
		cache.CacheSignature(modelName, state.CurrentThinkingText.String(), sig)
		state.CurrentThinkingText.Reset()
	}
	value := formatClaudeSignatureValue(modelName, sig)
	delta := []byte(fmt.Sprintf(`{"type":"content_block_delta","index":%d,"delta":{"type":"signature_delta","signature":""}}`, state.BlockIndex))
	delta, _ = sjson.SetBytes(delta, "delta.signature", value)
	appendEvent("content_block_delta", delta)
	state.HasContent = true
}

func finishStream(state *streamState, force bool) []byte {
	if state.SentFinish {
		return nil
	}
	if !state.HasContent && force {
		state.SentFinish = true
		return []byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}
	if !state.HasContent {
		return nil
	}
	var output []byte
	appendEvent := func(event string, payload []byte) {
		output = append(output, "event: "...)
		output = append(output, event...)
		output = append(output, '\n')
		output = append(output, "data: "...)
		output = append(output, payload...)
		output = append(output, "\n\n"...)
	}
	closeBlock(state, appendEvent)

	stop := resolveStopReason(state.FinishReason)
	if state.SawToolUse {
		stop = "tool_use"
	}
	outputTokens := state.CandidateTokens + state.ThoughtTokens
	if outputTokens == 0 && state.TotalTokens > 0 {
		outputTokens = state.TotalTokens - state.PromptTokens
		if outputTokens < 0 {
			outputTokens = 0
		}
	}
	delta := []byte(fmt.Sprintf(`{"type":"message_delta","delta":{"stop_reason":"%s","stop_sequence":null},"usage":{"input_tokens":%d,"output_tokens":%d}}`, stop, state.PromptTokens, outputTokens))
	if state.CachedTokens > 0 {
		delta, _ = sjson.SetBytes(delta, "usage.cache_read_input_tokens", state.CachedTokens)
	}
	appendEvent("message_delta", delta)
	appendEvent("message_stop", []byte(`{"type":"message_stop"}`))
	state.SentFinish = true
	return output
}

func resolveStopReason(finish string) string {
	switch strings.ToUpper(finish) {
	case "MAX_TOKENS":
		return "max_tokens"
	case "STOP", "FINISH_REASON_UNSPECIFIED", "UNKNOWN", "":
		return "end_turn"
	default:
		return "end_turn"
	}
}

func buildToolNameMap(rawJSON []byte) map[string]string {
	tools := gjson.GetBytes(rawJSON, "tools")
	if !tools.IsArray() {
		return nil
	}
	m := map[string]string{}
	for _, tool := range tools.Array() {
		if tool.Get("type").String() != "function" {
			continue
		}
		original := tool.Get("name").String()
		if original == "" {
			continue
		}
		m[antigravity.CloakName(openai.SanitizeFunctionName(original))] = original
	}
	return m
}

func restoreToolName(toolNameMap map[string]string, name string) string {
	if toolNameMap == nil {
		return name
	}
	if original, ok := toolNameMap[name]; ok {
		return original
	}
	return name
}

func newToolUseID(name string) string {
	return fmt.Sprintf("toolu_%s_%d_%d", name, time.Now().UnixNano(), atomic.AddUint64(&toolUseIDCounter, 1))
}

func formatClaudeSignatureValue(modelName, sig string) string {
	// Decode double-layer R-form back to single-layer E-form for native
	// Claude-style clients, otherwise keep the cache-prefixed form.
	if strings.HasPrefix(modelName, "claude-") {
		if strings.HasPrefix(sig, "R") {
			if decoded, err := base64.StdEncoding.DecodeString(sig); err == nil && len(decoded) > 0 && decoded[0] == 'E' {
				return string(decoded)
			}
		}
		return sig
	}
	if signature.HasClaudeThinkingSignaturePrefix(sig) {
		return cache.GetModelGroup(modelName) + "#" + sig
	}
	return sig
}

func mustMarshalJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
