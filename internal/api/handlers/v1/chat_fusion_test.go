package v1

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildFusionJudgeBody_PreservesConversationHistory(t *testing.T) {
	originalReq := []byte(`{
		"model": "combo-fusion",
		"temperature": 0.7,
		"messages": [
			{"role": "system", "content": "You are a helpful assistant."},
			{"role": "user", "content": "What is the capital of France?"},
			{"role": "assistant", "content": "Paris."},
			{"role": "user", "content": "And Germany?"}
		]
	}`)

	panels := []fusionPanel{
		{modelID: "openai/gpt-4o", content: "Berlin is the capital of Germany."},
		{modelID: "claude/claude-sonnet-4", content: "The capital of Germany is Berlin."},
	}

	got := buildFusionJudgeBody(originalReq, panels, false)

	var out map[string]any
	if err := json.Unmarshal(got, &out); err != nil {
		t.Fatalf("judge body is not valid JSON: %v", err)
	}

	if out["model"] != "combo-fusion" {
		t.Errorf("model field changed: got %v, want combo-fusion", out["model"])
	}
	if out["temperature"] != 0.7 {
		t.Errorf("temperature field changed: got %v, want 0.7", out["temperature"])
	}

	messages, ok := out["messages"].([]any)
	if !ok {
		t.Fatalf("messages is not an array")
	}
	if len(messages) != 5 {
		t.Fatalf("expected 5 messages (4 original + 1 judge turn), got %d", len(messages))
	}

	// Verify original messages are preserved in order.
	expectOriginal := [4]map[string]string{
		{"role": "system", "content": "You are a helpful assistant."},
		{"role": "user", "content": "What is the capital of France?"},
		{"role": "assistant", "content": "Paris."},
		{"role": "user", "content": "And Germany?"},
	}
	for i, want := range expectOriginal {
		msg, ok := messages[i].(map[string]any)
		if !ok {
			t.Fatalf("message %d is not an object", i)
		}
		if msg["role"] != want["role"] {
			t.Errorf("message %d role: got %v, want %v", i, msg["role"], want["role"])
		}
		if msg["content"] != want["content"] {
			t.Errorf("message %d content: got %v, want %v", i, msg["content"], want["content"])
		}
	}

	// Verify judge directive is appended as a new user turn.
	judgeMsg, ok := messages[4].(map[string]any)
	if !ok {
		t.Fatalf("judge message is not an object")
	}
	if judgeMsg["role"] != "user" {
		t.Errorf("judge turn role: got %v, want user", judgeMsg["role"])
	}

	judgeContent, ok := judgeMsg["content"].(string)
	if !ok {
		t.Fatalf("judge content is not a string")
	}
	for _, want := range []string{
		"You are a synthesis assistant",
		"User question: And Germany?",
		"Source openai/gpt-4o",
		"Source claude/claude-sonnet-4",
		"Berlin is the capital of Germany.",
		"Synthesize the best answer.",
	} {
		if !strings.Contains(judgeContent, want) {
			t.Errorf("judge content missing %q\ncontent:\n%s", want, judgeContent)
		}
	}
}

func TestBuildFusionJudgeBody_AnonymizeSources(t *testing.T) {
	originalReq := []byte(`{"model":"combo","messages":[{"role":"user","content":"hello"}]}`)
	panels := []fusionPanel{
		{modelID: "openai/gpt-4o", content: "hi there"},
		{modelID: "claude/claude-sonnet-4", content: "greetings"},
	}

	got := buildFusionJudgeBody(originalReq, panels, true)

	var out map[string]any
	if err := json.Unmarshal(got, &out); err != nil {
		t.Fatalf("judge body is not valid JSON: %v", err)
	}

	messages := out["messages"].([]any)
	judgeContent := messages[1].(map[string]any)["content"].(string)

	if strings.Contains(judgeContent, "openai/gpt-4o") || strings.Contains(judgeContent, "claude/claude-sonnet-4") {
		t.Errorf("judge content should not contain model IDs when anonymized:\n%s", judgeContent)
	}
	if !strings.Contains(judgeContent, "Source 1") || !strings.Contains(judgeContent, "Source 2") {
		t.Errorf("judge content should use anonymous labels:\n%s", judgeContent)
	}
}


