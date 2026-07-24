package antigravity

import (
	"strings"
	"testing"
)

func TestObfuscate(t *testing.T) {
	words := []string{"cursor", "claude code", "opencode"}

	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "empty text",
			in:   "",
			want: "",
		},
		{
			name: "no match",
			in:   "hello world",
			want: "hello world",
		},
		{
			name: "single ASCII word",
			in:   "I use cursor daily",
			want: "I use c\u200dursor daily",
		},
		{
			name: "preserves case",
			in:   "Try Cursor IDE",
			want: "Try C\u200dursor IDE",
		},
		{
			name: "multi-word phrase",
			in:   "It runs claude code well",
			want: "It runs c\u200dlaude code well",
		},
		{
			name: "multiple words",
			in:   "cursor or opencode?",
			want: "c\u200dursor or o\u200dpencode?",
		},
		{
			name: "single rune unchanged",
			in:   "a",
			want: "a",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Obfuscate(c.in, words)
			if got != c.want {
				t.Errorf("Obfuscate(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestObfuscate_EmptyWords(t *testing.T) {
	got := Obfuscate("cursor", nil)
	if got != "cursor" {
		t.Errorf("expected no change when words empty, got %q", got)
	}
}

func TestObfuscate_WordsAreTrimmed(t *testing.T) {
	got := Obfuscate("cursor", []string{" cursor "})
	if !strings.Contains(got, "\u200d") {
		t.Errorf("expected trimmed word to still match, got %q", got)
	}
}

func TestObfuscate_CaseInsensitive(t *testing.T) {
	got := Obfuscate("CURSOR", []string{"cursor"})
	want := "C\u200dURSOR"
	if got != want {
		t.Errorf("Obfuscate(%q) = %q, want %q", "CURSOR", got, want)
	}
}
