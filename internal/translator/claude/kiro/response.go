package kiro

import (
	"context"

	"github.com/rickicode/AxonRouter-Go/internal/translator/kiro/claude"
)

// convertKiroResponseToClaudeStream translates upstream Kiro streaming chunks
// (OpenAI-format chat.completion.chunk SSE with an assistantResponseEvent
// fallback) into Claude Messages API SSE events. It delegates to the existing
// Kiro→Claude translator so the conversion logic is kept in one place.
func convertKiroResponseToClaudeStream(ctx context.Context, model string, originalReq, translatedReq, rawChunk []byte, param *any) [][]byte {
	return claude.ConvertKiroResponseToClaudeStream(ctx, model, originalReq, translatedReq, rawChunk, param)
}

// convertKiroResponseToClaudeNonStream translates a complete Kiro response into
// a single Claude Messages API response object.
func convertKiroResponseToClaudeNonStream(ctx context.Context, model string, originalReq, translatedReq, rawResponse []byte, param *any) []byte {
	return claude.ConvertKiroResponseToClaudeNonStream(ctx, model, originalReq, translatedReq, rawResponse, param)
}
