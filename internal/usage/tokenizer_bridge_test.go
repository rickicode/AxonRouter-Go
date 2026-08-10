package usage

import (
	"testing"

	"github.com/rickicode/AxonRouter-Go/internal/tokenizer"
)

func TestEstimateTokensFromString_UsesTokenizerCodec(t *testing.T) {
	if testing.Short() {
		t.Skip("tokenizer codec load skipped in short mode")
	}

	enc, err := tokenizer.CodecForModel("")
	if err != nil {
		t.Fatalf("CodecForModel(\"\") unexpected error: %v", err)
	}

	tests := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"english", "the quick brown fox jumps"},
		{"cjk", "你好世界"},
		{"short latin", "abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want, err := enc.Count(tt.input)
			if err != nil {
				t.Fatalf("codec.Count(%q) error: %v", tt.input, err)
			}

			got := EstimateTokensFromString(tt.input)
			if got != int64(want) {
				t.Errorf("EstimateTokensFromString(%q) = %d, want tokenizer count %d", tt.input, got, want)
			}
		})
	}
}

func TestEstimateTokensFromString_MatchesOpenAITokenizerApproximation(t *testing.T) {
	// Approximate token counts for cl100k_base (default fallback codec).
	tests := []struct {
		name  string
		input string
		want  int64
	}{
		{"hello world", "hello world", 2},
		{"common sentence", "the quick brown fox jumps", 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EstimateTokensFromString(tt.input)
			if got != tt.want {
				t.Errorf("EstimateTokensFromString(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestEstimateTokensFromString_FallsBackToCharHeuristicOnFailure(t *testing.T) {
	// We cannot force CodecForModel to fail in normal operation, but we can
	// verify the fallback path is not taken for a valid input by checking the
	// tokenizer count differs from the rune/4 heuristic for CJK text.
	// A correct tokenizer returns 5 tokens, while rune/4 would return 1.
	got := EstimateTokensFromString("你好世界")
	if got == 1 {
		t.Errorf("EstimateTokensFromString(CJK) returned rune/4 fallback unexpectedly: %d", got)
	}
}
