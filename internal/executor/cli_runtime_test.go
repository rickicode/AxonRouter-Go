package executor

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCLIRunCapturesStdoutStderrStdinEnvWorkDirAndExitCode(t *testing.T) {
	dir := t.TempDir()
	out := RunCLI(context.Background(), CLIInvocation{
		Command: []string{"/bin/sh", "-c", "printf 'out:%s:%s:%s' \"$AXON_TEST_VALUE\" \"$(cat)\" \"$(pwd)\"; printf 'err-line' >&2; exit 7"},
		Env:     []string{"AXON_TEST_VALUE=env-value"},
		Stdin:   []byte("stdin-value"),
		WorkDir: dir,
	})

	if out.Err == nil {
		t.Fatal("RunCLI error is nil, want exit error")
	}
	if out.TimedOut {
		t.Fatal("RunCLI timed out unexpectedly")
	}
	if out.ExitCode != 7 {
		t.Fatalf("ExitCode=%d, want 7", out.ExitCode)
	}
	wantStdout := "out:env-value:stdin-value:" + dir
	if out.Stdout != wantStdout {
		t.Fatalf("Stdout=%q, want %q", out.Stdout, wantStdout)
	}
	if out.Stderr != "err-line" {
		t.Fatalf("Stderr=%q, want err-line", out.Stderr)
	}
}

func TestCLIRunReturnsLaunchErrors(t *testing.T) {
	out := RunCLI(context.Background(), CLIInvocation{Command: []string{"/definitely/missing/axon-cli-binary"}})

	if out.Err == nil {
		t.Fatal("RunCLI error is nil, want launch error")
	}
	if out.ExitCode != -1 {
		t.Fatalf("ExitCode=%d, want -1", out.ExitCode)
	}
}

func TestCLIRunTimesOutAndKillsProcessGroup(t *testing.T) {
	ctx := context.Background()
	out := RunCLI(ctx, CLIInvocation{
		Command:          []string{"/bin/sh", "-c", "sleep 10 & wait"},
		Timeout:          50 * time.Millisecond,
		GracefulShutdown: 20 * time.Millisecond,
	})

	if !out.TimedOut {
		t.Fatal("TimedOut=false, want true")
	}
	if out.Err == nil {
		t.Fatal("RunCLI error is nil, want timeout error")
	}
	if !errors.Is(out.Err, context.DeadlineExceeded) {
		t.Fatalf("Err=%v, want context deadline exceeded", out.Err)
	}
}

func TestCLIResolveBinChecksOverrideKnownPathsThenPath(t *testing.T) {
	dir := t.TempDir()
	known := filepath.Join(dir, "known-cli")
	pathBin := filepath.Join(dir, "path-cli")
	for _, path := range []string{known, pathBin} {
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatalf("write executable %s: %v", path, err)
		}
	}
	t.Setenv("PATH", dir)
	t.Setenv("AXON_TEST_CLI", known)

	if got, err := ResolveBin("path-cli", "AXON_TEST_CLI", []string{"/missing"}); err != nil || got != known {
		t.Fatalf("ResolveBin override=(%q, %v), want %q", got, err, known)
	}
	t.Setenv("AXON_TEST_CLI", "")
	if got, err := ResolveBin("path-cli", "AXON_TEST_CLI", []string{known}); err != nil || got != known {
		t.Fatalf("ResolveBin known=(%q, %v), want %q", got, err, known)
	}
	if got, err := ResolveBin("path-cli", "", nil); err != nil || got != pathBin {
		t.Fatalf("ResolveBin path=(%q, %v), want %q", got, err, pathBin)
	}
}

func TestCLIFlatPromptFromMessagesPrefixesKnownRoles(t *testing.T) {
	body := []byte(`{"messages":[{"role":"system","content":"stay terse"},{"role":"user","content":"hello"},{"role":"assistant","content":"hi"},{"role":"tool","content":"ignored"}]}`)

	got, err := FlatPromptFromMessages(body)
	if err != nil {
		t.Fatalf("FlatPromptFromMessages error: %v", err)
	}
	want := "[System] stay terse\n[User] hello\n[Assistant] hi"
	if got != want {
		t.Fatalf("prompt=%q, want %q", got, want)
	}
}

func TestCLIOpenAIStreamChunks(t *testing.T) {
	chunk := OpenAIChunkFromText("resp-1", "model-a", "hello")
	if !strings.HasPrefix(chunk, "data: ") || !strings.HasSuffix(chunk, "\n\n") {
		t.Fatalf("chunk %q does not use SSE data framing", chunk)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(strings.TrimPrefix(strings.TrimSpace(chunk), "data: ")), &payload); err != nil {
		t.Fatalf("chunk JSON invalid: %v", err)
	}
	if payload["id"] != "resp-1" || payload["model"] != "model-a" || payload["object"] != "chat.completion.chunk" {
		t.Fatalf("unexpected payload metadata: %#v", payload)
	}
	choices := payload["choices"].([]any)
	delta := choices[0].(map[string]any)["delta"].(map[string]any)
	if delta["content"] != "hello" {
		t.Fatalf("delta content=%v, want hello", delta["content"])
	}
	if got := OpenAIDoneChunk(); got != "data: [DONE]\n\n" {
		t.Fatalf("done chunk=%q", got)
	}
	errChunk := OpenAIStreamErrorChunk("bad things")
	var errPayload map[string]any
	if err := json.Unmarshal([]byte(strings.TrimPrefix(strings.TrimSpace(errChunk), "data: ")), &errPayload); err != nil {
		t.Fatalf("error chunk JSON invalid: %v", err)
	}
	errObject := errPayload["error"].(map[string]any)
	if errObject["message"] != "bad things" {
		t.Fatalf("error message=%v, want bad things", errObject["message"])
	}
}
