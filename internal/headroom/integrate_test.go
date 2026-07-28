package headroom

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestApplyToRequestBodyToolResult(t *testing.T) {
	s := NewServer()
	endpoint, err := s.Start()
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Stop(context.Background())

	client, _ := NewClient(Config{Enabled: true, Endpoint: endpoint, TimeoutMs: 5000})

	body := `{"model":"claude","messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_1","content":"ok  github.com/rickicode/AxonRouter-Go/internal/headroom  0.006s\n\n\n"}]}]}`
	out := ApplyToRequestBody(context.Background(), client, []byte(body))

	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	msgs, _ := m["messages"].([]any)
	if len(msgs) == 0 {
		t.Fatal("no messages")
	}
	msg, _ := msgs[0].(map[string]any)
	content, _ := msg["content"].([]any)
	if len(content) == 0 {
		t.Fatal("no content")
	}
	part, _ := content[0].(map[string]any)
	text := toolResultTextMap(part)
	if text == "" {
		t.Fatal("tool_result text empty")
	}
	if strings.Contains(text, "\n\n") {
		t.Errorf("expected empty lines collapsed, got %q", text)
	}
}

func TestApplyToRequestBodyDisabled(t *testing.T) {
	client, _ := NewClient(Config{Enabled: false})
	body := `{"model":"claude","messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_1","content":"ok  test\n\n\n"}]}]}`
	out := ApplyToRequestBody(context.Background(), client, []byte(body))
	if string(out) != body {
		t.Errorf("disabled client should return original body unchanged")
	}
}
