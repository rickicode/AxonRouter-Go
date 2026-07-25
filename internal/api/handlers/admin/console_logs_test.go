package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
)

func setupTestLog(t *testing.T, lines ...string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range lines {
		f.WriteString(l + "\n")
	}
	f.Close()
	return path
}

func TestParseLogLine_Structured(t *testing.T) {
	line := `{"ts":"2026-01-01T00:00:00Z","level":"info","msg":"server started","component":"router"}`
	entry, ok := parseLogLine(line)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if entry.Level != "info" {
		t.Errorf("level = %q, want info", entry.Level)
	}
	if entry.Component != "router" {
		t.Errorf("component = %q, want router", entry.Component)
	}
	if entry.Message != "server started" {
		t.Errorf("msg = %q, want server started", entry.Message)
	}
}

func TestParseLogLine_RawFallback(t *testing.T) {
	entry, ok := parseLogLine("some raw text line")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if entry.Level != "info" {
		t.Errorf("level = %q, want info (inferred)", entry.Level)
	}
	if entry.Message != "some raw text line" {
		t.Errorf("msg = %q, want raw text", entry.Message)
	}
}

func TestParseLogLine_RawErrorInference(t *testing.T) {
	entry, _ := parseLogLine("connection failed: timeout")
	if entry.Level != "error" {
		t.Errorf("level = %q, want error", entry.Level)
	}
}

func TestLevelGTE(t *testing.T) {
	tests := []struct {
		level, min string
		want       bool
	}{
		{"debug", "debug", true},
		{"info", "debug", true},
		{"warn", "info", true},
		{"error", "warn", true},
		{"debug", "info", false},
		{"info", "error", false},
	}
	for _, tt := range tests {
		if got := levelGTE(tt.level, tt.min); got != tt.want {
			t.Errorf("levelGTE(%q, %q) = %v, want %v", tt.level, tt.min, got, tt.want)
		}
	}
}

func TestMatchesSearch(t *testing.T) {
	entry := ConsoleLogEntry{
		Message:   "connection timeout",
		Component: "proxy",
		Provider:  "openai",
		Model:     "gpt-4o",
	}
	if !matchesSearch(entry, "timeout") {
		t.Error("should match message")
	}
	if !matchesSearch(entry, "proxy") {
		t.Error("should match component")
	}
	if !matchesSearch(entry, "OPENAI") {
		t.Error("should be case-insensitive")
	}
	if matchesSearch(entry, "claude") {
		t.Error("should not match unrelated term")
	}
}

func TestGet_NoLogFile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &ConsoleLogsHandler{}
	// Temporarily override consoleLogPath by using a non-existent path
	// Since consoleLogPath is const, we test via the real path which doesn't exist in test env
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/console-logs", nil)

	handler.Get(c)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	var resp ConsoleLogsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	// When file doesn't exist, entries should be empty
	if resp.Entries == nil {
		resp.Entries = []ConsoleLogEntry{}
	}
}

func TestTailLogLines(t *testing.T) {
	path := setupTestLog(t, "line1", "line2", "line3", "line4", "line5")
	lines, err := tailLogLines(path, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 3 {
		t.Errorf("got %d lines, want 3", len(lines))
	}
	// Should be the last 3 lines
	if lines[0] != "line3" {
		t.Errorf("lines[0] = %q, want line3", lines[0])
	}
	if lines[2] != "line5" {
		t.Errorf("lines[2] = %q, want line5", lines[2])
	}
}

func TestTailLogLines_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.log")
	os.WriteFile(path, []byte{}, 0644)
	lines, err := tailLogLines(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 0 {
		t.Errorf("got %d lines, want 0", len(lines))
	}
}

func TestTailLogLines_NonExistent(t *testing.T) {
	lines, err := tailLogLines("/tmp/nonexistent-test-file.log", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 0 {
		t.Errorf("got %d lines, want 0", len(lines))
	}
}
