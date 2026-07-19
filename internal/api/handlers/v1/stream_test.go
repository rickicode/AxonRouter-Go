package v1

import "testing"

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
		{
			name: "gemini usageMetadata",
			body: `{"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":10,"cachedContentTokenCount":2}}`,
			want: StreamTokenCounts{InputTokens: 5, OutputTokens: 10, CachedTokens: 2},
		},
		{
			name: "openai responses api usage input_tokens",
			body: `{"usage":{"input_tokens":8,"output_tokens":16,"total_tokens":24}}`,
			want: StreamTokenCounts{InputTokens: 8, OutputTokens: 16},
		},
		{
			name: "openai responses api nested response.usage",
			body: `{"response":{"usage":{"input_tokens":3,"output_tokens":9}}}`,
			want: StreamTokenCounts{InputTokens: 3, OutputTokens: 9},
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

func TestExtractTokensFromSSEChunk(t *testing.T) {
	cases := []struct {
		name  string
		line  string
		want  StreamTokenCounts
		found bool
	}{
		{
			name:  "non-data line returns false",
			line:  "event: message",
			want:  StreamTokenCounts{},
			found: false,
		},
		{
			name:  "empty data line returns false",
			line:  "data:",
			want:  StreamTokenCounts{},
			found: false,
		},
		{
			name:  "done marker returns false",
			line:  "data: [DONE]",
			want:  StreamTokenCounts{},
			found: false,
		},
		{
			name:  "openai usage",
			line:  `data: {"usage":{"prompt_tokens":10,"completion_tokens":5}}`,
			want:  StreamTokenCounts{InputTokens: 10, OutputTokens: 5},
			found: true,
		},
		{
			name:  "openai usage with details",
			line:  `data: {"usage":{"prompt_tokens":7,"completion_tokens":3,"prompt_tokens_details":{"cached_tokens":2},"completion_tokens_details":{"reasoning_tokens":1}}}`,
			want:  StreamTokenCounts{InputTokens: 7, OutputTokens: 3, CachedTokens: 2, ReasoningTokens: 1},
			found: true,
		},
		{
			name:  "claude message_start",
			line:  `data: {"type":"message_start","message":{"usage":{"input_tokens":4,"output_tokens":8,"cache_creation_input_tokens":1,"cache_read_input_tokens":2}}}`,
			want:  StreamTokenCounts{InputTokens: 7, OutputTokens: 8, CachedTokens: 2, CacheCreationTokens: 1},
			found: true,
		},
		{
			name:  "claude message_delta output only",
			line:  `data: {"type":"message_delta","usage":{"output_tokens":5}}`,
			want:  StreamTokenCounts{OutputTokens: 5},
			found: true,
		},
		{
			name:  "gemini usageMetadata",
			line:  `data: {"usageMetadata":{"promptTokenCount":9,"candidatesTokenCount":2,"cachedContentTokenCount":5,"thoughtsTokenCount":1}}`,
			want:  StreamTokenCounts{InputTokens: 9, OutputTokens: 2, CachedTokens: 5, ReasoningTokens: 1},
			found: true,
		},
		{
			name:  "codex response.completed with usage",
			line:  `data: {"type":"response.completed","response":{"usage":{"input_tokens":15,"output_tokens":25}}}`,
			want:  StreamTokenCounts{InputTokens: 15, OutputTokens: 25},
			found: true,
		},
		{
			name:  "codex done with usage",
			line:  `data: {"done":true,"response":{"usage":{"input_tokens":3,"output_tokens":7}}}`,
			want:  StreamTokenCounts{InputTokens: 3, OutputTokens: 7},
			found: true,
		},
		{
			name:  "no token info returns false",
			line:  `data: {"type":"ping"}`,
			want:  StreamTokenCounts{},
			found: false,
		},
		{
			name:  "content chunk no usage returns false",
			line:  `data: {"type":"content_block_delta","delta":{"text":"hello"}}`,
			want:  StreamTokenCounts{},
			found: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, found := ExtractTokensFromSSEChunk([]byte(tc.line))
			if found != tc.found {
				t.Fatalf("got found=%v, want %v", found, tc.found)
			}
			if got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestExtractCostInUsdTicksFromSSEChunk(t *testing.T) {
	cases := []struct {
		name  string
		line  string
		want  float64
		found bool
	}{
		{
			name:  "non-data line returns false",
			line:  "event: message",
			want:  0,
			found: false,
		},
		{
			name:  "empty data line returns false",
			line:  "data:",
			want:  0,
			found: false,
		},
		{
			name:  "done marker returns false",
			line:  "data: [DONE]",
			want:  0,
			found: false,
		},
		{
			name:  "response.usage.cost_in_usd_ticks",
			line:  `data: {"type":"response.completed","response":{"usage":{"cost_in_usd_ticks":25000000000}}}`,
			want:  2.5,
			found: true,
		},
		{
			name:  "top-level usage.cost_in_usd_ticks",
			line:  `data: {"done":true,"usage":{"cost_in_usd_ticks":10000000000}}`,
			want:  1.0,
			found: true,
		},
		{
			name:  "content chunk no cost returns false",
			line:  `data: {"type":"content_block_delta","delta":{"text":"hello"}}`,
			want:  0,
			found: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, found := ExtractCostInUsdTicksFromSSEChunk([]byte(tc.line))
			if found != tc.found {
				t.Fatalf("got found=%v, want %v", found, tc.found)
			}
			if got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMergeTokenCounts(t *testing.T) {
	cases := []struct {
		name string
		dst  StreamTokenCounts
		src  StreamTokenCounts
		want StreamTokenCounts
	}{
		{
			name: "merge all non-zero fields",
			dst:  StreamTokenCounts{},
			src:  StreamTokenCounts{InputTokens: 10, OutputTokens: 20, ReasoningTokens: 5, CachedTokens: 3, CacheCreationTokens: 1, CostUsd: 1.25},
			want: StreamTokenCounts{InputTokens: 10, OutputTokens: 20, ReasoningTokens: 5, CachedTokens: 3, CacheCreationTokens: 1, CostUsd: 1.25},
		},
		{
			name: "non-zero src overwrites dst",
			dst:  StreamTokenCounts{InputTokens: 1, OutputTokens: 2},
			src:  StreamTokenCounts{InputTokens: 10, OutputTokens: 20},
			want: StreamTokenCounts{InputTokens: 10, OutputTokens: 20},
		},
		{
			name: "zero src fields do not overwrite dst",
			dst:  StreamTokenCounts{InputTokens: 10},
			src:  StreamTokenCounts{InputTokens: 0, OutputTokens: 0},
			want: StreamTokenCounts{InputTokens: 10},
		},
		{
			name: "merge partial fields preserves dst",
			dst:  StreamTokenCounts{InputTokens: 10},
			src:  StreamTokenCounts{OutputTokens: 20},
			want: StreamTokenCounts{InputTokens: 10, OutputTokens: 20},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			MergeTokenCounts(&tc.dst, &tc.src)
			if tc.dst != tc.want {
				t.Fatalf("got %+v, want %+v", tc.dst, tc.want)
			}
		})
	}
}
