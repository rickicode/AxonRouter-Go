package v1

import (
	"bytes"
	"encoding/json"
	"strings"
)

// StreamTokenCounts holds extracted token counts from streaming response.
type StreamTokenCounts struct {
	InputTokens        int64
	OutputTokens       int64
	ReasoningTokens    int64
	CachedTokens       int64 // cache READ only
	CacheCreationTokens int64 // cache WRITE (new)
}

// ExtractTokensFromFinalChunk extracts token counts from the final SSE chunk.
// Supports OpenAI, Claude, and Gemini formats (including cached tokens).
func ExtractTokensFromFinalChunk(chunk []byte) StreamTokenCounts {
	var counts StreamTokenCounts

	// SSE chunks may still carry the leading `data: ` prefix.
	if strings.HasPrefix(string(chunk), "data: ") {
		chunk = chunk[len("data: "):]
	}

	// Try OpenAI format: {"usage": {"prompt_tokens": N, "completion_tokens": N, "prompt_tokens_details": {"cached_tokens": N, "cache_creation_tokens": N}}}
	var openai struct {
		Usage struct {
			PromptTokens int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
			TotalTokens int64 `json:"total_tokens"`
			PromptTokensDetails *struct {
				CachedTokens int64 `json:"cached_tokens"`
				CacheCreationTokens int64 `json:"cache_creation_tokens"`
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
			counts.CacheCreationTokens = openai.Usage.PromptTokensDetails.CacheCreationTokens
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
				InputTokens int64 `json:"input_tokens"`
				OutputTokens int64 `json:"output_tokens"`
				CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
				CacheReadInputTokens int64 `json:"cache_read_input_tokens"`
			} `json:"usage"`
		} `json:"message"`
	}
	if err := json.Unmarshal(chunk, &claude); err == nil && claude.Message.Usage.InputTokens > 0 {
		counts.InputTokens = claude.Message.Usage.InputTokens + claude.Message.Usage.CacheCreationInputTokens + claude.Message.Usage.CacheReadInputTokens
		counts.OutputTokens = claude.Message.Usage.OutputTokens
		counts.CachedTokens = claude.Message.Usage.CacheReadInputTokens
		counts.CacheCreationTokens = claude.Message.Usage.CacheCreationInputTokens
		return counts
	}

	// Try Gemini format: {"usageMetadata": {"promptTokenCount": N, "candidatesTokenCount": N, "cachedContentTokenCount": N}}
	var gemini struct {
		UsageMetadata struct {
			PromptTokenCount int64 `json:"promptTokenCount"`
			CandidatesTokenCount int64 `json:"candidatesTokenCount"`
			CachedContentTokenCount int64 `json:"cachedContentTokenCount"`
			ThoughtsTokenCount int64 `json:"thoughtsTokenCount"`
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

// ExtractTokensFromBody extracts token counts from a non-streaming response body.
// Handles OpenAI-format (emitted by translators), native Claude-format usage,
// Gemini usageMetadata, and OpenAI Responses API usage shapes.
func ExtractTokensFromBody(body []byte) StreamTokenCounts {
	var counts StreamTokenCounts

	// OpenAI-format (also what translators emit).
	var resp struct {
		Usage struct {
			PromptTokens int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
			TotalTokens int64 `json:"total_tokens"`
			InputTokens int64 `json:"input_tokens"`
			OutputTokens int64 `json:"output_tokens"`
			PromptTokensDetails *struct {
				CachedTokens int64 `json:"cached_tokens"`
				CacheCreationTokens int64 `json:"cache_creation_tokens"`
			} `json:"prompt_tokens_details"`
			CompletionTokensDetails *struct {
				ReasoningTokens int64 `json:"reasoning_tokens"`
			} `json:"completion_tokens_details"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &resp); err == nil {
		if resp.Usage.PromptTokens > 0 || resp.Usage.CompletionTokens > 0 {
			counts.InputTokens = resp.Usage.PromptTokens
			counts.OutputTokens = resp.Usage.CompletionTokens
		} else if resp.Usage.InputTokens > 0 || resp.Usage.OutputTokens > 0 || resp.Usage.TotalTokens > 0 {
			counts.InputTokens = resp.Usage.InputTokens
			counts.OutputTokens = resp.Usage.OutputTokens
		}
		if resp.Usage.PromptTokensDetails != nil {
			counts.CachedTokens = resp.Usage.PromptTokensDetails.CachedTokens
			counts.CacheCreationTokens = resp.Usage.PromptTokensDetails.CacheCreationTokens
		}
		if resp.Usage.CompletionTokensDetails != nil {
			counts.ReasoningTokens = resp.Usage.CompletionTokensDetails.ReasoningTokens
		}
		if counts.InputTokens > 0 {
			return counts
		}
	}

	// Native Claude-format usage (input_tokens is base only; cache is reported separately).
	var claude struct {
		Usage struct {
			InputTokens int64 `json:"input_tokens"`
			OutputTokens int64 `json:"output_tokens"`
			CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
			CacheReadInputTokens int64 `json:"cache_read_input_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &claude); err == nil && claude.Usage.InputTokens > 0 {
		counts.InputTokens = claude.Usage.InputTokens + claude.Usage.CacheCreationInputTokens + claude.Usage.CacheReadInputTokens
		counts.OutputTokens = claude.Usage.OutputTokens
		counts.CachedTokens = claude.Usage.CacheReadInputTokens
		counts.CacheCreationTokens = claude.Usage.CacheCreationInputTokens
		return counts
	}

	// Gemini non-streaming format: {"usageMetadata": {"promptTokenCount": N, "candidatesTokenCount": N, ...}}
	var gemini struct {
		UsageMetadata *struct {
			PromptTokenCount int64 `json:"promptTokenCount"`
			CandidatesTokenCount int64 `json:"candidatesTokenCount"`
			CachedContentTokenCount int64 `json:"cachedContentTokenCount"`
			ThoughtsTokenCount int64 `json:"thoughtsTokenCount"`
		} `json:"usageMetadata"`
	}
	if err := json.Unmarshal(body, &gemini); err == nil && gemini.UsageMetadata != nil && gemini.UsageMetadata.PromptTokenCount > 0 {
		counts.InputTokens = gemini.UsageMetadata.PromptTokenCount
		counts.OutputTokens = gemini.UsageMetadata.CandidatesTokenCount
		counts.CachedTokens = gemini.UsageMetadata.CachedContentTokenCount
		counts.ReasoningTokens = gemini.UsageMetadata.ThoughtsTokenCount
		return counts
	}

	// OpenAI Responses API: {"response": {"usage": {"input_tokens": N, "output_tokens": N}}}
	var responsesAPI struct {
		Response *struct {
			Usage *struct {
				InputTokens int64 `json:"input_tokens"`
				OutputTokens int64 `json:"output_tokens"`
				TotalTokens int64 `json:"total_tokens"`
			} `json:"usage"`
		} `json:"response"`
	}
	if err := json.Unmarshal(body, &responsesAPI); err == nil && responsesAPI.Response != nil && responsesAPI.Response.Usage != nil {
		u := responsesAPI.Response.Usage
		if u.InputTokens > 0 || u.OutputTokens > 0 || u.TotalTokens > 0 {
			counts.InputTokens = u.InputTokens
			counts.OutputTokens = u.OutputTokens
			return counts
		}
	}

	return counts
}

// ExtractTokensFromSSEChunk attempts to extract token counts from a single SSE data line.
// Returns false for non-data lines, [DONE] markers, and chunks without token information.
// Supports OpenAI usage, Claude message_start/message_delta, Gemini usageMetadata,
// and Codex/OpenAI Responses API response.completed/done shapes.
func ExtractTokensFromSSEChunk(line []byte) (StreamTokenCounts, bool) {
	var counts StreamTokenCounts

	// Only process data lines.
	if !bytes.HasPrefix(line, []byte("data:")) {
		return counts, false
	}
	chunk := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
	if len(chunk) == 0 || string(chunk) == "[DONE]" {
		return counts, false
	}

	// Try OpenAI format: {"usage": {"prompt_tokens": N, "completion_tokens": N, ...}}
	var openai struct {
		Usage *struct {
			PromptTokens int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
			PromptTokensDetails *struct {
				CachedTokens int64 `json:"cached_tokens"`
				CacheCreationTokens int64 `json:"cache_creation_tokens"`
			} `json:"prompt_tokens_details"`
			CompletionTokensDetails *struct {
				ReasoningTokens int64 `json:"reasoning_tokens"`
			} `json:"completion_tokens_details"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(chunk, &openai); err == nil && openai.Usage != nil && openai.Usage.PromptTokens > 0 {
		counts.InputTokens = openai.Usage.PromptTokens
		counts.OutputTokens = openai.Usage.CompletionTokens
		if openai.Usage.PromptTokensDetails != nil {
			counts.CachedTokens = openai.Usage.PromptTokensDetails.CachedTokens
			counts.CacheCreationTokens = openai.Usage.PromptTokensDetails.CacheCreationTokens
		}
		if openai.Usage.CompletionTokensDetails != nil {
			counts.ReasoningTokens = openai.Usage.CompletionTokensDetails.ReasoningTokens
		}
		return counts, true
	}

	// Try Claude message_start format: {"type":"message_start","message":{"usage":{"input_tokens":N,...}}}
	var claudeStart struct {
		Type    string `json:"type"`
		Message *struct {
			Usage *struct {
				InputTokens            int64 `json:"input_tokens"`
				OutputTokens           int64 `json:"output_tokens"`
				CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
				CacheReadInputTokens    int64 `json:"cache_read_input_tokens"`
			} `json:"usage"`
		} `json:"message"`
	}
	if err := json.Unmarshal(chunk, &claudeStart); err == nil && claudeStart.Type == "message_start" && claudeStart.Message != nil && claudeStart.Message.Usage != nil && claudeStart.Message.Usage.InputTokens > 0 {
		u := claudeStart.Message.Usage
		counts.InputTokens = u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens
		counts.OutputTokens = u.OutputTokens
		counts.CachedTokens = u.CacheReadInputTokens
		counts.CacheCreationTokens = u.CacheCreationInputTokens
		return counts, true
	}

	// Try Claude message_delta format: {"type":"message_delta","usage":{"output_tokens":N}}
	var claudeDelta struct {
		Type  string `json:"type"`
		Usage *struct {
			OutputTokens int64 `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(chunk, &claudeDelta); err == nil && claudeDelta.Type == "message_delta" && claudeDelta.Usage != nil && claudeDelta.Usage.OutputTokens > 0 {
		counts.OutputTokens = claudeDelta.Usage.OutputTokens
		return counts, true
	}

	// Try Gemini format: {"usageMetadata": {"promptTokenCount": N, "candidatesTokenCount": N, ...}}
	var gemini struct {
		UsageMetadata *struct {
			PromptTokenCount      int64 `json:"promptTokenCount"`
			CandidatesTokenCount  int64 `json:"candidatesTokenCount"`
			CachedContentTokenCount int64 `json:"cachedContentTokenCount"`
			ThoughtsTokenCount    int64 `json:"thoughtsTokenCount"`
		} `json:"usageMetadata"`
	}
	if err := json.Unmarshal(chunk, &gemini); err == nil && gemini.UsageMetadata != nil && gemini.UsageMetadata.PromptTokenCount > 0 {
		counts.InputTokens = gemini.UsageMetadata.PromptTokenCount
		counts.OutputTokens = gemini.UsageMetadata.CandidatesTokenCount
		counts.CachedTokens = gemini.UsageMetadata.CachedContentTokenCount
		counts.ReasoningTokens = gemini.UsageMetadata.ThoughtsTokenCount
		return counts, true
	}

	// Try Codex/OpenAI Responses API format:
	//   {"type":"response.completed","response":{"usage":{"input_tokens":N,"output_tokens":N}}}
	//   {"done":true,"response":{"usage":{...}}}
	type chunkUsage struct {
		InputTokens  int64 `json:"input_tokens"`
		OutputTokens int64 `json:"output_tokens"`
		TotalTokens  int64 `json:"total_tokens"`
	}
	var codex struct {
		Type     string `json:"type"`
		Done     *bool  `json:"done"`
		Response *struct {
			Usage *chunkUsage `json:"usage"`
		} `json:"response"`
	}
	if err := json.Unmarshal(chunk, &codex); err == nil {
		isCompleted := codex.Type == "response.completed" || (codex.Done != nil && *codex.Done)
		if isCompleted && codex.Response != nil && codex.Response.Usage != nil {
			u := codex.Response.Usage
			if u.InputTokens > 0 || u.OutputTokens > 0 || u.TotalTokens > 0 {
				counts.InputTokens = u.InputTokens
				counts.OutputTokens = u.OutputTokens
				return counts, true
			}
		}
	}

	return counts, false
}

// MergeTokenCounts copies non-zero fields from src into dst.
// Matches OmniRoute's mergeUsage pattern where incoming non-zero values overwrite target.
func MergeTokenCounts(dst, src *StreamTokenCounts) {
	if src.InputTokens > 0 {
		dst.InputTokens = src.InputTokens
	}
	if src.OutputTokens > 0 {
		dst.OutputTokens = src.OutputTokens
	}
	if src.ReasoningTokens > 0 {
		dst.ReasoningTokens = src.ReasoningTokens
	}
	if src.CachedTokens > 0 {
		dst.CachedTokens = src.CachedTokens
	}
	if src.CacheCreationTokens > 0 {
		dst.CacheCreationTokens = src.CacheCreationTokens
	}
}

// IsFinalChunk checks if a chunk is the final chunk in a stream.
// Looks for [DONE] marker or end of stream indicators.
func IsFinalChunk(chunk []byte) bool {
	s := strings.TrimSpace(string(chunk))
	return s == "[DONE]" || s == "data: [DONE]"
}
