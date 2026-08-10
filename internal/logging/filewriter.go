package logging

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"time"
)

// TeeHandler wraps an existing slog.Handler and additionally writes structured
// JSON lines to a log file. This allows the Console Log Viewer in the dashboard
// to display rich, filterable log entries instead of raw text.
type TeeHandler struct {
	inner  slog.Handler
	writer *RotatingFileWriter
}

// NewTeeHandler creates a TeeHandler that delegates to inner and also appends
// structured JSON lines via writer.
func NewTeeHandler(inner slog.Handler, writer *RotatingFileWriter) (*TeeHandler, error) {
	return &TeeHandler{inner: inner, writer: writer}, nil
}

// Enabled reports true if either the inner handler or the file writer accepts the level.
func (h *TeeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

// Handle writes the record to the inner handler AND appends a JSON line to the file.
func (h *TeeHandler) Handle(ctx context.Context, r slog.Record) error {
	// Extract an inline [COMPONENT] prefix from the message.  If the log also
	// carries an explicit "component" attribute, that attribute wins.
	component, message := extractComponent(r.Message)

	// Clone the record with the cleaned message so the compact/text handler does
	// not emit the redundant [COMPONENT] token.
	clone := slog.NewRecord(r.Time, r.Level, message, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		clone.AddAttrs(a)
		return true
	})
	if component != "" && !recordHasAttr(clone, "component") {
		clone.AddAttrs(slog.String("component", component))
	}

	// Always delegate to the inner handler (compact text -> stdout).
	if err := h.inner.Handle(ctx, clone); err != nil {
		return err
	}

	// Build a structured JSON entry for the log file.
	entry := jsonLogEntry{
		Timestamp: r.Time.UTC().Format(time.RFC3339Nano),
		Level:     normalizeLevel(r.Level),
		Message:   message,
	}
	clone.Attrs(func(a slog.Attr) bool {
		switch a.Key {
		case "component":
			entry.Component = a.Value.String()
		case "request_id", "cid":
			entry.RequestID = a.Value.String()
		case "provider":
			entry.Provider = a.Value.String()
		case "model":
			entry.Model = a.Value.String()
		case "conn", "connection":
			entry.Connection = a.Value.String()
		case "error":
			entry.Error = a.Value.String()
		default:
			if entry.Extra == nil {
				entry.Extra = make(map[string]any)
			}
			entry.Extra[a.Key] = a.Value.Any()
		}
		return true
	})
	if entry.Component == "" && component != "" {
		entry.Component = component
	}

	line, err := json.Marshal(entry)
	if err != nil {
		return nil // swallow — don't break the app over a log-file write
	}
	line = append(line, '\n')

	logFileMu.Lock()
	defer logFileMu.Unlock()
	_, writeErr := h.writer.Write(line)
	if writeErr == nil {
		LogBroadcaster.BroadcastLine(string(line))
	}
	return writeErr
}

// recordHasAttr reports whether a slog.Record contains an attribute with the
// given key.
func recordHasAttr(r slog.Record, key string) bool {
	found := false
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == key {
			found = true
			return false
		}
		return true
	})
	return found
}

// WithAttrs delegates to the inner handler (attrs are captured at Handle time via r.Attrs).
func (h *TeeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &TeeHandler{inner: h.inner.WithAttrs(attrs), writer: h.writer}
}

// WithGroup delegates to the inner handler.
func (h *TeeHandler) WithGroup(name string) slog.Handler {
	return &TeeHandler{inner: h.inner.WithGroup(name), writer: h.writer}
}

// Close closes the underlying log file.
func (h *TeeHandler) Close() error {
	if h.writer != nil {
		return h.writer.Close()
	}
	return nil
}

// jsonLogEntry is the structured shape written to the log file.
type jsonLogEntry struct {
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

// normalizeLevel maps slog.Level to a short lowercase string.
func normalizeLevel(l slog.Level) string {
	switch {
	case l <= slog.LevelDebug:
		return "debug"
	case l <= slog.LevelInfo:
		return "info"
	case l <= slog.LevelWarn:
		return "warn"
	default:
		return "error"
	}
}

// StripANSI removes ANSI escape sequences from a byte slice.
func StripANSI(p []byte) []byte {
	var out strings.Builder
	out.Grow(len(p))
	i := 0
	for i < len(p) {
		if p[i] == 0x1b && i+1 < len(p) && p[i+1] == '[' {
			// Skip until 'm'
			j := i + 2
			for j < len(p) && p[j] != 'm' {
				j++
			}
			if j < len(p) {
				i = j + 1
				continue
			}
		}
		out.WriteByte(p[i])
		i++
	}
	return []byte(out.String())
}

// Compile-time check that TeeHandler implements slog.Handler.
var _ slog.Handler = (*TeeHandler)(nil)

// Ensure TeeHandler also implements io.Closer (optional, for shutdown).
var _ io.Closer = (*TeeHandler)(nil)
