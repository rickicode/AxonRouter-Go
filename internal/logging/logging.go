package logging

import (
	"bytes"
	"context"
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Logger is the application-wide structured logger.
// It is safe for concurrent use and reinitialization via Init/SetLogger.
var Logger = &SafeLogger{}

// SafeLogger is a concurrency-safe wrapper around *slog.Logger.
type SafeLogger struct {
	mu sync.RWMutex
	l  *slog.Logger
}

func (sl *SafeLogger) set(l *slog.Logger) {
	sl.mu.Lock()
	defer sl.mu.Unlock()
	sl.l = l
}

// Load returns the underlying logger. If no logger has been set, it returns slog.Default.
func (sl *SafeLogger) Load() *slog.Logger {
	sl.mu.RLock()
	defer sl.mu.RUnlock()
	if sl.l == nil {
		return slog.Default()
	}
	return sl.l
}

// Info logs at LevelInfo.
func (sl *SafeLogger) Info(msg string, args ...any) { sl.Load().Info(msg, args...) }

// Error logs at LevelError.
func (sl *SafeLogger) Error(msg string, args ...any) { sl.Load().Error(msg, args...) }

// Warn logs at LevelWarn.
func (sl *SafeLogger) Warn(msg string, args ...any) { sl.Load().Warn(msg, args...) }

// Debug logs at LevelDebug.
func (sl *SafeLogger) Debug(msg string, args ...any) { sl.Load().Debug(msg, args...) }

// SetLogger updates the application-wide logger.
func SetLogger(l *slog.Logger) {
	Logger.set(l)
}

// LogConfig holds environment-driven log settings.
type LogConfig struct {
	Path          string
	MaxSizeBytes  int64
	RetentionDays int
	MaxFiles      int
}

// Default log settings.
const (
	defaultLogPath          = "/tmp/axonrouter.log"
	defaultLogMaxSizeMB     = 50
	defaultLogRetentionDays = 7
	defaultLogMaxFiles      = 20
)

// LogFilePath is the active path where structured JSON logs are written.
// It is populated by Init() from AXON_LOG_FILE_PATH (default /tmp/axonrouter.log).
var LogFilePath = defaultLogPath

// LogWriter is the active rotating file writer. It is only non-nil when Init
// has been called with a non-empty log path.
var LogWriter *RotatingFileWriter

// LogBroadcaster fans out new log lines and clear events to SSE subscribers.
var LogBroadcaster = NewBroadcaster()

// logFileMu coordinates log-file truncation with concurrent writes from
// TeeHandler. All modifications to the active log file should hold this lock.
var logFileMu sync.Mutex

// logConfigFromEnv reads AXON_LOG_* environment variables and returns a
// populated LogConfig with defaults for missing values.
func logConfigFromEnv() LogConfig {
	path := strings.TrimSpace(os.Getenv("AXON_LOG_FILE_PATH"))
	if path == "" {
		// Preserve a non-default caller override (e.g. cmd/server/main.go sets
		// the path based on config.DataDir). If no override is present, fall
		// back to the built-in default.
		if LogFilePath != defaultLogPath {
			path = LogFilePath
		} else {
			path = defaultLogPath
		}
	}

	maxSizeMB := defaultLogMaxSizeMB
	if v := os.Getenv("AXON_LOG_MAX_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxSizeMB = n
		}
	}

	retentionDays := defaultLogRetentionDays
	if v := os.Getenv("AXON_LOG_RETENTION_DAYS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			retentionDays = n
		}
	}

	maxFiles := defaultLogMaxFiles
	if v := os.Getenv("AXON_LOG_MAX_FILES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			maxFiles = n
		}
	}

	return LogConfig{
		Path:          path,
		MaxSizeBytes:  int64(maxSizeMB) * 1024 * 1024,
		RetentionDays: retentionDays,
		MaxFiles:      maxFiles,
	}
}

// ClearLogFile truncates the active log file and broadcasts a clear event to
// all registered SSE subscribers. It is safe to call while TeeHandler is writing.
func ClearLogFile() error {
	logFileMu.Lock()
	defer logFileMu.Unlock()
	if LogWriter != nil {
		if err := LogWriter.Truncate(); err != nil {
			return err
		}
	} else if LogFilePath != "" {
		f, err := os.OpenFile(LogFilePath, os.O_WRONLY|os.O_CREATE, 0o644)
		if err != nil {
			return err
		}
		if err := f.Truncate(0); err != nil {
			_ = f.Close()
			return err
		}
		_ = f.Close()
	}
	LogBroadcaster.BroadcastClear()
	return nil
}

// rotationTickerCancel stops the background rotation/cleanup ticker.
var rotationTickerCancel context.CancelFunc
var rotationTickerMu sync.Mutex

// startRotationTicker starts a background goroutine that periodically checks
// whether the active log file exceeds its size limit and applies retention
// cleanup. This is hooked into Init() and into runtime reconfiguration.
func startRotationTicker(cfg LogConfig) {
	rotationTickerMu.Lock()
	defer rotationTickerMu.Unlock()

	if rotationTickerCancel != nil {
		rotationTickerCancel()
		rotationTickerCancel = nil
	}

	if cfg.MaxSizeBytes <= 0 && cfg.RetentionDays <= 0 {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	rotationTickerCancel = cancel

	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if LogWriter == nil {
					continue
				}
				if cfg.RetentionDays > 0 {
					_ = LogWriter.CleanupRetention()
				}
				if cfg.MaxSizeBytes > 0 {
					logFileMu.Lock()
					stat, err := os.Stat(cfg.Path)
					needsRotate := err == nil && stat.Size() >= cfg.MaxSizeBytes
					logFileMu.Unlock()
					if needsRotate {
						_ = LogWriter.Rotate()
					}
				}
			}
		}
	}()
}

// stopRotationTicker stops the background rotation ticker.
func stopRotationTicker() {
	rotationTickerMu.Lock()
	defer rotationTickerMu.Unlock()
	if rotationTickerCancel != nil {
		rotationTickerCancel()
		rotationTickerCancel = nil
	}
}

// Init initialises the global logger. format must be "json", "text", or "compact" (default).
// It reads AXON_LOG_* environment variables, configures size-based rotation, and
// optionally creates the log directory and rotating file writer.
func Init(format string) {
	// Close any previous rotating writer before replacing it.
	if LogWriter != nil {
		_ = LogWriter.Close()
		LogWriter = nil
	}
	stopRotationTicker()

	cfg := logConfigFromEnv()
	LogFilePath = cfg.Path

	var baseHandler slog.Handler
	switch format {
	case "json":
		baseHandler = slog.NewJSONHandler(os.Stdout, nil)
	case "text":
		textOpts := &slog.HandlerOptions{
			ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
				if a.Key == slog.TimeKey {
					return slog.String(slog.TimeKey, a.Value.Time().In(time.Local).Format("2006-01-02 15:04:05"))
				}
				if colorEnabled() {
					if c := colorForKey(a.Key); c != reset {
						a.Key = c + a.Key + reset
					}
				}
				return a
			},
		}
		baseHandler = slog.NewTextHandler(&ansiWriter{w: os.Stdout}, textOpts)
	default:
		h := NewCompactHandler(os.Stdout)
		baseHandler = &levelHandler{inner: h, level: slog.LevelDebug}
	}

	var l *slog.Logger
	if LogFilePath != "" {
		if err := os.MkdirAll(filepath.Dir(LogFilePath), 0o755); err != nil {
			log.Printf("WARN: failed to create log directory for %s: %v (console log viewer will be empty)", LogFilePath, err)
			l = slog.New(baseHandler)
		} else {
			var err error
			LogWriter, err = NewRotatingFileWriter(LogFilePath, cfg.MaxSizeBytes, cfg.RetentionDays, cfg.MaxFiles)
			if err != nil {
				log.Printf("WARN: failed to open log file %s: %v (console log viewer will be empty)", LogFilePath, err)
				l = slog.New(baseHandler)
			} else {
				tee, err := NewTeeHandler(baseHandler, LogWriter)
				if err != nil {
					log.Printf("WARN: failed to create tee handler for %s: %v (console log viewer will be empty)", LogFilePath, err)
					l = slog.New(baseHandler)
				} else {
					l = slog.New(tee)
					startRotationTicker(cfg)
				}
			}
		}
	} else {
		l = slog.New(baseHandler)
	}

	Logger.set(l)
	slog.SetDefault(l)
	log.SetOutput(&slogWriter{l: l})
}

// slogWriter redirects standard log calls to slog.
type slogWriter struct {
	l *slog.Logger
}

func (w *slogWriter) Write(p []byte) (n int, err error) {
	// log.Printf appends a trailing newline; strip it before forwarding to slog
	// so the underlying handler doesn't emit a blank line after its own newline.
	msg := strings.TrimSuffix(string(p), "\n")
	w.l.Info(msg)
	return len(p), nil
}

// levelHandler wraps a handler with a minimum level filter.
type levelHandler struct {
	inner slog.Handler
	level slog.Level
}

func (h *levelHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *levelHandler) Handle(ctx context.Context, r slog.Record) error {
	return h.inner.Handle(ctx, r)
}

func (h *levelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &levelHandler{inner: h.inner.WithAttrs(attrs), level: h.level}
}

func (h *levelHandler) WithGroup(name string) slog.Handler {
	return &levelHandler{inner: h.inner.WithGroup(name), level: h.level}
}

type ansiWriter struct {
	w io.Writer
}

func (aw *ansiWriter) Write(p []byte) (int, error) {
	if _, err := aw.w.Write(unescapeANSICodes(p)); err != nil {
		return 0, err
	}
	return len(p), nil
}

func unescapeANSICodes(p []byte) []byte {
	const prefix = "\\x1b["
	out := make([]byte, 0, len(p))
	for i := 0; i < len(p); {
		j := bytes.Index(p[i:], []byte(prefix))
		if j < 0 {
			out = append(out, p[i:]...)
			break
		}
		out = append(out, p[i:i+j]...)
		i += j + len(prefix)
		start := i
		for i < len(p) && p[i] != 'm' {
			i++
		}
		if i < len(p) {
			seq := append([]byte{0x1b, '['}, p[start:i]...)
			seq = append(seq, 'm')
			out = append(out, seq...)
			i++
		} else {
			out = append(out, []byte(prefix)...)
			out = append(out, p[start:]...)
			break
		}
	}
	return out
}
