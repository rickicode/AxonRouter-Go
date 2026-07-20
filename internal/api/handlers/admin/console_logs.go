package admin

import (
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

const (
	consoleLogPath = "/tmp/axonrouter.log"
	maxConsoleLines = 500
)

// ConsoleLogsResponse is the shape returned by GET /api/admin/console-logs.
type ConsoleLogsResponse struct {
	Lines []string `json:"lines"`
	Path  string   `json:"path"`
}

// ConsoleLogsHandler returns the most recent lines from the on-disk application log.
// It is intentionally simple: the dashboard polls this endpoint every few seconds.
type ConsoleLogsHandler struct{}

// NewConsoleLogsHandler creates a handler for the console log endpoint.
func NewConsoleLogsHandler() *ConsoleLogsHandler {
	return &ConsoleLogsHandler{}
}

// Get reads the log file from the end and returns up to maxConsoleLines lines.
func (h *ConsoleLogsHandler) Get(c *gin.Context) {
	lines, err := tailLogLines(consoleLogPath, maxConsoleLines)
	if err != nil {
		// Avoid leaking detailed filesystem errors to the UI.
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read console log"})
		return
	}
	c.JSON(http.StatusOK, ConsoleLogsResponse{
		Lines: lines,
		Path:  consoleLogPath,
	})
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
