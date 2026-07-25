package antigravity

import (
	"context"

	"github.com/rickicode/AxonRouter-Go/internal/translator/antigravity/claude"
)

// convertAntigravityResponseToClaudeStream translates an upstream Antigravity
// streaming chunk (Gemini / Cloud Code Assist SSE) into Claude Messages API SSE
// events. It delegates to the reverse-direction translator in
// internal/translator/antigravity/claude so the conversion logic is kept in one
// place.
func convertAntigravityResponseToClaudeStream(ctx context.Context, model string, originalReq, translatedReq, rawChunk []byte, param *any) [][]byte {
	return claude.ConvertAntigravityResponseToClaudeStream(ctx, model, originalReq, translatedReq, rawChunk, param)
}

// convertAntigravityResponseToClaudeNonStream translates a complete Antigravity
// non-streaming response into a single Claude Messages API response object.
func convertAntigravityResponseToClaudeNonStream(ctx context.Context, model string, originalReq, translatedReq, rawResponse []byte, param *any) []byte {
	return claude.ConvertAntigravityResponseToClaudeNonStream(ctx, model, originalReq, translatedReq, rawResponse, param)
}
