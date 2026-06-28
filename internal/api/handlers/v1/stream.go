package v1

import (
	"encoding/json"
	"strings"
)

// StreamTokenCounts holds extracted token counts from streaming response.
type StreamTokenCounts struct {
	InputTokens     int64
	OutputTokens    int64
	ReasoningTokens int64
}

// ExtractTokensFromFinalChunk extracts token counts from the final SSE chunk.
// Supports OpenAI, Claude, and Gemini formats.
func ExtractTokensFromFinalChunk(chunk []byte) StreamTokenCounts {
	var counts StreamTokenCounts

	// Try OpenAI format: {"usage": {"prompt_tokens": N, "completion_tokens": N}}
	var openai struct {
		Usage struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
			TotalTokens      int64 `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(chunk, &openai); err == nil && openai.Usage.PromptTokens > 0 {
		counts.InputTokens = openai.Usage.PromptTokens
		counts.OutputTokens = openai.Usage.CompletionTokens
		return counts
	}

	// Try Claude format: {"message": {"usage": {"input_tokens": N, "output_tokens": N}}}
	var claude struct {
		Message struct {
			Usage struct {
				InputTokens  int64 `json:"input_tokens"`
				OutputTokens int64 `json:"output_tokens"`
			} `json:"usage"`
		} `json:"message"`
	}
	if err := json.Unmarshal(chunk, &claude); err == nil && claude.Message.Usage.InputTokens > 0 {
		counts.InputTokens = claude.Message.Usage.InputTokens
		counts.OutputTokens = claude.Message.Usage.OutputTokens
		return counts
	}

	// Try Gemini format: {"usageMetadata": {"promptTokenCount": N, "candidatesTokenCount": N}}
	var gemini struct {
		UsageMetadata struct {
			PromptTokenCount     int64 `json:"promptTokenCount"`
			CandidatesTokenCount int64 `json:"candidatesTokenCount"`
		} `json:"usageMetadata"`
	}
	if err := json.Unmarshal(chunk, &gemini); err == nil && gemini.UsageMetadata.PromptTokenCount > 0 {
		counts.InputTokens = gemini.UsageMetadata.PromptTokenCount
		counts.OutputTokens = gemini.UsageMetadata.CandidatesTokenCount
		return counts
	}

	return counts
}

// IsFinalChunk checks if a chunk is the final chunk in a stream.
// Looks for [DONE] marker or end of stream indicators.
func IsFinalChunk(chunk []byte) bool {
	s := strings.TrimSpace(string(chunk))
	return s == "[DONE]" || s == "data: [DONE]"
}
