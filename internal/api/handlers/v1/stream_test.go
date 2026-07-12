package v1

import "testing"

func TestExtractTokensFromFinalChunk(t *testing.T) {
	cases := []struct {
		name  string
		chunk string
		want  StreamTokenCounts
	}{
		{
			name:  "openai sse with data prefix",
			chunk: `data: {"usage":{"prompt_tokens":10,"completion_tokens":5}}`,
			want:  StreamTokenCounts{InputTokens: 10, OutputTokens: 5},
		},
		{
			name:  "openai raw json without prefix",
			chunk: `{"usage":{"prompt_tokens":7,"completion_tokens":3,"prompt_tokens_details":{"cached_tokens":2},"completion_tokens_details":{"reasoning_tokens":1}}}`,
			want:  StreamTokenCounts{InputTokens: 7, OutputTokens: 3, CachedTokens: 2, ReasoningTokens: 1},
		},
		{
			name:  "claude sse",
			chunk: `data: {"message":{"usage":{"input_tokens":4,"output_tokens":8,"cache_creation_input_tokens":1,"cache_read_input_tokens":2}}}`,
			want:  StreamTokenCounts{InputTokens: 4, OutputTokens: 8, CachedTokens: 3},
		},
		{
			name:  "gemini sse",
			chunk: `data: {"usageMetadata":{"promptTokenCount":9,"candidatesTokenCount":2,"cachedContentTokenCount":5,"thoughtsTokenCount":1}}`,
			want:  StreamTokenCounts{InputTokens: 9, OutputTokens: 2, CachedTokens: 5, ReasoningTokens: 1},
		},
		{
			name:  "done marker returns zero",
			chunk: "data: [DONE]",
			want:  StreamTokenCounts{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractTokensFromFinalChunk([]byte(tc.chunk))
			if got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestExtractTokensFromBody(t *testing.T) {
	cases := []struct {
		name string
		body string
		want StreamTokenCounts
	}{
		{
			name: "openai usage prompt_tokens",
			body: `{"usage":{"prompt_tokens":11,"completion_tokens":4}}`,
			want: StreamTokenCounts{InputTokens: 11, OutputTokens: 4},
		},
		{
			name: "normalized internal usage input_tokens",
			body: `{"usage":{"input_tokens":6,"output_tokens":7}}`,
			want: StreamTokenCounts{InputTokens: 6, OutputTokens: 7},
		},
		{
			name: "completion only still counted",
			body: `{"usage":{"prompt_tokens":0,"completion_tokens":5,"total_tokens":5}}`,
			want: StreamTokenCounts{InputTokens: 0, OutputTokens: 5},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractTokensFromBody([]byte(tc.body))
			if got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}
