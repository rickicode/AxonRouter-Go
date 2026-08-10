package combo

import "testing"

func TestSplitProviderModel_EdgeCases(t *testing.T) {
	cases := []struct {
		input        string
		wantProvider string
		wantModel    string
		wantOk       bool
	}{
		{"openai/gpt-4o", "openai", "gpt-4o", true},
		{"@openai/gpt-4o", "openai", "gpt-4o", true},
		{"  @openai/gpt-4o  ", "openai", "gpt-4o", true},
		{"openai/", "", "", false},
		{"/gpt-4o", "", "", false},
		{"gpt-4o", "", "", false},
		{"", "", "", false},
		{"openai/gpt-4o/extra", "openai", "gpt-4o/extra", true},
		{"  cx/gpt-5.4  ", "cx", "gpt-5.4", true},
		{"@cx/gpt-5.4", "cx", "gpt-5.4", true},
	}

	for _, tc := range cases {
		gotProvider, gotModel, gotOk := SplitProviderModel(tc.input)
		if gotOk != tc.wantOk {
			t.Errorf("SplitProviderModel(%q) ok = %v, want %v", tc.input, gotOk, tc.wantOk)
			continue
		}
		if gotOk && (gotProvider != tc.wantProvider || gotModel != tc.wantModel) {
			t.Errorf("SplitProviderModel(%q) = (%q, %q), want (%q, %q)", tc.input, gotProvider, gotModel, tc.wantProvider, tc.wantModel)
		}
	}
}

func TestIsSmartCombo_CaseInsensitive(t *testing.T) {
	cases := []struct {
		input    string
		wantGoal SmartGoal
		wantOk   bool
	}{
		{"Auto", GoalAuto, true},
		{"  auto  ", GoalAuto, true},
		{"SMART/AUTO", GoalAuto, true},
		{"smart/auto", GoalAuto, true},
		{"Economy", GoalEconomy, true},
		{"BALANCED", GoalBalanced, true},
		{" Smart/Premium ", GoalPremium, true},
		{"unknown", "", false},
		{"", "", false},
	}

	for _, tc := range cases {
		gotGoal, gotOk := isSmartCombo(tc.input)
		if gotOk != tc.wantOk {
			t.Errorf("isSmartCombo(%q) ok = %v, want %v", tc.input, gotOk, tc.wantOk)
			continue
		}
		if gotOk && gotGoal != tc.wantGoal {
			t.Errorf("isSmartCombo(%q) goal = %v, want %v", tc.input, gotGoal, tc.wantGoal)
		}
	}
}
