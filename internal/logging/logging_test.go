package logging

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestExtractComponent(t *testing.T) {
	cases := []struct {
		msg       string
		wantMsg   string
		wantComp  string
	}{
		{"[ROUTER] server started", "server started", "ROUTER"},
		{"[  proxy  ]connection timeout", "connection timeout", "proxy"},
		{"server started", "server started", ""},
		{"[] empty", "[] empty", ""},
		{"no prefix here", "no prefix here", ""},
	}
	for _, c := range cases {
		gotMsg, gotComp := extractComponent(c.msg)
		if gotMsg != c.wantMsg || gotComp != c.wantComp {
			t.Errorf("extractComponent(%q) = (%q, %q), want (%q, %q)", c.msg, gotMsg, gotComp, c.wantMsg, c.wantComp)
		}
	}
}

func TestTeeHandler_ExtractsComponentPrefix(t *testing.T) {
	path := filepath.Join(t.TempDir(), "component.log")
	w, err := NewRotatingFileWriter(path, 1024*1024, 0, 3)
	if err != nil {
		t.Fatalf("NewRotatingFileWriter: %v", err)
	}
	defer w.Close()

	tee, err := NewTeeHandler(slog.NewJSONHandler(io.Discard, nil), w)
	if err != nil {
		t.Fatalf("NewTeeHandler: %v", err)
	}

	logger := slog.New(tee)
	logger.Info("[ROUTER] server started")

	// Force a fresh read; close writer first to flush.
	_ = w.Close()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(content), `"component":"ROUTER"`) {
		t.Errorf("expected component extracted from prefix, got:\n%s", string(content))
	}
	if !strings.Contains(string(content), `"msg":"server started"`) {
		t.Errorf("expected message with prefix removed, got:\n%s", string(content))
	}
}

func TestTeeHandler_ExplicitComponentAttributeWins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "component.log")
	w, err := NewRotatingFileWriter(path, 1024*1024, 0, 3)
	if err != nil {
		t.Fatalf("NewRotatingFileWriter: %v", err)
	}
	defer w.Close()

	tee, err := NewTeeHandler(slog.NewJSONHandler(io.Discard, nil), w)
	if err != nil {
		t.Fatalf("NewTeeHandler: %v", err)
	}

	logger := slog.New(tee)
	logger.Info("[ROUTER] server started", slog.String("component", "api"))

	_ = w.Close()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(content), `"component":"api"`) {
		t.Errorf("expected explicit component attribute to win, got:\n%s", string(content))
	}
}

func TestLogConfigFromEnv(t *testing.T) {
	t.Setenv("AXON_LOG_FILE_PATH", "/var/log/axon.log")
	t.Setenv("AXON_LOG_MAX_SIZE", "100")
	t.Setenv("AXON_LOG_RETENTION_DAYS", "14")
	t.Setenv("AXON_LOG_MAX_FILES", "50")

	cfg := logConfigFromEnv()
	if cfg.Path != "/var/log/axon.log" {
		t.Errorf("path = %q, want /var/log/axon.log", cfg.Path)
	}
	if cfg.MaxSizeBytes != 100*1024*1024 {
		t.Errorf("max size = %d, want %d", cfg.MaxSizeBytes, 100*1024*1024)
	}
	if cfg.RetentionDays != 14 {
		t.Errorf("retention = %d, want 14", cfg.RetentionDays)
	}
	if cfg.MaxFiles != 50 {
		t.Errorf("max files = %d, want 50", cfg.MaxFiles)
	}
}

func TestLogConfigFromEnv_Defaults(t *testing.T) {
	t.Setenv("AXON_LOG_FILE_PATH", "")
	t.Setenv("AXON_LOG_MAX_SIZE", "")
	t.Setenv("AXON_LOG_RETENTION_DAYS", "")
	t.Setenv("AXON_LOG_MAX_FILES", "")

	cfg := logConfigFromEnv()
	if cfg.Path != defaultLogPath {
		t.Errorf("path = %q, want %q", cfg.Path, defaultLogPath)
	}
	if cfg.MaxSizeBytes != defaultLogMaxSizeMB*1024*1024 {
		t.Errorf("max size = %d, want %d", cfg.MaxSizeBytes, defaultLogMaxSizeMB*1024*1024)
	}
	if cfg.RetentionDays != defaultLogRetentionDays {
		t.Errorf("retention = %d, want %d", cfg.RetentionDays, defaultLogRetentionDays)
	}
	if cfg.MaxFiles != defaultLogMaxFiles {
		t.Errorf("max files = %d, want %d", cfg.MaxFiles, defaultLogMaxFiles)
	}
}

func TestRotatingFileWriter_RotatesOnSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	w, err := NewRotatingFileWriter(path, 100, 0, 3)
	if err != nil {
		t.Fatalf("NewRotatingFileWriter: %v", err)
	}
	defer w.Close()

	first := []byte(strings.Repeat("a", 80) + "\n")
	if _, err := w.Write(first); err != nil {
		t.Fatalf("write first: %v", err)
	}

	second := []byte(strings.Repeat("b", 30) + "\n")
	if _, err := w.Write(second); err != nil {
		t.Fatalf("write second: %v", err)
	}

	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read current: %v", err)
	}
	if !bytes.Equal(current, second) {
		t.Errorf("current file = %q, want %q", current, second)
	}

	backup, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if !bytes.Equal(backup, first) {
		t.Errorf("backup file = %q, want %q", backup, first)
	}
}

func TestRotatingFileWriter_EnforcesMaxFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	w, err := NewRotatingFileWriter(path, 10, 0, 2)
	if err != nil {
		t.Fatalf("NewRotatingFileWriter: %v", err)
	}

	data := []byte(strings.Repeat("x", 12) + "\n")
	for i := 0; i < 3; i++ {
		if _, err := w.Write(data); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	_ = w.Close()

	if _, err := os.Stat(path + ".3"); !os.IsNotExist(err) {
		t.Errorf("backup .3 should not exist (maxFiles=2)")
	}
	if _, err := os.Stat(path + ".2"); err != nil {
		t.Errorf("backup .2 should exist: %v", err)
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Errorf("backup .1 should exist: %v", err)
	}
}

func TestRotatingFileWriter_RetentionCleanup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	w, err := NewRotatingFileWriter(path, 100, 0, 3)
	if err != nil {
		t.Fatalf("NewRotatingFileWriter: %v", err)
	}
	oldData := []byte(strings.Repeat("a", 110) + "\n")
	if _, err := w.Write(oldData); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = w.Close()

	// Age the rotated backup beyond the retention window.
	oldTime := time.Now().AddDate(0, 0, -10)
	if err := os.Chtimes(path+".1", oldTime, oldTime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	w2, err := NewRotatingFileWriter(path, 100, 5, 3)
	if err != nil {
		t.Fatalf("NewRotatingFileWriter: %v", err)
	}
	defer w2.Close()
	if err := w2.CleanupRetention(); err != nil {
		t.Fatalf("CleanupRetention: %v", err)
	}

	if _, err := os.Stat(path + ".1"); !os.IsNotExist(err) {
		t.Errorf("expired backup should be removed")
	}
}

func TestRotatingFileWriter_Truncate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	w, err := NewRotatingFileWriter(path, 1024, 0, 3)
	if err != nil {
		t.Fatalf("NewRotatingFileWriter: %v", err)
	}
	if _, err := w.Write([]byte("hello world\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := w.Truncate(); err != nil {
		t.Fatalf("Truncate: %v", err)
	}
	_ = w.Close()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Size() != 0 {
		t.Errorf("size = %d, want 0", info.Size())
	}
}
