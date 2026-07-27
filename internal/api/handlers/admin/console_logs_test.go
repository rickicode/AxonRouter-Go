package admin

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
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

func setTestLogPath(t *testing.T, path string) {
	t.Helper()
	oldPath := logging.LogFilePath
	logging.LogFilePath = path
	logging.LogBroadcaster = logging.NewBroadcaster()
	t.Cleanup(func() {
		logging.LogFilePath = oldPath
	})
}

// sseRecorder provides a real streaming writer for SSE tests. httptest.ResponseRecorder
// buffers the entire response and is not safe for concurrent reads, so we redirect
// body writes to a condition-variable-backed buffer that supports blocking reads.
type sseRecorder struct {
	rec *httptest.ResponseRecorder
	sb  *streamingBuffer
}

func newSSERecorder(t *testing.T) *sseRecorder {
	t.Helper()
	return &sseRecorder{
		rec: httptest.NewRecorder(),
		sb:  newStreamingBuffer(),
	}
}

func (r *sseRecorder) ginContext(t *testing.T, method, path string) (*gin.Context, context.CancelFunc) {
	t.Helper()
	c, _ := gin.CreateTestContext(r.rec)
	ctx, cancel := context.WithCancel(context.Background())
	c.Request = httptest.NewRequest(method, path, nil).WithContext(ctx)
	c.Writer = &streamingResponseWriter{ResponseWriter: c.Writer, sb: r.sb}
	return c, cancel
}

func (r *sseRecorder) reader() *bufio.Reader { return bufio.NewReader(r.sb) }

func (r *sseRecorder) close() { r.sb.Close() }

// streamingBuffer is a thread-safe byte buffer with blocking reads.
type streamingBuffer struct {
	mu     sync.Mutex
	cond   *sync.Cond
	buf    bytes.Buffer
	closed bool
}

func newStreamingBuffer() *streamingBuffer {
	s := &streamingBuffer{}
	s.cond = sync.NewCond(&s.mu)
	return s
}

func (s *streamingBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, io.ErrClosedPipe
	}
	n, err := s.buf.Write(p)
	s.cond.Broadcast()
	return n, err
}

func (s *streamingBuffer) Read(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for s.buf.Len() == 0 && !s.closed {
		s.cond.Wait()
	}
	if s.buf.Len() == 0 {
		return 0, io.EOF
	}
	return s.buf.Read(p)
}

func (s *streamingBuffer) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.cond.Broadcast()
}

// streamingResponseWriter delegates body writes to a streaming buffer while
// letting the wrapped ResponseRecorder capture status and headers.
type streamingResponseWriter struct {
	gin.ResponseWriter
	sb *streamingBuffer
}

func (w *streamingResponseWriter) Write(b []byte) (int, error) { return w.sb.Write(b) }
func (w *streamingResponseWriter) Flush()                      {}

func parseSSEData(t *testing.T, reader *bufio.Reader) map[string]any {
	t.Helper()
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("failed to read sse line: %v", err)
	}
	if line == "\n" {
		line, err = reader.ReadString('\n')
		if err != nil {
			t.Fatalf("failed to read sse line after blank: %v", err)
		}
	}
	if !strings.HasPrefix(line, "data: ") {
		t.Fatalf("expected data: line, got %q", line)
	}
	payload := strings.TrimPrefix(strings.TrimSuffix(line, "\n"), "data: ")
	var out map[string]any
	if err := json.Unmarshal([]byte(payload), &out); err != nil {
		t.Fatalf("failed to unmarshal sse payload %q: %v", payload, err)
	}
	reader.ReadString('\n')
	return out
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

func TestParseLogLine_InferLevelFromJSONWithoutLevel(t *testing.T) {
	line := `{"ts":"2026-01-01T00:00:00Z","msg":"database is locked","component":"sqlite"}`
	entry, ok := parseLogLine(line)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if entry.Level != "error" {
		t.Errorf("level = %q, want error", entry.Level)
	}
	if entry.Component != "sqlite" {
		t.Errorf("component = %q, want sqlite", entry.Component)
	}
}

func TestParseLogLine_InferWarn(t *testing.T) {
	cases := []struct {
		msg   string
		level string
	}{
		{"quota exhausted for provider openai", "warn"},
		{"upstream error, will try next", "warn"},
		{"SQLITE_BUSY: database is locked", "error"},
		{"error response from upstream", "error"},
		{"request started", "info"},
	}
	for _, tc := range cases {
		t.Run(tc.msg, func(t *testing.T) {
			entry, _ := parseLogLine(tc.msg)
			if entry.Level != tc.level {
				t.Errorf("level = %q, want %q", entry.Level, tc.level)
			}
		})
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
	if resp.Entries == nil {
		resp.Entries = []ConsoleLogEntry{}
	}
}

func TestGet_ReturnsEntries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	path := setupTestLog(t,
		`{"ts":"2026-01-01T00:00:00Z","level":"info","msg":"first","component":"router"}`,
		`{"ts":"2026-01-01T00:00:01Z","level":"error","msg":"second","component":"proxy"}`,
	)
	setTestLogPath(t, path)

	handler := &ConsoleLogsHandler{}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/console-logs", nil)

	handler.Get(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp ConsoleLogsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(resp.Entries))
	}
	if resp.Entries[1].Message != "second" {
		t.Errorf("last message = %q, want second", resp.Entries[1].Message)
	}
	if resp.Path != path {
		t.Errorf("path = %q, want %q", resp.Path, path)
	}
	if resp.Total != 2 {
		t.Errorf("total = %d, want 2", resp.Total)
	}
}

func TestGet_FilterByLevel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	path := setupTestLog(t,
		`{"ts":"2026-01-01T00:00:00Z","level":"debug","msg":"debug msg"}`,
		`{"ts":"2026-01-01T00:00:01Z","level":"error","msg":"error msg"}`,
	)
	setTestLogPath(t, path)

	handler := &ConsoleLogsHandler{}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/console-logs?level=error", nil)

	handler.Get(c)

	var resp ConsoleLogsResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(resp.Entries))
	}
	if resp.Entries[0].Message != "error msg" {
		t.Errorf("message = %q, want error msg", resp.Entries[0].Message)
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

func TestStream_EmitsInitAndLiveLines(t *testing.T) {
	gin.SetMode(gin.TestMode)
	path := setupTestLog(t, `{"ts":"2026-01-01T00:00:00Z","level":"info","msg":"seed","component":"router"}`)
	setTestLogPath(t, path)

	handler := &ConsoleLogsHandler{}
	rec := newSSERecorder(t)
	c, cancel := rec.ginContext(t, "GET", "/console-logs/stream")
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.Stream(c)
		rec.close()
	}()

	reader := rec.reader()

	evt := parseSSEData(t, reader)
	if evt["type"] != "init" {
		t.Fatalf("expected init event, got %v", evt)
	}

	evt = parseSSEData(t, reader)
	if evt["type"] != "line" {
		t.Fatalf("expected line event for seed, got %v", evt)
	}
	entry, ok := evt["entry"].(map[string]any)
	if !ok {
		t.Fatalf("expected entry map, got %T", evt["entry"])
	}
	if entry["msg"] != "seed" {
		t.Errorf("seed msg = %v, want seed", entry["msg"])
	}

	logging.LogBroadcaster.BroadcastLine(`{"ts":"2026-01-01T00:00:01Z","level":"warn","msg":"live","component":"proxy"}`)

	evt = parseSSEData(t, reader)
	if evt["type"] != "line" {
		t.Fatalf("expected line event for live line, got %v", evt)
	}
	entry, ok = evt["entry"].(map[string]any)
	if !ok {
		t.Fatalf("expected entry map, got %T", evt["entry"])
	}
	if entry["msg"] != "live" {
		t.Errorf("live msg = %v, want live", entry["msg"])
	}

	cancel()
	<-done
}

func TestClear_TruncatesAndBroadcastsClear(t *testing.T) {
	gin.SetMode(gin.TestMode)
	path := setupTestLog(t,
		`{"ts":"2026-01-01T00:00:00Z","level":"info","msg":"before","component":"router"}`,
	)
	setTestLogPath(t, path)

	handler := &ConsoleLogsHandler{}

	rec := newSSERecorder(t)
	c, cancel := rec.ginContext(t, "GET", "/console-logs/stream")
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.Stream(c)
		rec.close()
	}()

	reader := rec.reader()

	evt := parseSSEData(t, reader)
	if evt["type"] != "init" {
		t.Fatalf("expected init event, got %v", evt)
	}
	evt = parseSSEData(t, reader)
	if evt["type"] != "line" {
		t.Fatalf("expected line event, got %v", evt)
	}

	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest("DELETE", "/console-logs", nil)
	handler.Clear(c2)

	if w2.Code != http.StatusOK {
		t.Fatalf("clear status = %d, want 200", w2.Code)
	}

	evt = parseSSEData(t, reader)
	if evt["type"] != "clear" {
		t.Fatalf("expected clear event, got %v", evt)
	}

	stat, err := os.Stat(path)
	if err != nil {
		t.Fatalf("failed to stat cleared log: %v", err)
	}
	if stat.Size() != 0 {
		t.Errorf("cleared file size = %d, want 0", stat.Size())
	}

	cancel()
	<-done
}
