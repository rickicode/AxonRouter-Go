package logging

import (
	"encoding/json"
	"fmt"
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
		msg           string
		wantComponent string
		wantClean     string
	}{
		{"[ROUTER] server started", "ROUTER", "server started"},
		{"  [DB]   connected  ", "DB", "connected"},
		{"plain message", "", "plain message"},
		{"[missing bracket", "", "[missing bracket"},
		{"[has space] ignored", "", "[has space] ignored"},
		{"[A]", "A", ""},
	}
	for _, c := range cases {
		gotComp, gotClean := extractComponent(c.msg)
		if gotComp != c.wantComponent || gotClean != c.wantClean {
			t.Errorf("extractComponent(%q) = (%q, %q), want (%q, %q)",
				c.msg, gotComp, gotClean, c.wantComponent, c.wantClean)
		}
	}
}

func TestComponentExtractionInFileOutput(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AXON_LOG_FILE_PATH", filepath.Join(dir, "app.log"))
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "dumb")

	Init("json")
	t.Cleanup(StopLogRotation)
	Logger.Info("[ROUTER] server started")

	data, err := os.ReadFile(LogFilePath)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	var entry jsonLogEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatalf("unmarshal log line: %v", err)
	}
	if entry.Component != "ROUTER" {
		t.Errorf("component = %q, want ROUTER", entry.Component)
	}
	if entry.Message != "server started" {
		t.Errorf("message = %q, want 'server started'", entry.Message)
	}
}

func TestComponentAttrWinsOverPrefix(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AXON_LOG_FILE_PATH", filepath.Join(dir, "app.log"))
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "dumb")

	Init("json")
	t.Cleanup(StopLogRotation)
	Logger.Info("[ROUTER] server started", slog.String("component", "api"))

	data, err := os.ReadFile(LogFilePath)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	var entry jsonLogEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatalf("unmarshal log line: %v", err)
	}
	if entry.Component != "api" {
		t.Errorf("component = %q, want api", entry.Component)
	}
}

func TestLoadRotationConfigDefaults(t *testing.T) {
	t.Setenv("AXON_LOG_FILE_PATH", "")
	t.Setenv("AXON_LOG_MAX_SIZE", "")
	t.Setenv("AXON_LOG_RETENTION_DAYS", "")
	t.Setenv("AXON_LOG_MAX_FILES", "")
	LogFilePath = "/tmp/axonrouter.log"

	cfg := LoadRotationConfig()
	if cfg.LogFilePath != "/tmp/axonrouter.log" {
		t.Errorf("LogFilePath = %q, want /tmp/axonrouter.log", cfg.LogFilePath)
	}
	if cfg.MaxSizeBytes != 50*1024*1024 {
		t.Errorf("MaxSizeBytes = %d, want 50MB", cfg.MaxSizeBytes)
	}
	if cfg.RetentionDays != 7 {
		t.Errorf("RetentionDays = %d, want 7", cfg.RetentionDays)
	}
	if cfg.MaxFiles != 20 {
		t.Errorf("MaxFiles = %d, want 20", cfg.MaxFiles)
	}
}

func TestLoadRotationConfigFromEnv(t *testing.T) {
	t.Setenv("AXON_LOG_FILE_PATH", "/custom/app.log")
	t.Setenv("AXON_LOG_MAX_SIZE", "10MB")
	t.Setenv("AXON_LOG_RETENTION_DAYS", "3")
	t.Setenv("AXON_LOG_MAX_FILES", "5")

	cfg := LoadRotationConfig()
	if cfg.LogFilePath != "/custom/app.log" {
		t.Errorf("LogFilePath = %q, want /custom/app.log", cfg.LogFilePath)
	}
	if cfg.MaxSizeBytes != 10*1024*1024 {
		t.Errorf("MaxSizeBytes = %d, want 10MB", cfg.MaxSizeBytes)
	}
	if cfg.RetentionDays != 3 {
		t.Errorf("RetentionDays = %d, want 3", cfg.RetentionDays)
	}
	if cfg.MaxFiles != 5 {
		t.Errorf("MaxFiles = %d, want 5", cfg.MaxFiles)
	}
}

func TestRotatingFileWriterRotatesOnSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	w, err := NewRotatingFileWriter(path, 100, 10, 7)
	if err != nil {
		t.Fatalf("NewRotatingFileWriter: %v", err)
	}
	defer w.Close()

	if _, err := w.Write([]byte("line 1\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Second write pushes the file past 100 bytes and triggers rotation.
	if _, err := w.Write(make([]byte, 200)); err != nil {
		t.Fatalf("write large payload: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	rotated := 0
	for _, e := range entries {
		if e.Name() != "app.log" {
			rotated++
		}
	}
	if rotated == 0 {
		t.Fatalf("expected rotated file, got %v", entries)
	}
}

func TestRotatingFileWriterPrunesMaxFiles(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "app.log")
	now := time.Now()
	// Seed three rotated files older than the active log.
	for i := 0; i < 3; i++ {
		name := fmt.Sprintf("app.2026-01-0%d_00000%d.log", i+1, i)
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatalf("write rotated file: %v", err)
		}
		if err := os.Chtimes(path, now.Add(-time.Duration(4-i)*time.Hour), now.Add(-time.Duration(4-i)*time.Hour)); err != nil {
			t.Fatalf("chtimes: %v", err)
		}
	}
	// Active log is newest.
	if err := os.WriteFile(base, []byte("active"), 0o644); err != nil {
		t.Fatalf("write active file: %v", err)
	}
	if err := os.Chtimes(base, now, now); err != nil {
		t.Fatalf("chtimes active: %v", err)
	}

	w, err := NewRotatingFileWriter(base, 1024, 1, 7)
	if err != nil {
		t.Fatalf("NewRotatingFileWriter: %v", err)
	}
	defer w.Close()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var rotated []string
	for _, e := range entries {
		if e.Name() != "app.log" {
			rotated = append(rotated, e.Name())
		}
	}
	if len(rotated) != 1 {
		t.Errorf("expected 1 rotated file, got %d: %v", len(rotated), rotated)
	}
}

func TestRotatingFileWriterPrunesByRetention(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "app.log")
	// Old rotated file past retention.
	old := filepath.Join(dir, "app.2020-01-01_000000.log")
	if err := os.WriteFile(old, []byte("old"), 0o644); err != nil {
		t.Fatalf("write old file: %v", err)
	}
	if err := os.Chtimes(old, time.Now().Add(-30*24*time.Hour), time.Now().Add(-30*24*time.Hour)); err != nil {
		t.Fatalf("chtimes old: %v", err)
	}
	// Active log.
	if err := os.WriteFile(base, []byte("active"), 0o644); err != nil {
		t.Fatalf("write active file: %v", err)
	}

	w, err := NewRotatingFileWriter(base, 1024, 10, 7)
	if err != nil {
		t.Fatalf("NewRotatingFileWriter: %v", err)
	}
	defer w.Close()

	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("old rotated file should have been pruned by retention")
	}
}

func TestInitCreatesLogDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "logs", "app.log")
	t.Setenv("AXON_LOG_FILE_PATH", path)
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "dumb")

	Init("json")
	t.Cleanup(StopLogRotation)

	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		t.Fatalf("log directory should be created: %v", err)
	}
}

func TestParseByteSize(t *testing.T) {
	cases := []struct {
		raw  string
		want int64
	}{
		{"", 100},
		{"50", 50},
		{"10K", 10 * 1024},
		{"10kb", 10 * 1024},
		{"2MB", 2 * 1024 * 1024},
		{"1gb", 1024 * 1024 * 1024},
		{"bad", 100},
		{"-5", 100},
	}
	for _, c := range cases {
		if got := parseByteSize(c.raw, 100); got != c.want {
			t.Errorf("parseByteSize(%q, 100) = %d, want %d", c.raw, got, c.want)
		}
	}
}
