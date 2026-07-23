package thinking

import "testing"

func TestParseThinkingSuffix(t *testing.T) {
	cases := []struct {
		model    string
		wantBase string
		wantRaw  string
		wantOK   bool
	}{
		{"claude-sonnet-4-5(8192)", "claude-sonnet-4-5", "8192", true},
		{"gpt-4o(high)", "gpt-4o", "high", true},
		{"claude/claude-sonnet-4-5(none)", "claude/claude-sonnet-4-5", "none", true},
		{"openai/gpt-4o(-1)", "openai/gpt-4o", "-1", true},
		{"gemini-2.5-pro", "gemini-2.5-pro", "", false},
		{"model(1)(2)", "model(1)", "2", true},
		{"model(foo", "model(foo", "", false},
		{"model)", "model)", "", false},
		{"model()", "model()", "", false},
	}
	for _, c := range cases {
		base, raw, ok := ParseThinkingSuffix(c.model)
		if base != c.wantBase || raw != c.wantRaw || ok != c.wantOK {
			t.Errorf("ParseThinkingSuffix(%q) = (%q, %q, %v), want (%q, %q, %v)",
				c.model, base, raw, ok, c.wantBase, c.wantRaw, c.wantOK)
		}
	}
}
