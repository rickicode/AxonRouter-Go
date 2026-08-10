package openai

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/tidwall/gjson"
)

// kiroStreamState holds per-stream state when rebuilding OpenAI SSE chunks
// from Kiro SSE text events.
type kiroStreamState struct {
	responseID string
	created    int64
	chunkIndex int
}

func ensureKiroState(param *any) *kiroStreamState {
	if param != nil && *param != nil {
		if s, ok := (*param).(*kiroStreamState); ok {
			return s
		}
	}
	s := &kiroStreamState{}
	if param != nil {
		*param = s
	}
	return s
}

// convertKiroResponseToOpenAIStream converts Kiro streaming chunks to OpenAI format.
//
// Kiro normally uses OpenAI-format SSE, so the default path is a passthrough.
// As a fallback, it also accepts Kiro text SSE events (lines starting with
// "data:") containing an assistantResponseEvent payload and rebuilds them as
// OpenAI chat.completion.chunk SSE frames.
func convertKiroResponseToOpenAIStream(_ context.Context, model string, _, _, rawChunk []byte, param *any) [][]byte {
	trimmed := bytes.TrimSpace(rawChunk)
	if !bytes.HasPrefix(trimmed, []byte("data:")) {
		return [][]byte{append(rawChunk, "\n\n"...)}
	}

	state := ensureKiroState(param)
	if state.responseID == "" {
		state.responseID = "chatcmpl-" + hex.EncodeToString(randBytes(8))
		state.created = time.Now().Unix()
	}

	var out [][]byte
	for _, line := range bytes.Split(rawChunk, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}

		payload := bytes.TrimSpace(line[5:])
		if bytes.Equal(payload, []byte("[DONE]")) {
			out = append(out, []byte("data: [DONE]\n\n"))
			continue
		}

		event := gjson.ParseBytes(payload).Get("assistantResponseEvent")
		if !event.Exists() {
			// Not a recognized Kiro event line; preserve it unchanged.
			out = append(out, append(bytes.TrimSuffix(line, []byte("\r")), "\n\n"...))
			continue
		}

		content := event.Get("content").String()
		if content == "" {
			continue
		}

		delta := map[string]any{"content": content}
		if state.chunkIndex == 0 {
			delta["role"] = "assistant"
		}
		chunk := map[string]any{
			"id":      state.responseID,
			"object":  "chat.completion.chunk",
			"created": state.created,
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
		out = append(out, []byte("data: "+string(b)+"\n\n"))
		state.chunkIndex++
	}
	return out
}

// convertKiroResponseToOpenAINonStream converts a complete Kiro response to OpenAI format.
// Kiro uses OpenAI format natively, so this is a passthrough.
func convertKiroResponseToOpenAINonStream(_ context.Context, _ string, _, _, rawResponse []byte, _ *any) []byte {
	return rawResponse
}

func randBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}
