package admin

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	consoleLogPath = "/tmp/axonrouter.log"
	maxConsoleLines = 500
)

// ConsoleLogEntry is a single structured log entry parsed from the JSON-lines log file.
type ConsoleLogEntry struct {
	Timestamp  string         `json:"ts"`
	Level      string         `json:"level"`
	Message    string         `json:"msg"`
	Component  string         `json:"component,omitempty"`
	RequestID  string         `json:"request_id,omitempty"`
	Provider   string         `json:"provider,omitempty"`
	Model      string         `json:"model,omitempty"`
	Connection string         `json:"conn,omitempty"`
	Error      string         `json:"error,omitempty"`
	Extra      map[string]any `json:"extra,omitempty"`
}

// ConsoleLogsResponse is the shape returned by GET /api/admin/console-logs.
type ConsoleLogsResponse struct {
	Entries []ConsoleLogEntry `json:"entries"`
	Path    string            `json:"path"`
	Total   int               `json:"total"`
}

// ConsoleLogsHandler returns the most recent structured log entries from the on-disk log file.
type ConsoleLogsHandler struct{}

// NewConsoleLogsHandler creates a handler for the console log endpoint.
func NewConsoleLogsHandler() *ConsoleLogsHandler {
	return &ConsoleLogsHandler{}
}

// Get reads the log file, parses JSON lines, applies filters, and returns structured entries.
// Query params:
//   - level: minimum level (debug|info|warn|error), default "debug" (all)
//   - search: text search across message, component, provider, model, error
//   - limit: max entries (default 500, max 2000)
func (h *ConsoleLogsHandler) Get(c *gin.Context) {
	levelFilter := strings.ToLower(c.DefaultQuery("level", "debug"))
	search := strings.ToLower(c.Query("search"))
	limit := maxConsoleLines

	rawLines, err := tailLogLines(consoleLogPath, limit*2) // read more to account for filtering
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read console log"})
		return
	}

	entries := make([]ConsoleLogEntry, 0, len(rawLines))
	for _, line := range rawLines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		entry, ok := parseLogLine(line)
		if !ok {
			continue
		}

		// Level filter
		if !levelGTE(entry.Level, levelFilter) {
			continue
		}

		// Search filter
		if search != "" && !matchesSearch(entry, search) {
			continue
		}

		entries = append(entries, entry)
	}

	// Trim to limit (take the most recent entries)
	if len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}

	c.JSON(http.StatusOK, ConsoleLogsResponse{
		Entries: entries,
		Path:    consoleLogPath,
		Total:   len(entries),
	})
}

// parseLogLine attempts to parse a line as a structured JSON log entry.
// Falls back to a synthetic entry for raw text lines.
func parseLogLine(line string) (ConsoleLogEntry, bool) {
	var entry ConsoleLogEntry
	if err := json.Unmarshal([]byte(line), &entry); err == nil && entry.Level != "" {
		return entry, true
	}

	// Fallback: treat as raw text (backward compat with old log format)
	entry = ConsoleLogEntry{
		Level:   inferLevelFromText(line),
		Message: line,
	}
	return entry, true
}

// levelGTE returns true if level >= minLevel.
func levelGTE(level, minLevel string) bool {
	return levelPriority(level) >= levelPriority(minLevel)
}

func levelPriority(l string) int {
	switch strings.ToLower(l) {
	case "debug":
		return 0
	case "info":
		return 1
	case "warn", "warning":
		return 2
	case "error":
		return 3
	case "fatal":
		return 4
	default:
		return 0
	}
}

// matchesSearch checks if the search term appears in any text field of the entry.
func matchesSearch(e ConsoleLogEntry, search string) bool {
	search = strings.ToLower(search)
	if strings.Contains(strings.ToLower(e.Message), search) {
		return true
	}
	if strings.Contains(strings.ToLower(e.Component), search) {
		return true
	}
	if strings.Contains(strings.ToLower(e.Provider), search) {
		return true
	}
	if strings.Contains(strings.ToLower(e.Model), search) {
		return true
	}
	if strings.Contains(strings.ToLower(e.Error), search) {
		return true
	}
	if strings.Contains(strings.ToLower(e.RequestID), search) {
		return true
	}
	// Also search in extra fields
	for _, v := range e.Extra {
		if s, ok := v.(string); ok && strings.Contains(strings.ToLower(s), search) {
			return true
		}
	}
	return false
}

// inferLevelFromText tries to guess the level from raw text lines (for backward compat).
func inferLevelFromText(line string) string {
	lower := strings.ToLower(line)
	switch {
	case strings.Contains(lower, "error") || strings.Contains(lower, "failed"):
		return "error"
	case strings.Contains(lower, "warn"):
		return "warn"
	case strings.Contains(lower, "debug"):
		return "debug"
	default:
		return "info"
	}
}

// tailLogLines reads the last n lines from path using a reverse scan.
// If the file does not exist it returns an empty slice without an error.
func tailLogLines(path string, n int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	defer f.Close()

	stats, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := stats.Size()
	if size == 0 {
		return []string{}, nil
	}

	// Scan from the end in small blocks to avoid loading huge files.
	const blockSize = 4096
	lines := make([]string, 0, n)
	buf := make([]byte, blockSize)
	var leftover string
	offset := size

	for offset > 0 && len(lines) < n {
		readLen := int64(blockSize)
		if offset < readLen {
			readLen = offset
		}
		offset -= readLen

		_, err := f.ReadAt(buf[:readLen], offset)
		if err != nil {
			return nil, err
		}

		chunk := string(buf[:readLen]) + leftover

		// Split from the end, keeping the trailing partial line in leftover.
		start := len(chunk)
		for i := len(chunk) - 1; i >= 0 && len(lines) < n; i-- {
			if chunk[i] == '\n' {
				line := chunk[i+1 : start]
				if len(line) > 0 {
					lines = append([]string{line}, lines...)
				}
				start = i
			}
		}
		leftover = chunk[:start]
	}

	// First line of the file has no preceding newline.
	if len(lines) < n && len(leftover) > 0 {
		lines = append([]string{leftover}, lines...)
	}

	return lines, nil
}
