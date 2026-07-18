package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
)

func captureTextOutput(t *testing.T, logFn func()) string {
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w

	logFn()

	w.Close()
	os.Stdout = oldStdout

	var buf strings.Builder
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("read pipe: %v", err)
	}
	return buf.String()
}

func TestTextHandlerColorsKeys(t *testing.T) {
	out := captureTextOutput(t, func() {
		Init("text")
		Logger.Info("request handled",
			slog.String("provider", "openai"),
			slog.String("conn", "c1"),
			slog.String("name", "myconn"),
			slog.String("host", "example.com"),
			slog.String("request_id", "req-123"),
			slog.String("client_ip", "1.2.3.4"),
			slog.String("user_agent", "tester"),
			slog.String("status", "200"),
			slog.String("method", "POST"),
			slog.String("proxy", "px"),
			slog.String("path", "/v1/chat"),
			slog.String("lat", "12ms"),
			slog.String("error", "none"),
			slog.String("body", "{}"),
			slog.String("model", "gpt-4o"),
			slog.String("account_id", "acc-1"),
			slog.String("unknown_key", "value"),
		)
	})

	cases := []struct {
		key   string
		color string
	}{
		{"provider", cyan},
		{"conn", dim},
		{"name", magenta + bold},
		{"host", cyan},
		{"request_id", yellow},
		{"client_ip", blue},
		{"user_agent", green},
		{"status", magenta},
		{"method", green},
		{"proxy", yellow},
		{"path", cyan},
		{"lat", white},
		{"error", red},
		{"body", white},
		{"model", yellow},
		{"account_id", blue},
	}
	for _, c := range cases {
		want := " \"" + c.color + c.key + reset + "\"="
		if !strings.Contains(out, want) {
			t.Errorf("missing colored key %q in text output:\n%s", c.key, out)
		}
	}
	if !strings.Contains(out, " unknown_key=value") {
		t.Errorf("unknown key should remain uncolored in text output:\n%s", out)
	}
}

func TestCompactHandlerColorsKnownKeys(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")

	var buf strings.Builder
	logger := slog.New(NewCompactHandler(&buf))
	logger.Info("request handled",
		slog.String("provider", "openai"),
		slog.String("conn", "c1"),
		slog.String("name", "myconn"),
		slog.String("host", "example.com"),
		slog.String("request_id", "req-123"),
		slog.String("client_ip", "1.2.3.4"),
		slog.String("user_agent", "tester"),
		slog.String("status", "200"),
		slog.String("method", "POST"),
		slog.String("proxy", "px"),
		slog.String("path", "/v1/chat"),
		slog.String("lat", "12ms"),
		slog.String("error", "none"),
		slog.String("body", "{}"),
		slog.String("model", "gpt-4o"),
		slog.String("account_id", "acc-1"),
		slog.String("unknown_key", "value"),
	)

	out := buf.String()
	cases := []struct {
		key   string
		color string
	}{
		{"provider", cyan},
		{"conn", dim},
		{"name", magenta + bold},
		{"host", cyan},
		{"request_id", yellow},
		{"client_ip", blue},
		{"user_agent", green},
		{"status", magenta},
		{"method", green},
		{"proxy", yellow},
		{"path", cyan},
		{"lat", white},
		{"error", red},
		{"body", white},
		{"model", yellow},
		{"account_id", blue},
	}
	for _, c := range cases {
		want := " " + c.color + c.key + reset + "="
		if !strings.Contains(out, want) {
			t.Errorf("missing colored key %q in output:\n%s", c.key, out)
		}
	}
	unknown := " " + dim + "unknown_key" + reset + "="
	if !strings.Contains(out, unknown) {
		t.Errorf("unknown key should fall back to dim in output:\n%s", out)
	}
}
