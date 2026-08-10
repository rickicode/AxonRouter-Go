package claude

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"time"

	claudeopenai "github.com/rickicode/AxonRouter-Go/internal/translator/claude/openai"
	"github.com/tidwall/gjson"
)

var dataPrefix = []byte("data:")

// ConvertKiroResponseToClaudeStream converts Kiro streaming chunks into
// Anthropic Messages API SSE events.
//
// Kiro usually returns OpenAI-format chat.completion.chunk SSE, so the default
// path reuses the existing OpenAI→Claude streaming converter. As a fallback, it
// also accepts Kiro assistantResponseEvent text events and rebuilds them as
// OpenAI chunks before translation. This guarantees that reasoning content,
// tool calls, text deltas, and stop reasons are all mapped to the equivalent
// Claude SSE frames.
func ConvertKiroResponseToClaudeStream(ctx context.Context, model string, originalReq, translatedReq, rawChunk []byte, param *any) [][]byte {
	if !bytes.HasPrefix(bytes.TrimSpace(rawChunk), dataPrefix) {
		return [][]byte{append(rawChunk, "\n\n"...)}
	}

	var out [][]byte
	for _, rawLine := range bytes.Split(rawChunk, []byte("\n")) {
		line := bytes.TrimSpace(rawLine)
		if len(line) == 0 {
			continue
		}
		if !bytes.HasPrefix(line, dataPrefix) {
			out = append(out, append(line, "\n\n"...))
			continue
		}

		payload := bytes.TrimSpace(line[len(dataPrefix):])
		if bytes.Equal(payload, []byte("[DONE]")) {
			chunks := claudeopenai.ConvertOpenAIResponseToClaudeStream(ctx, model, originalReq, translatedReq, []byte("data: [DONE]\n\n"), param)
			out = append(out, chunks...)
			continue
		}

		root := gjson.ParseBytes(payload)
		event := root.Get("assistantResponseEvent")
		var openaiChunk []byte
		if event.Exists() {
			content := event.Get("content").String()
			if content == "" {
				continue
			}
			openaiChunk = buildFallbackOpenAIChunk(model, content)
		} else {
			openaiChunk = payload
		}

		wrapped := append([]byte("data: "), openaiChunk...)
		wrapped = append(wrapped, '\n', '\n')
		chunks := claudeopenai.ConvertOpenAIResponseToClaudeStream(ctx, model, originalReq, translatedReq, wrapped, param)
		out = append(out, chunks...)
	}
	return out
}

func buildFallbackOpenAIChunk(model, content string) []byte {
	delta := map[string]any{"content": content, "role": "assistant"}
	chunk := map[string]any{
		"id":      "chatcmpl-" + hex.EncodeToString(randBytes(8)),
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []any{
			map[string]any{
				"index":         0,
				"delta":         delta,
				"finish_reason": nil,
			},
		},
	}
	b, _ := json.Marshal(chunk)
	return b
}

// ConvertKiroResponseToClaudeNonStream converts a complete Kiro response into a
// single Claude Messages API response object. Kiro usually returns OpenAI-format
// chat.completion JSON, so the default path reuses the existing OpenAI→Claude
// non-stream converter. When the payload is a Kiro assistantResponseEvent, it is
// first rebuilt into an OpenAI response.
func ConvertKiroResponseToClaudeNonStream(ctx context.Context, model string, originalReq, translatedReq, rawResponse []byte, param *any) []byte {
	if !gjson.ValidBytes(rawResponse) {
		return rawResponse
	}
	if event := gjson.ParseBytes(rawResponse).Get("assistantResponseEvent"); event.Exists() {
		content := event.Get("content").String()
		rawResponse = buildFallbackOpenAIResponse(model, content)
	}
	return claudeopenai.ConvertOpenAIResponseToClaudeNonStream(ctx, model, originalReq, translatedReq, rawResponse, param)
}

func buildFallbackOpenAIResponse(model, content string) []byte {
	resp := map[string]any{
		"id":      "chatcmpl-" + hex.EncodeToString(randBytes(8)),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []any{
			map[string]any{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": content,
				},
				"finish_reason": "stop",
			},
		},
	}
	b, _ := json.Marshal(resp)
	return b
}

func randBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}
