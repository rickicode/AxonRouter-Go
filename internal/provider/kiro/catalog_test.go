package kiro

import (
	"slices"
	"testing"
)

func TestBaseModels(t *testing.T) {
	want := []string{
		"claude-opus-4.8",
		"claude-opus-4.7",
		"claude-opus-4.5",
		"claude-sonnet-5",
		"claude-sonnet-4.6",
		"claude-sonnet-4.5",
		"claude-haiku-4.5",
		"deepseek-3.2",
		"minimax-m2.7",
		"minimax-m2.5",
		"minimax-m2.1",
		"glm-5",
		"qwen3-coder-next",
		"gpt-5.6-sol",
		"gpt-5.6-terra",
		"gpt-5.6-luna",
	}
	got := make([]string, 0, len(BaseModels))
	for _, m := range BaseModels {
		if m.ID == "" || m.DisplayName == "" {
			t.Fatalf("base model missing id or display_name: %+v", m)
		}
		got = append(got, m.ID)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("base models = %v, want %v", got, want)
	}
}

func TestBaseModelContextAndOutput(t *testing.T) {
	for _, m := range BaseModels {
		if m.ContextLength <= 0 {
			t.Errorf("%s: context_length must be > 0, got %d", m.ID, m.ContextLength)
		}
		if m.MaxOutputTokens <= 0 {
			t.Errorf("%s: max_output_tokens must be > 0, got %d", m.ID, m.MaxOutputTokens)
		}
	}
}

func TestExpandVariants(t *testing.T) {
	models := ExpandVariants(BaseModels)

	if len(models) != len(BaseModels)*4 {
		t.Fatalf("expected %d models, got %d", len(BaseModels)*4, len(models))
	}

	seen := make(map[string]bool)
	for _, m := range models {
		if seen[m.ID] {
			t.Fatalf("duplicate model id %q", m.ID)
		}
		seen[m.ID] = true
	}

	cases := map[string]Capabilities{
		"claude-sonnet-5":                         {Thinking: false, Agentic: false},
		"claude-sonnet-5-thinking":                {Thinking: true, Agentic: false},
		"claude-sonnet-5-agentic":                 {Thinking: false, Agentic: true},
		"claude-sonnet-5-thinking-agentic":         {Thinking: true, Agentic: true},
		"deepseek-3.2-thinking":                   {Thinking: true, Agentic: false},
		"qwen3-coder-next-agentic":                {Thinking: false, Agentic: true},
		"minimax-m2.5-thinking-agentic":           {Thinking: true, Agentic: true},
	}
	for id, want := range cases {
		m, ok := findModel(models, id)
		if !ok {
			t.Fatalf("missing model %q", id)
		}
		if m.Capabilities != want {
			t.Errorf("%s capabilities = %+v, want %+v", id, m.Capabilities, want)
		}
		if m.UpstreamModelID == "" {
			t.Errorf("%s: upstream_model_id must be set", id)
		}
		if m.ContextLength <= 0 || m.MaxOutputTokens <= 0 {
			t.Errorf("%s: context/output must be > 0", id)
		}
	}
}

func TestStripSyntheticSuffix(t *testing.T) {
	cases := map[string]string{
		"claude-sonnet-5-thinking":            "claude-sonnet-5",
		"claude-sonnet-5-agentic":             "claude-sonnet-5",
		"claude-sonnet-5-thinking-agentic":    "claude-sonnet-5",
		"deepseek-3.2":                        "deepseek-3.2",
	}
	for in, want := range cases {
		if got := StripSyntheticSuffix(in); got != want {
			t.Errorf("StripSyntheticSuffix(%q) = %q, want %q", in, got, want)
		}
	}
}

func findModel(models []Model, id string) (Model, bool) {
	for _, m := range models {
		if m.ID == id {
			return m, true
		}
	}
	return Model{}, false
}
