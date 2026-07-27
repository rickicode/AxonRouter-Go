package logging

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RotationConfig controls size-based rotation and retention for the structured
// log file. Values are populated from environment variables by LoadRotationConfig.
type RotationConfig struct {
	LogFilePath   string
	MaxSizeBytes  int64
	RetentionDays int
	MaxFiles      int
	CheckInterval time.Duration
}

const (
	defaultLogFilePath   = "/tmp/axonrouter.log"
	defaultMaxSizeBytes  = 50 * 1024 * 1024
	defaultRetentionDays = 7
	defaultMaxFiles      = 20
	defaultCheckInterval = 60 * time.Second
)

var (
	rotationMu     sync.Mutex
	rotationCancel context.CancelFunc
	rotationConfig RotationConfig
	logWriter      *RotatingFileWriter
)

// LoadRotationConfig reads AXON_LOG_* environment variables and returns the
// RotationConfig.  The current LogFilePath value is used as a fallback when the
// environment variable is unset, preserving callers that set LogFilePath before
// calling Init().
func LoadRotationConfig() RotationConfig {
	path := os.Getenv("AXON_LOG_FILE_PATH")
	if path == "" {
		path = LogFilePath
	}
	if path == "" {
		path = defaultLogFilePath
	}

	return RotationConfig{
		LogFilePath:   path,
		MaxSizeBytes:  parseByteSize(os.Getenv("AXON_LOG_MAX_SIZE"), defaultMaxSizeBytes),
		RetentionDays: parsePositiveInt(os.Getenv("AXON_LOG_RETENTION_DAYS"), defaultRetentionDays),
		MaxFiles:      parsePositiveInt(os.Getenv("AXON_LOG_MAX_FILES"), defaultMaxFiles),
		CheckInterval: defaultCheckInterval,
	}
}

// StartLogRotation creates the log directory, rotates/cleans stale files on
// startup, and starts a background goroutine that re-checks rotation every
// configured interval (default 60 s).  It is safe to call multiple times; each
// call stops any previously started rotation timer.
func StartLogRotation(cfg RotationConfig) {
	rotationMu.Lock()
	defer rotationMu.Unlock()

	stopLogRotationLocked()
	rotationConfig = cfg

	if cfg.LogFilePath == "" || (cfg.MaxSizeBytes <= 0 && cfg.RetentionDays <= 0 && cfg.MaxFiles <= 0) {
		return
	}

	if dir := filepath.Dir(cfg.LogFilePath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Printf("WARN: logging: failed to create log directory %s: %v", dir, err)
		}
	}

	// Run cleanup immediately so stale files are pruned before the next write.
	rotateIfNeeded(cfg.LogFilePath, cfg.MaxSizeBytes)
	cleanupOldLogs(cfg.LogFilePath, cfg.RetentionDays)
	cleanupOverflowLogs(cfg.LogFilePath, cfg.MaxFiles)

	if cfg.CheckInterval <= 0 {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	rotationCancel = cancel
	go runLogRotation(ctx, cfg)
}

// StopLogRotation stops the background rotation timer.  It is idempotent.
func StopLogRotation() {
	rotationMu.Lock()
	defer rotationMu.Unlock()
	stopLogRotationLocked()
}

func stopLogRotationLocked() {
	if rotationCancel == nil {
		return
	}
	rotationCancel()
	rotationCancel = nil
}

func runLogRotation(ctx context.Context, cfg RotationConfig) {
	ticker := time.NewTicker(cfg.CheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rotateIfNeeded(cfg.LogFilePath, cfg.MaxSizeBytes)
			cleanupOldLogs(cfg.LogFilePath, cfg.RetentionDays)
			cleanupOverflowLogs(cfg.LogFilePath, cfg.MaxFiles)
		}
	}
}

// RotatingFileWriter writes log lines to a file and rotates it when it exceeds
// MaxSizeBytes.  The writer is not safe for concurrent use; callers should
// serialize accesses (the package-level logFileMu does this in TeeHandler).
type RotatingFileWriter struct {
	path          string
	file          *os.File
	size          int64
	maxSizeBytes  int64
	maxFiles      int
	retentionDays int
}

// NewRotatingFileWriter opens (or creates) path and prepares it for rotation.
func NewRotatingFileWriter(path string, maxSize int64, maxFiles, retentionDays int) (*RotatingFileWriter, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	w := &RotatingFileWriter{
		path:          path,
		file:          f,
		size:          info.Size(),
		maxSizeBytes:  maxSize,
		maxFiles:      maxFiles,
		retentionDays: retentionDays,
	}
	// Prune stale rotated files on open so stale files do not accumulate
	// when the timer has not run yet.
	cleanupOldLogs(path, retentionDays)
	cleanupOverflowLogs(path, maxFiles)
	return w, nil
}

// Write appends p to the file.  If p would push the file past MaxSizeBytes, the
// current file is rotated first.
func (w *RotatingFileWriter) Write(p []byte) (int, error) {
	if w.maxSizeBytes > 0 && w.size+int64(len(p)) > w.maxSizeBytes {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

// rotate closes the active file, renames it with a timestamp suffix, opens a
// new file, and runs retention cleanup.
func (w *RotatingFileWriter) rotate() error {
	if w.file == nil {
		return fmt.Errorf("rotating file writer is closed")
	}
	if err := w.file.Close(); err != nil {
		return err
	}
	dir := filepath.Dir(w.path)
	ext := filepath.Ext(w.path)
	base := strings.TrimSuffix(filepath.Base(w.path), ext)
	ts := time.Now().UTC().Format("2006-01-02_150405")
	rotatedPath := filepath.Join(dir, fmt.Sprintf("%s.%s%s", base, ts, ext))
	if err := os.Rename(w.path, rotatedPath); err != nil {
		// Reopen the original file so writes can continue even if rotation failed.
		f, openErr := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if openErr == nil {
			w.file = f
			if info, statErr := f.Stat(); statErr == nil {
				w.size = info.Size()
			}
		}
		return err
	}

	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	w.file = f
	w.size = info.Size()

	cleanupOldLogs(w.path, w.retentionDays)
	cleanupOverflowLogs(w.path, w.maxFiles)
	return nil
}

// Close flushes and closes the underlying file.
func (w *RotatingFileWriter) Close() error {
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	w.size = 0
	return err
}

// Truncate truncates the active log file to zero bytes without rotating it.
// It is used by ClearLogFile.
func (w *RotatingFileWriter) Truncate() error {
	if w.file == nil {
		return nil
	}
	if err := w.file.Truncate(0); err != nil {
		return err
	}
	w.size = 0
	return nil
}

// Path returns the active log file path.
func (w *RotatingFileWriter) Path() string { return w.path }

// rotateIfNeeded rotates path when its size exceeds maxSizeBytes.  It does not
// re-open any active writer; RotatingFileWriter handles rotation on writes.
func rotateIfNeeded(path string, maxSizeBytes int64) {
	if maxSizeBytes <= 0 || path == "" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		log.Printf("WARN: logging: failed to stat log file %s: %v", path, err)
		return
	}
	if info.Size() < maxSizeBytes {
		return
	}
	dir := filepath.Dir(path)
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(filepath.Base(path), ext)
	ts := time.Now().UTC().Format("2006-01-02_150405")
	rotatedPath := filepath.Join(dir, fmt.Sprintf("%s.%s%s", base, ts, ext))
	if err := os.Rename(path, rotatedPath); err != nil {
		log.Printf("WARN: logging: failed to rotate log file %s: %v", path, err)
		return
	}
}

// cleanupOldLogs removes rotated log files older than retentionDays.
func cleanupOldLogs(path string, retentionDays int) {
	if retentionDays <= 0 || path == "" {
		return
	}
	dir := filepath.Dir(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		log.Printf("WARN: logging: failed to read log directory %s: %v", dir, err)
		return
	}
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(filepath.Base(path), ext)
	prefix := base + "."
	cutoff := time.Now().UTC().Add(-time.Duration(retentionDays) * 24 * time.Hour)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == filepath.Base(path) || !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ext) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().UTC().Before(cutoff) {
			if err := os.Remove(filepath.Join(dir, name)); err != nil {
				log.Printf("WARN: logging: failed to remove old log file %s: %v", name, err)
			}
		}
	}
}

// cleanupOverflowLogs keeps only the newest maxFiles rotated log files.
func cleanupOverflowLogs(path string, maxFiles int) {
	if maxFiles <= 0 || path == "" {
		return
	}
	dir := filepath.Dir(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		log.Printf("WARN: logging: failed to read log directory %s: %v", dir, err)
		return
	}
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(filepath.Base(path), ext)
	prefix := base + "."
	type rotatedFile struct {
		path    string
		modTime time.Time
	}
	var rotated []rotatedFile
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == filepath.Base(path) || !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ext) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		rotated = append(rotated, rotatedFile{path: filepath.Join(dir, name), modTime: info.ModTime()})
	}
	if len(rotated) <= maxFiles {
		return
	}
	// Newest first so the most recent rotated files are kept.
	sort.Slice(rotated, func(i, j int) bool {
		return rotated[i].modTime.After(rotated[j].modTime)
	})
	for _, f := range rotated[maxFiles:] {
		if err := os.Remove(f.path); err != nil {
			log.Printf("WARN: logging: failed to remove overflow log file %s: %v", filepath.Base(f.path), err)
		}
	}
}

var byteSizePattern = regexp.MustCompile(`^(\d+)\s*([kmgt]?b?)$`)

// parseByteSize parses a string such as "50", "50MB", "1gb", or "100K" and
// returns the size in bytes.  A bare number is treated as bytes.  When raw is
// empty or invalid, fallback is returned.
func parseByteSize(raw string, fallback int64) int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback
	}
	m := byteSizePattern.FindStringSubmatch(strings.ToLower(raw))
	if m == nil {
		return fallback
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil || n < 0 {
		return fallback
	}
	switch m[2] {
	case "k", "kb":
		n *= 1024
	case "m", "mb":
		n *= 1024 * 1024
	case "g", "gb":
		n *= 1024 * 1024 * 1024
	case "t", "tb":
		n *= 1024 * 1024 * 1024 * 1024
	}
	if n < 0 {
		return fallback
	}
	return n
}

func parsePositiveInt(raw string, fallback int) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

// extractComponent removes a leading [COMPONENT] token from msg and returns the
// token plus the cleaned message.  Whitespace after the token is trimmed.
func extractComponent(msg string) (component, clean string) {
	msg = strings.TrimSpace(msg)
	if !strings.HasPrefix(msg, "[") {
		return "", msg
	}
	end := strings.Index(msg, "]")
	if end <= 0 {
		return "", msg
	}
	component = msg[1:end]
	if strings.Contains(component, " ") || strings.Contains(component, "\t") {
		return "", msg
	}
	clean = strings.TrimSpace(msg[end+1:])
	return component, clean
}
