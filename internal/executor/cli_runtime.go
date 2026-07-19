package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type CLIInvocation struct {
	Command          []string
	Env              []string
	Stdin            []byte
	Timeout          time.Duration
	GracefulShutdown time.Duration
	WorkDir          string
}

type CLIOutput struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Err      error
	TimedOut bool
}

func RunCLI(ctx context.Context, inv CLIInvocation) CLIOutput {
	if len(inv.Command) == 0 || inv.Command[0] == "" {
		return CLIOutput{ExitCode: -1, Err: errors.New("cli command is empty")}
	}

	runCtx := ctx
	cancel := func() {}
	if inv.Timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, inv.Timeout)
	}
	defer cancel()

	cmd := exec.Command(inv.Command[0], inv.Command[1:]...)
	cmd.Dir = inv.WorkDir
	cmd.Env = append(os.Environ(), inv.Env...)
	cmd.Stdin = bytes.NewReader(inv.Stdin)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return CLIOutput{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: -1, Err: err}
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	var err error
	var timedOut bool
	select {
	case err = <-done:
	case <-runCtx.Done():
		timedOut = true
		terminateProcessGroup(cmd.Process.Pid, inv.GracefulShutdown)
		err = <-done
		if err == nil || errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			err = runCtx.Err()
		}
	}

	return CLIOutput{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode(err),
		Err:      err,
		TimedOut: timedOut,
	}
}

func terminateProcessGroup(pid int, gracefulShutdown time.Duration) {
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		return
	}
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	if gracefulShutdown > 0 {
		time.Sleep(gracefulShutdown)
	}
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

func ResolveBin(name, envOverride string, knownPaths []string) (string, error) {
	if envOverride != "" {
		if path := os.Getenv(envOverride); path != "" {
			if executableFile(path) {
				return path, nil
			}
			return "", fmt.Errorf("%s points to non-executable path %q", envOverride, path)
		}
		if executableFile(envOverride) {
			return envOverride, nil
		}
	}
	for _, path := range knownPaths {
		if executableFile(path) {
			return path, nil
		}
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return "", err
	}
	return path, nil
}

func executableFile(path string) bool {
	info, err := os.Stat(filepath.Clean(path))
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}

func FlatPromptFromMessages(body []byte) (string, error) {
	var request struct {
		Messages []struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		return "", err
	}

	lines := make([]string, 0, len(request.Messages))
	for _, message := range request.Messages {
		prefix, ok := rolePrefix(message.Role)
		if !ok {
			continue
		}
		content := messageContentText(message.Content)
		if content == "" {
			continue
		}
		lines = append(lines, prefix+" "+content)
	}
	return strings.Join(lines, "\n"), nil
}

func rolePrefix(role string) (string, bool) {
	switch role {
	case "system":
		return "[System]", true
	case "user":
		return "[User]", true
	case "assistant":
		return "[Assistant]", true
	default:
		return "", false
	}
}

func messageContentText(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if text, ok := m["text"].(string); ok {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	case nil:
		return ""
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(b)
	}
}

func OpenAIChunkFromText(responseID, model, text string) string {
	payload := map[string]any{
		"id":      responseID,
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]any{
			{
				"index": 0,
				"delta": map[string]any{"content": text},
			},
		},
	}
	return sseJSON(payload)
}

func OpenAIDoneChunk() string {
	return "data: [DONE]\n\n"
}

func OpenAIStreamErrorChunk(message string) string {
	payload := map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    "server_error",
		},
	}
	return sseJSON(payload)
}

func sseJSON(payload any) string {
	b, err := json.Marshal(payload)
	if err != nil {
		return "data: {}\n\n"
	}
	return "data: " + string(b) + "\n\n"
}
