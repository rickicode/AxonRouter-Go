package output

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rickicode/AxonRouter-Go/internal/compression"
)

func TestEngine_AppendExistingSystemMessage(t *testing.T) {
	body := []byte(`{"model":"gpt-4o","messages":[{"role":"system","content":"You are helpful."},{"role":"user","content":"Hi"}]}`)

	e := &Engine{}
	out, stats, err := e.Apply(body, compression.EngineConfig{"level": "caveman"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("invalid output JSON: %v", err)
	}
	messages, _ := m["messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
	sys, _ := messages[0].(map[string]any)
	content, _ := sys["content"].(string)
	if !strings.Contains(content, "You are helpful.") || !strings.Contains(content, "Be extremely terse") {
		t.Fatalf("system prompt not appended correctly: %s", content)
	}
	if len(stats.TechniquesUsed) != 1 || stats.TechniquesUsed[0] != "output_caveman" {
		t.Fatalf("unexpected techniques: %v", stats.TechniquesUsed)
	}
}

func TestEngine_PrependsNewSystemMessage(t *testing.T) {
	body := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"Hi"}]}`)

	e := &Engine{}
	out, stats, err := e.Apply(body, compression.EngineConfig{"level": "ponytail"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("invalid output JSON: %v", err)
	}
	messages, _ := m["messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
	sys, _ := messages[0].(map[string]any)
	if sys["role"] != "system" {
		t.Fatalf("expected system message, got %v", sys["role"])
	}
	content, _ := sys["content"].(string)
	if !strings.Contains(content, "YAGNI") {
		t.Fatalf("ponytail prompt missing: %s", content)
	}
	if stats.TechniquesUsed[0] != "output_ponytail" {
		t.Fatalf("unexpected techniques: %v", stats.TechniquesUsed)
	}
}

func TestEngine_FailOpenOnInvalidJSON(t *testing.T) {
	body := []byte(`not json`)
	e := &Engine{}
	out, _, err := e.Apply(body, compression.EngineConfig{"level": "caveman"})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if string(out) != string(body) {
		t.Fatalf("expected original body, got %s", string(out))
	}
}

func TestEngine_FailOpenOnNoMessages(t *testing.T) {
	body := []byte(`{"model":"gpt-4o"}`)
	e := &Engine{}
	out, _, _ := e.Apply(body, compression.EngineConfig{"level": "caveman"})
	if string(out) != string(body) {
		t.Fatalf("expected original body, got %s", string(out))
	}
}

func TestEngine_InvalidLevelReturnsOriginal(t *testing.T) {
	body := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"Hi"}]}`)
	e := &Engine{}
	out, _, _ := e.Apply(body, compression.EngineConfig{"level": "unknown"})
	if string(out) != string(body) {
		t.Fatalf("expected original body for unknown level, got %s", string(out))
	}
}
