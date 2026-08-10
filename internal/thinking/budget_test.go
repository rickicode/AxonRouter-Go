package thinking

import "testing"

func TestBudgetFromString(t *testing.T) {
	cases := []struct {
		raw    string
		want   int
		wantOK bool
	}{
		{"8192", 8192, true},
		{"high", 24576, true},
		{"none", 0, true},
		{"-1", 0, true},
		{"auto", -1, true},
		{"0", 0, true},
		{"minimal", 512, true},
		{"MAX", 128000, true},
		{"foo", 0, false},
		{"-5", 0, false},
	}
	for _, c := range cases {
		g, ok := BudgetFromString(c.raw)
		if g != c.want || ok != c.wantOK {
			t.Errorf("BudgetFromString(%q) = (%d, %v), want (%d, %v)", c.raw, g, ok, c.want, c.wantOK)
		}
	}
}

func TestClampBudget(t *testing.T) {
	cases := []struct {
		provider string
		budget   int
		want     int
	}{
		{"claude", 500, 1024},
		{"claude", 64000, 64000},
		{"claude", 200000, 128000},
		{"gemini", 40000, 24576},
		{"openai", 5000, 5000},
		{"", -1, -1},
		{"claude", 0, 0},
	}
	for _, c := range cases {
		g := ClampBudget(c.provider, c.budget)
		if g != c.want {
			t.Errorf("ClampBudget(%q, %d) = %d, want %d", c.provider, c.budget, g, c.want)
		}
	}
}
