package logging

import (
	"bytes"
	"context"
	"io"
	"log"
	"log/slog"
	"os"
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

// LogFilePath is the default path where structured JSON logs are written.
var LogFilePath = "/tmp/axonrouter.log"

// Init initialises the global logger. format must be "json", "text", or "compact" (default).
// If LogFilePath is non-empty, a TeeHandler is installed that also writes structured
// JSON lines to that file (for the dashboard Console Log Viewer).
func Init(format string) {
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
		tee, err := NewTeeHandler(baseHandler, LogFilePath)
		if err != nil {
			log.Printf("WARN: failed to open log file %s: %v (console log viewer will be empty)", LogFilePath, err)
			l = slog.New(baseHandler)
		} else {
			l = slog.New(tee)
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
