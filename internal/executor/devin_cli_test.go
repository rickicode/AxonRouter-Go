package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDevinCLIExecutor_ExecuteStream(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping shell-based test on windows")
	}

	bin := writeMockDevin(t, `#!/bin/sh
# Read the three JSON-RPC requests.
read -r _init
read -r _new
read -r _prompt
# Handshake
echo '{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"0.3"}}'
echo '{"jsonrpc":"2.0","id":2,"result":{"sessionId":"sess-123"}}'
echo '{"jsonrpc":"2.0","id":3,"result":{}}'
# Streaming deltas
echo '{"jsonrpc":"2.0","method":"session/update","params":{"type":"message_delta","content":"Hello "}}'
echo '{"jsonrpc":"2.0","method":"session/update","params":{"type":"message_delta","content":"Devin"}}'
echo '{"jsonrpc":"2.0","method":"session/update","params":{"type":"message_stop"}}'
`)

	t.Setenv("CLI_DEVIN_BIN", bin)

	exec := NewDevinCLIExecutor(NewBaseExecutor())
	req := &Request{
		Model:  "devin/swe-1.6-fast",
		Body:   []byte(`{"messages":[{"role":"user","content":"Say hi"}]}`),
		Stream: true,
		APIKey: "test-key",
	}

	result, err := exec.ExecuteStream(context.Background(), req)
	if err != nil {
		t.Fatalf("ExecuteStream failed: %v", err)
	}

	var deltas []string
	var done bool
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			t.Fatalf("stream error: %v", chunk.Err)
		}
		s := string(chunk.Payload)
		if strings.Contains(s, "[DONE]") {
			done = true
			continue
		}
		text := extractDeltaText(chunk.Payload)
		if text != "" {
			deltas = append(deltas, text)
		}
	}

	if !done {
		t.Fatal("expected [DONE] chunk")
	}
	got := strings.Join(deltas, "")
	if got != "Hello Devin" {
		t.Fatalf("expected 'Hello Devin', got %q", got)
	}
}

func TestDevinCLIExecutor_Execute(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping shell-based test on windows")
	}

	bin := writeMockDevin(t, `#!/bin/sh
read -r _init
read -r _new
read -r _prompt
echo '{"jsonrpc":"2.0","id":1,"result":{}}'
echo '{"jsonrpc":"2.0","id":2,"result":{"sessionId":"sess-abc"}}'
echo '{"jsonrpc":"2.0","id":3,"result":{"message":{"role":"assistant","content":"Final result"}}}'
`)

	t.Setenv("CLI_DEVIN_BIN", bin)

	exec := NewDevinCLIExecutor(NewBaseExecutor())
	req := &Request{
		Model:  "devin/swe-1.6-fast",
		Body:   []byte(`{"messages":[{"role":"user","content":"Hi"}]}`),
		Stream: false,
	}

	resp, err := exec.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]any
	if err := json.Unmarshal(resp.Body, &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	choices, ok := body["choices"].([]any)
	if !ok || len(choices) == 0 {
		t.Fatal("expected choices")
	}
	msg := choices[0].(map[string]any)["message"].(map[string]any)
	if msg["content"] != "Final result" {
		t.Fatalf("expected 'Final result', got %v", msg["content"])
	}
}

func TestDevinCLIExecutor_BinaryNotFound(t *testing.T) {
	t.Setenv("CLI_DEVIN_BIN", "/nonexistent/devin/binary")

	exec := NewDevinCLIExecutor(NewBaseExecutor())
	req := &Request{
		Model:  "devin/swe-1.6-fast",
		Body:   []byte(`{"messages":[]}`),
		Stream: true,
	}

	_, err := exec.ExecuteStream(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for missing binary")
	}
}

func writeMockDevin(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "devin")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write mock: %v", err)
	}
	return path
}

func TestResolveDevinBin_PrefersEnv(t *testing.T) {
	dir := t.TempDir()
	mock := filepath.Join(dir, "devin")
	if err := os.WriteFile(mock, []byte("# mock"), 0o755); err != nil {
		t.Fatalf("write mock: %v", err)
	}
	t.Setenv("CLI_DEVIN_BIN", mock)

	bin, err := resolveDevinBin()
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if bin != mock {
		t.Fatalf("expected %s, got %s", mock, bin)
	}
}

func TestFlatPromptFromMessages(t *testing.T) {
	body := []byte(`{"messages":[{"role":"system","content":"sys"},{"role":"user","content":"hi"},{"role":"assistant","content":"hello"}]}`)
	prompt, err := FlatPromptFromMessages(body)
	if err != nil {
		t.Fatalf("flat prompt: %v", err)
	}
	expected := fmt.Sprintf("%s\n%s\n%s", "[System] sys", "[User] hi", "[Assistant] hello")
	if prompt != expected {
		t.Fatalf("expected %q, got %q", expected, prompt)
	}
}
