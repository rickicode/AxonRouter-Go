package logging

import (
	"bytes"
	"context"
	"io"
	"log"
	"log/slog"
	"os"
	"strings"
	"time"
)

// Logger is the application-wide structured logger.
var Logger *slog.Logger

// Init initialises the global logger. format must be "json", "text", or "compact" (default).
func Init(format string) {
	switch format {
	case "json":
		Logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
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
		Logger = slog.New(slog.NewTextHandler(&ansiWriter{w: os.Stdout}, textOpts))

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
