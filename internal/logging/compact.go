package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"
)

const (
	reset   = "\033[0m"
	bold    = "\033[1m"
	dim     = "\033[2m"
	red     = "\033[31m"
	green   = "\033[32m"
	yellow  = "\033[33m"
	blue    = "\033[34m"
	magenta = "\033[35m"
	cyan    = "\033[36m"
	white   = "\033[37m"
)

// CompactHandler is a slog.Handler that produces human-readable one-line logs.
// Format: [HH:MM:SS] LEVEL msg key=value key=value
type CompactHandler struct {
	w io.Writer
}

func NewCompactHandler(w io.Writer) *CompactHandler {
	return &CompactHandler{w: w}
}

func (h *CompactHandler) Enabled(_ context.Context, _ slog.Level) bool {
	return true
}

func colorEnabled() bool {
	return os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
}

var logKeyColors = map[string]string{
	"provider":   cyan,
	"conn":       dim,
	"name":       magenta + bold,
	"host":       cyan,
	"request_id": yellow,
	"client_ip":  blue,
	"user_agent": green,
	"status":     magenta,
	"method":     green,
	"proxy":      yellow,
	"path":       cyan,
	"lat":        white,
	"error":      red,
	"body":       white,
	"model":      yellow,
	"account_id": blue,
}

func colorForKey(key string) string {
	if c, ok := logKeyColors[key]; ok {
		return c
	}
	return reset
}

func (h *CompactHandler) Handle(_ context.Context, r slog.Record) error {
	ts := r.Time.In(time.Local).Format("2006-01-02 15:04:05")
	level := r.Level.String()
	if level == "WARNING" {
		level = "WARN"
	}

	color := colorEnabled()
	var sb strings.Builder
	if !color {
		sb.WriteString(fmt.Sprintf("[%s] %-5s %s", ts, level, r.Message))
		r.Attrs(func(a slog.Attr) bool {
			sb.WriteString(fmt.Sprintf(" %s=%v", a.Key, a.Value))
			return true
		})
		sb.WriteByte('\n')
		_, err := h.w.Write([]byte(sb.String()))
		return err
	}

	levelColor := reset
	switch r.Level {
	case slog.LevelDebug:
		levelColor = cyan
	case slog.LevelInfo:
		levelColor = green
	case slog.LevelWarn:
		levelColor = yellow
	case slog.LevelError:
		levelColor = red
	}

	sb.WriteString(fmt.Sprintf("%s[%s]%s %s%-5s%s %s",
		dim, ts, reset, levelColor, level, reset, r.Message))

	r.Attrs(func(a slog.Attr) bool {
		keyColor := colorForKey(a.Key)
		if keyColor == reset {
			keyColor = dim
		}
		sb.WriteString(fmt.Sprintf(" %s%s%s=%v", keyColor, a.Key, reset, a.Value))
		return true
	})

	sb.WriteByte('\n')
	_, err := h.w.Write([]byte(sb.String()))
	return err
}

func (h *CompactHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	// ponytail: compact handler doesn't carry group attrs, acceptable for single-process logger
	return h
}

func (h *CompactHandler) WithGroup(name string) slog.Handler {
	return h
}
