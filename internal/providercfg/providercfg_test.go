package providercfg

import "testing"

func TestNextRoundRobinIndexPerModelCursor(t *testing.T) {
	m := NewManager(t.TempDir())

	provider := "openai"
	modelA := "gpt-4o"
	modelB := "gpt-3.5-turbo"
	total := 3

	if got := m.NextRoundRobinIndex(provider, modelA, total); got != 0 {
		t.Fatalf("first index for modelA = %d, want 0", got)
	}

	// modelB must start from its own cursor, not inherit modelA's counter.
	if got := m.NextRoundRobinIndex(provider, modelB, total); got != 0 {
		t.Fatalf("first index for modelB = %d, want 0 (cursor should be independent)", got)
	}

	wantA := []int{1, 2, 0, 1}
	for _, want := range wantA {
		if got := m.NextRoundRobinIndex(provider, modelA, total); got != want {
			t.Fatalf("modelA index = %d, want %d", got, want)
		}
	}

	// modelB should have advanced only once so far.
	if got := m.NextRoundRobinIndex(provider, modelB, total); got != 1 {
		t.Fatalf("modelB index after skip = %d, want 1", got)
	}
}

func TestNextRoundRobinIndexIndependentPerProvider(t *testing.T) {
	m := NewManager(t.TempDir())

	model := "gpt-4o"
	total := 2

	m.NextRoundRobinIndex("openai", model, total)
	if got := m.NextRoundRobinIndex("openai", model, total); got != 1 {
		t.Fatalf("openai cursor = %d, want 1", got)
	}

	// A different provider with the same model name should start fresh.
	if got := m.NextRoundRobinIndex("cx", model, total); got != 0 {
		t.Fatalf("cx cursor = %d, want 0", got)
	}
}
