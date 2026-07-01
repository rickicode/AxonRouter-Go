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
	CachedTokens    int64
}

// ExtractTokensFromFinalChunk extracts token counts from the final SSE chunk.
// Supports OpenAI, Claude, and Gemini formats (including cached tokens).
func ExtractTokensFromFinalChunk(chunk []byte) StreamTokenCounts {
	var counts StreamTokenCounts

	// Try OpenAI format: {"usage": {"prompt_tokens": N, "completion_tokens": N, "prompt_tokens_details": {"cached_tokens": N}}}
	var openai struct {
		Usage struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
			TotalTokens      int64 `json:"total_tokens"`
			PromptTokensDetails *struct {
				CachedTokens int64 `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
			CompletionTokensDetails *struct {
				ReasoningTokens int64 `json:"reasoning_tokens"`
			} `json:"completion_tokens_details"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(chunk, &openai); err == nil && openai.Usage.PromptTokens > 0 {
		counts.InputTokens = openai.Usage.PromptTokens
		counts.OutputTokens = openai.Usage.CompletionTokens
		if openai.Usage.PromptTokensDetails != nil {
			counts.CachedTokens = openai.Usage.PromptTokensDetails.CachedTokens
		}
		if openai.Usage.CompletionTokensDetails != nil {
			counts.ReasoningTokens = openai.Usage.CompletionTokensDetails.ReasoningTokens
		}
		return counts
	}

	// Try Claude format: {"message": {"usage": {"input_tokens": N, "output_tokens": N, "cache_creation_input_tokens": N, "cache_read_input_tokens": N}}}
	var claude struct {
		Message struct {
			Usage struct {
				InputTokens              int64 `json:"input_tokens"`
				OutputTokens             int64 `json:"output_tokens"`
				CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
				CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
			} `json:"usage"`
		} `json:"message"`
	}
	if err := json.Unmarshal(chunk, &claude); err == nil && claude.Message.Usage.InputTokens > 0 {
		counts.InputTokens = claude.Message.Usage.InputTokens
		counts.OutputTokens = claude.Message.Usage.OutputTokens
		counts.CachedTokens = claude.Message.Usage.CacheCreationInputTokens + claude.Message.Usage.CacheReadInputTokens
		return counts
	}

	// Try Gemini format: {"usageMetadata": {"promptTokenCount": N, "candidatesTokenCount": N, "cachedContentTokenCount": N}}
	var gemini struct {
		UsageMetadata struct {
			PromptTokenCount        int64 `json:"promptTokenCount"`
			CandidatesTokenCount    int64 `json:"candidatesTokenCount"`
			CachedContentTokenCount int64 `json:"cachedContentTokenCount"`
			ThoughtsTokenCount      int64 `json:"thoughtsTokenCount"`
		} `json:"usageMetadata"`
	}
	if err := json.Unmarshal(chunk, &gemini); err == nil && gemini.UsageMetadata.PromptTokenCount > 0 {
		counts.InputTokens = gemini.UsageMetadata.PromptTokenCount
		counts.OutputTokens = gemini.UsageMetadata.CandidatesTokenCount
		counts.CachedTokens = gemini.UsageMetadata.CachedContentTokenCount
		counts.ReasoningTokens = gemini.UsageMetadata.ThoughtsTokenCount
		return counts
	}

	return counts
}

// ExtractTokensFromBody extracts token counts from a non-streaming OpenAI-format response body.
// The translated response has usage at top level: {"usage": {"prompt_tokens": N, ...}}
func ExtractTokensFromBody(body []byte) StreamTokenCounts {
	var counts StreamTokenCounts
	var resp struct {
		Usage struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
			PromptTokensDetails *struct {
				CachedTokens int64 `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
			CompletionTokensDetails *struct {
				ReasoningTokens int64 `json:"reasoning_tokens"`
			} `json:"completion_tokens_details"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &resp); err != nil || resp.Usage.PromptTokens == 0 {
		return counts
	}
	counts.InputTokens = resp.Usage.PromptTokens
	counts.OutputTokens = resp.Usage.CompletionTokens
	if resp.Usage.PromptTokensDetails != nil {
		counts.CachedTokens = resp.Usage.PromptTokensDetails.CachedTokens
	}
	if resp.Usage.CompletionTokensDetails != nil {
		counts.ReasoningTokens = resp.Usage.CompletionTokensDetails.ReasoningTokens
	}
	return counts
}

// IsFinalChunk checks if a chunk is the final chunk in a stream.
// Looks for [DONE] marker or end of stream indicators.
func IsFinalChunk(chunk []byte) bool {
	s := strings.TrimSpace(string(chunk))
	return s == "[DONE]" || s == "data: [DONE]"
}
