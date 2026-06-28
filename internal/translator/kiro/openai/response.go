package openai

import "context"

// convertKiroResponseToOpenAIStream converts Kiro streaming chunks to OpenAI format.
// Kiro uses OpenAI format natively, so this is a passthrough.
func convertKiroResponseToOpenAIStream(_ context.Context, _ string, _, _, rawChunk []byte, _ *any) [][]byte {
	return [][]byte{rawChunk}
}

// convertKiroResponseToOpenAINonStream converts a complete Kiro response to OpenAI format.
// Kiro uses OpenAI format natively, so this is a passthrough.
func convertKiroResponseToOpenAINonStream(_ context.Context, _ string, _, _, rawResponse []byte, _ *any) []byte {
	return rawResponse
}
