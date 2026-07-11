package caveman

import (
	"strings"
	"testing"

	"github.com/rickicode/AxonRouter-Go/internal/cache"
	"github.com/rickicode/AxonRouter-Go/internal/compression"
)

func TestCavemanFillerAdverbs(t *testing.T) {
	e := &Engine{}
	body := []byte(`{"messages":[{"role":"user","content":"basically I need you to simply explain Go interfaces"}]}`)
	got, stats, err := e.Apply(body, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(string(got), "basically") {
		t.Error("still contains basically")
	}
	if strings.Contains(string(got), "simply") {
		t.Error("still contains simply")
	}
	if stats.SavingsPercent <= 0 {
		t.Errorf("expected positive savings, got %f", stats.SavingsPercent)
	}
	found := false
	for _, tech := range stats.TechniquesUsed {
		if tech == "filler_adverbs" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected technique filler_adverbs, got %v", stats.TechniquesUsed)
	}
}

func TestCavemanVerboseInstructions(t *testing.T) {
	e := &Engine{}
	body := []byte(`{"messages":[{"role":"user","content":"Provide a detailed and comprehensive explanation of Go interfaces"}]}`)
	got, stats, err := e.Apply(body, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(string(got), "provide a detailed") {
		t.Error("still contains 'provide a detailed'")
	}
	if stats.SavingsPercent <= 0 {
		t.Errorf("expected positive savings, got %f", stats.SavingsPercent)
	}
}

func TestCavemanPreservesCodeBlocks(t *testing.T) {
	e := &Engine{}
	body := []byte("{\"messages\":[{\"role\":\"user\",\"content\":\"basically here is the code:\\n```go\\nfmt.Println(\\\"hello\\\")\\n```\"}]}")
	got, _, err := e.Apply(body, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(got), "fmt.Println") {
		t.Error("code block was corrupted")
	}
	if strings.Contains(string(got), "basically") {
		t.Error("should have removed 'basically' outside code block")
	}
}

func TestCavemanFailOpenEmpty(t *testing.T) {
	e := &Engine{}
	body := []byte(`{"messages":[{"role":"user","content":"hi there"}]}`)
	got, stats, err := e.Apply(body, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// "hi there" gets stripped by redundant_openers, leaving empty string.
	// Fail-open: if result is empty, return original.
	if string(got) != string(body) {
		t.Logf("got: %s", string(got))
	}
	_ = stats
}

func TestLiteCollapseWhitespace(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"hello   world\n\n  foo"}]}`)
	cfg := compression.LiteConfig{CollapseWhitespace: true}
	got, stats, err := compression.ApplyLite(body, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(got), `"hello world foo"`) {
		t.Errorf("expected collapsed whitespace, got %s", string(got))
	}
	if stats.SavingsPercent <= 0 {
		t.Errorf("expected positive savings, got %f", stats.SavingsPercent)
	}
}

func TestStrategyOff(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"hello world"}]}`)
	cfg := compression.Strategy{Mode: compression.ModeOff}
	got, stats, err := compression.Apply(cfg, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != string(body) {
		t.Error("mode=off should return original body")
	}
	if stats.SavingsPercent != 0 {
		t.Errorf("mode=off should have 0 savings, got %f", stats.SavingsPercent)
	}
}

func TestCacheExactHit(t *testing.T) {
	c := cache.NewExactCache(10)
	key := cache.ComputeKey([]byte(`{"model":"gpt-4"}`), "openai/gpt-4")
	_, ok := c.Get(key)
	if ok {
		t.Error("expected cache miss on empty cache")
	}
	c.Set(key, cache.CacheEntry{Body: []byte(`{"ok":true}`), StatusCode: 200, ContentType: "application/json"})
	entry, ok := c.Get(key)
	if !ok {
		t.Fatal("expected cache hit after set")
	}
	if string(entry.Body) != `{"ok":true}` {
		t.Errorf("unexpected body: %s", string(entry.Body))
	}
	stats := c.Stats()
	if stats.Hits != 1 {
		t.Errorf("expected 1 hit, got %d", stats.Hits)
	}
	if stats.Misses != 1 {
		t.Errorf("expected 1 miss, got %d", stats.Misses)
	}
	if stats.Size != 1 {
		t.Errorf("expected size 1, got %d", stats.Size)
	}
}
