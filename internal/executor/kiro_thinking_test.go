package executor

import (
	"encoding/json"
	"testing"
)

func collectThinkingChunks(s *kiroStreamState, pieces []string, model string) []map[string]any {
	var out []map[string]any
	for _, p := range pieces {
		for _, c := range s.splitInlineThinking(p, model) {
			var chunk map[string]any
			_ = json.Unmarshal(c[6:], &chunk) // strip "data: " prefix
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

func TestKiroInlineThinking_CompleteInOneChunk(t *testing.T) {
	s := &kiroStreamState{thinkingExpected: true}
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

func TestKiroInlineThinking_SplitTagAcrossChunks(t *testing.T) {
	s := &kiroStreamState{thinkingExpected: true}
	chunks := collectThinkingChunks(s, []string{"text<thin", "king>reason</think", "ing>more"}, "kiro")
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}
	want := []string{"text", "reason", "more"}
	modes := []string{"content", "reasoning_content", "content"}
	for i, mode := range modes {
		delta := firstDelta(t, chunks[i])
		if delta[mode] != want[i] {
			t.Errorf("chunk %d %s = %q, want %q", i, mode, delta[mode], want[i])
		}
	}
}

func TestKiroInlineThinking_NoTag(t *testing.T) {
	s := &kiroStreamState{thinkingExpected: true}
	chunks := collectThinkingChunks(s, []string{"just content"}, "kiro")
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	delta := firstDelta(t, chunks[0])
	if delta["content"] != "just content" {
		t.Errorf("content = %q", delta["content"])
	}
}

func firstDelta(t *testing.T, chunk map[string]any) map[string]any {
	t.Helper()
	choices, _ := chunk["choices"].([]any)
	if len(choices) == 0 {
		t.Fatal("no choices")
	}
	choice, _ := choices[0].(map[string]any)
	delta, _ := choice["delta"].(map[string]any)
	return delta
}
