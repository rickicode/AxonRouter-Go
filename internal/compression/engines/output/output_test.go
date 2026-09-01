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

// levelCase is a table-driven case: applying the level must yield a system
// message whose content contains want and whose technique is output_<level>.
type levelCase struct {
	level    string
	want     string
	notWant  string
}

func runLevelCase(t *testing.T, tc levelCase) {
	t.Helper()
	body := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"Hi"}]}`)
	e := &Engine{}
	out, stats, err := e.Apply(body, compression.EngineConfig{"level": tc.level})
	if err != nil {
		t.Fatalf("level %q: unexpected error: %v", tc.level, err)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("level %q: invalid output JSON: %v", tc.level, err)
	}
	messages, _ := m["messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("level %q: expected 2 messages, got %d", tc.level, len(messages))
	}
	sys, _ := messages[0].(map[string]any)
	if sys["role"] != "system" {
		t.Fatalf("level %q: expected system message, got %v", tc.level, sys["role"])
	}
	content, _ := sys["content"].(string)
	if !strings.Contains(content, tc.want) {
		t.Fatalf("level %q: content missing %q:\n%s", tc.level, tc.want, content)
	}
	if tc.notWant != "" && strings.Contains(content, tc.notWant) {
		t.Fatalf("level %q: content should NOT contain %q:\n%s", tc.level, tc.notWant, content)
	}
	wantTech := "output_" + tc.level
	if len(stats.TechniquesUsed) != 1 || stats.TechniquesUsed[0] != wantTech {
		t.Fatalf("level %q: expected technique %q, got %v", tc.level, wantTech, stats.TechniquesUsed)
	}
}

func TestEngine_CavemanLevels(t *testing.T) {
	cases := []levelCase{
		{level: "caveman-lite", want: "Fewest words"},
		{level: "caveman-full", want: "Be extremely terse"},
		{level: "caveman-ultra", want: "ULTRA caveman", notWant: "Be extremely terse"},
	}
	for _, tc := range cases {
		runLevelCase(t, tc)
	}
}

func TestEngine_WenyanLevels(t *testing.T) {
	cases := []levelCase{
		{level: "wenyan-lite", want: "文言"},
		{level: "wenyan", want: "以文言文作答"},
		{level: "wenyan-ultra", want: "极简文言"},
	}
	for _, tc := range cases {
		runLevelCase(t, tc)
	}
}

func TestEngine_PonytailLevels(t *testing.T) {
	cases := []levelCase{
		{level: "ponytail-lite", want: "Minimal code"},
		{level: "ponytail-full", want: "YAGNI"},
		{level: "ponytail-ultra", want: "ULTRA lazy"},
	}
	for _, tc := range cases {
		runLevelCase(t, tc)
	}
}

// TestEngine_WenyanOverridesPreserveLanguage checks that the wenyan fragment
// replaces the preserve-language rule, not coexists with it.
func TestEngine_WenyanOverridesPreserveLanguage(t *testing.T) {
	body := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"Hi"}]}`)
	e := &Engine{}
	out, _, err := e.Apply(body, compression.EngineConfig{"level": "wenyan"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("invalid output JSON: %v", err)
	}
	messages, _ := m["messages"].([]any)
	sys, _ := messages[0].(map[string]any)
	content, _ := sys["content"].(string)
	if strings.Contains(content, "Preserve the user's dominant language") {
		t.Fatalf("wenyan must override preserve-language rule:\n%s", content)
	}
}

// TestEngine_PonytailLiteSkipsGuardrails checks the lite level omits the
// not-lazy guardrail fragment while full includes it.
func TestEngine_PonytailLiteSkipsGuardrails(t *testing.T) {
	body := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"Hi"}]}`)
	e := &Engine{}
	out, _, err := e.Apply(body, compression.EngineConfig{"level": "ponytail-lite"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("invalid output JSON: %v", err)
	}
	messages, _ := m["messages"].([]any)
	sys, _ := messages[0].(map[string]any)
	content, _ := sys["content"].(string)
	if strings.Contains(content, "Never simplify away") {
		t.Fatalf("ponytail-lite must skip guardrails:\n%s", content)
	}
}
