package executor

import (
	"encoding/json"
	"testing"
)

func TestKiroExtractThinkingDisplay(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"included", `{"_thinkingDisplay":"included"}`, "included"},
		{"summarized", `{"_thinkingDisplay":"summarized"}`, "summarized"},
		{"stripped", `{"_thinkingDisplay":"stripped"}`, "stripped"},
		{"unknown falls back", `{"_thinkingDisplay":"verbose"}`, ""},
		{"missing", `{}`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := extractThinkingDisplay([]byte(c.body))
			if got != c.want {
				t.Errorf("extractThinkingDisplay(%q) = %q, want %q", c.body, got, c.want)
			}
		})
	}
}

func collectStrippedChunks(s *kiroStreamState, pieces []string, model string) []map[string]any {
	var out []map[string]any
	for _, p := range pieces {
		for _, c := range s.splitInlineThinkingStripped(p, model) {
			var chunk map[string]any
			_ = json.Unmarshal(c[6:], &chunk)
			out = append(out, chunk)
		}
	}
	for _, c := range s.flushPendingThinking(model) {
		var chunk map[string]any
		_ = json.Unmarshal(c[6:], &chunk)
		out = append(out, chunk)
	}
	return out
}

func TestKiroInlineThinkingStripped(t *testing.T) {
	s := &kiroStreamState{thinkingExpected: true, thinkingDisplay: "stripped"}
	chunks := collectStrippedChunks(s, []string{"hello <thinking>deep reason</thinking> world"}, "kiro")
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	delta := firstDelta(t, chunks[0])
	if delta["content"] != "hello  world" {
		t.Errorf("content = %q, want %q", delta["content"], "hello  world")
	}
	if _, hasReasoning := delta["reasoning_content"]; hasReasoning {
		t.Errorf("reasoning_content should not be emitted when stripped")
	}
}

func TestKiroReasoningEventStripped(t *testing.T) {
	s := &kiroStreamState{thinkingExpected: true, thinkingDisplay: "stripped"}
	frame := &EventFrame{
		Headers: map[string]string{":event-type": "reasoningContentEvent"},
		Payload: []byte(`{"reasoningContentEvent":{"reasoningText":{"text":"secret thought"}}}`),
	}
	out := s.handleEvent(frame, nil, "kiro")
	if len(out) != 0 {
		t.Fatalf("expected reasoning event to be dropped when stripped, got %d chunks", len(out))
	}
}

func TestKiroInlineThinkingIncluded(t *testing.T) {
	s := &kiroStreamState{thinkingExpected: true, thinkingDisplay: "included"}
	chunks := collectThinkingChunks(s, []string{"hello <thinking>deep reason</thinking> world"}, "kiro")
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}
	want := []string{"hello ", "deep reason", " world"}
	modes := []string{"content", "reasoning_content", "content"}
	for i, mode := range modes {
		delta := firstDelta(t, chunks[i])
		if delta[mode] != want[i] {
			t.Errorf("chunk %d %s = %q, want %q", i, mode, delta[mode], want[i])
		}
	}
}
