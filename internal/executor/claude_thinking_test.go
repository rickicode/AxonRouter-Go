package executor

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestDisableThinkingIfToolChoiceForced(t *testing.T) {
	cases := []struct {
		name           string
		input          string
		wantThinking   bool
		wantOutputConf bool
		wantEffort     bool
	}{
		{
			name:           "removes thinking for tool_choice any",
			input:          `{"model":"claude-opus-4","thinking":{"type":"enabled","budget_tokens":4096},"tool_choice":{"type":"any","name":"foo"}}`,
			wantThinking:   false,
			wantOutputConf: false,
		},
		{
			name:           "removes thinking for tool_choice tool",
			input:          `{"model":"claude-opus-4","thinking":{"type":"enabled","budget_tokens":4096},"tool_choice":{"type":"tool","name":"foo"},"output_config":{"effort":"high"}}`,
			wantThinking:   false,
			wantOutputConf: false,
			wantEffort:     false,
		},
		{
			name:           "keeps output_config siblings when effort is removed",
			input:          `{"thinking":{"type":"enabled"},"tool_choice":{"type":"tool"},"output_config":{"effort":"high","other":"x"}}`,
			wantThinking:   false,
			wantOutputConf: true,
			wantEffort:     false,
		},
		{
			name:           "preserves thinking without forced tool_choice",
			input:          `{"thinking":{"type":"enabled","budget_tokens":4096},"tool_choice":{"type":"auto"}}`,
			wantThinking:   true,
			wantOutputConf: false,
		},
		{
			name:           "preserves thinking when no tool_choice",
			input:          `{"thinking":{"type":"enabled","budget_tokens":4096}}`,
			wantThinking:   true,
			wantOutputConf: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := disableThinkingIfToolChoiceForced([]byte(tc.input))
			if got := gjson.GetBytes(out, "thinking").Exists(); got != tc.wantThinking {
				t.Errorf("thinking exists = %v, want %v", got, tc.wantThinking)
			}
			if got := gjson.GetBytes(out, "output_config").Exists(); got != tc.wantOutputConf {
				t.Errorf("output_config exists = %v, want %v", got, tc.wantOutputConf)
			}
			if tc.wantOutputConf {
				if got := gjson.GetBytes(out, "output_config.effort").Exists(); got != tc.wantEffort {
					t.Errorf("output_config.effort exists = %v, want %v", got, tc.wantEffort)
				}
			}
		})
	}
}

func TestNormalizeClaudeSamplingForUpstream(t *testing.T) {
	cases := []struct {
		name         string
		input        string
		wantTemp     bool
		wantTopP     bool
		wantTopK     bool
	}{
		{
			name:     "removes temperature and top_p when thinking is enabled",
			input:    `{"thinking":{"type":"enabled"},"temperature":0.5,"top_p":0.9,"top_k":40}`,
			wantTemp: false,
			wantTopP: false,
			wantTopK: false,
		},
		{
			name:     "removes sampling params when thinking is adaptive",
			input:    `{"thinking":{"type":"adaptive"},"temperature":1,"top_p":1}`,
			wantTemp: false,
			wantTopP: false,
			wantTopK: false,
		},
		{
			name:     "removes temperature and top_p even without thinking",
			input:    `{"temperature":0.5,"top_p":0.9}`,
			wantTemp: false,
			wantTopP: false,
			wantTopK: false,
		},
		{
			name:     "does not reject other fields",
			input:    `{"thinking":{"type":"disabled"},"temperature":0.5}`,
			wantTemp: false,
			wantTopP: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := normalizeClaudeSamplingForUpstream([]byte(tc.input))
			if got := gjson.GetBytes(out, "temperature").Exists(); got != tc.wantTemp {
				t.Errorf("temperature exists = %v, want %v", got, tc.wantTemp)
			}
			if got := gjson.GetBytes(out, "top_p").Exists(); got != tc.wantTopP {
				t.Errorf("top_p exists = %v, want %v", got, tc.wantTopP)
			}
			if got := gjson.GetBytes(out, "top_k").Exists(); got != tc.wantTopK {
				t.Errorf("top_k exists = %v, want %v", got, tc.wantTopK)
			}
		})
	}
}

func TestEnsureClaudeThinkingDisplay(t *testing.T) {
	cases := []struct {
		name        string
		input       string
		wantDisplay string
	}{
		{
			name:        "defaults enabled thinking to summarized",
			input:       `{"thinking":{"type":"enabled","budget_tokens":4096}}`,
			wantDisplay: "summarized",
		},
		{
			name:        "defaults adaptive thinking to summarized",
			input:       `{"thinking":{"type":"adaptive"}}`,
			wantDisplay: "summarized",
		},
		{
			name:        "defaults auto thinking to summarized",
			input:       `{"thinking":{"type":"auto"}}`,
			wantDisplay: "summarized",
		},
		{
			name:        "preserves explicit omitted value",
			input:       `{"thinking":{"type":"enabled","display":"omitted"}}`,
			wantDisplay: "omitted",
		},
		{
			name:        "sets display when empty string",
			input:       `{"thinking":{"type":"enabled","display":""}}`,
			wantDisplay: "summarized",
		},
		{
			name:        "leaves non-thinking body unchanged",
			input:       `{"temperature":0.5}`,
			wantDisplay: "",
		},
		{
			name:        "leaves disabled thinking unchanged",
			input:       `{"thinking":{"type":"disabled"}}`,
			wantDisplay: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := ensureClaudeThinkingDisplay([]byte(tc.input))
			got := gjson.GetBytes(out, "thinking.display").String()
			if got != tc.wantDisplay {
				t.Errorf("thinking.display = %q, want %q", got, tc.wantDisplay)
			}
		})
	}
}

func TestPrepareClaudeBody_ThinkingManagement(t *testing.T) {
	input := []byte(`{"model":"claude-opus-4","thinking":{"type":"enabled","budget_tokens":4096},"tool_choice":{"type":"any","name":"foo"},"temperature":0.5,"top_p":0.9,"betas":"beta1"}`)

	out, betas := prepareClaudeBody(input)

	if got := gjson.GetBytes(out, "thinking").Exists(); got {
		t.Errorf("thinking should be removed when tool_choice forces a tool")
	}
	if got := gjson.GetBytes(out, "temperature").Exists(); got {
		t.Errorf("temperature should be removed")
	}
	if got := gjson.GetBytes(out, "top_p").Exists(); got {
		t.Errorf("top_p should be removed")
	}
	if got := gjson.GetBytes(out, "betas").Exists(); got {
		t.Errorf("betas should be extracted and removed from body")
	}
	if len(betas) != 1 || betas[0] != "beta1" {
		t.Errorf("betas = %v, want [beta1]", betas)
	}
}
