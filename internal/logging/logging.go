package logging

import (
	"context"
	"log"
	"log/slog"
	"os"
	"strings"
)

// Logger is the application-wide structured logger.
var Logger *slog.Logger

// Init initialises the global logger. format must be "json", "text", or "compact" (default).
func Init(format string) {
	switch format {
	case "json":
		Logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	case "text":
		Logger = slog.New(slog.NewTextHandler(os.Stdout, nil))
	default:
		h := NewCompactHandler(os.Stdout)
		Logger = slog.New(&levelHandler{inner: h, level: slog.LevelDebug})
	}
	// Redirect standard log to slog so log.Printf also uses compact format
	slog.SetDefault(Logger)
	log.SetOutput(&slogWriter{l: Logger})
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
