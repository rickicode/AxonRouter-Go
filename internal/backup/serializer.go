package backup

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

type Writer struct {
	dest     io.Writer
	password string
	// buf holds the full serialized backup when encryption is enabled,
	// because AES-GCM requires the entire plaintext to produce a single
	// authenticated ciphertext. For plaintext backups we write directly to
	// dest so backups of large tables (e.g. request_logs) stream without
	// unbounded memory growth.
	buf     bytes.Buffer
	encoder *json.Encoder
	closed  bool
}

var ErrWriterClosed = errors.New("backup writer is closed")

func NewWriter(dest io.Writer, header Header, password string) (*Writer, error) {
	if dest == nil {
		return nil, errors.New("backup writer destination is required")
	}
	writer := &Writer{dest: dest, password: password}
	if password == "" {
		// Stream plaintext rows directly to dest for O(1) memory usage.
		writer.encoder = json.NewEncoder(dest)
		if err := writer.encoder.Encode(header); err != nil {
			return nil, fmt.Errorf("write backup header: %w", err)
		}
	} else {
		// Encrypted backups must be buffered in memory for AES-GCM auth tag.
		writer.encoder = json.NewEncoder(&writer.buf)
		if err := writer.encoder.Encode(header); err != nil {
			return nil, fmt.Errorf("write backup header: %w", err)
		}
	}
	return writer, nil
}

func (w *Writer) WriteRow(row Row) error {
	if w == nil || w.closed {
		return ErrWriterClosed
	}
	if err := w.encoder.Encode(row); err != nil {
		return fmt.Errorf("write backup row: %w", err)
	}
	return nil
}

func (w *Writer) Close() error {
	if w == nil || w.closed {
		return ErrWriterClosed
	}
	w.closed = true

	if w.password == "" {
		// Plaintext rows have already been streamed to dest.
		return nil
	}

	payload := w.buf.Bytes()
	sealed, err := Encrypt(payload, w.password)
	if err != nil {
		return fmt.Errorf("encrypt backup payload: %w", err)
	}
	if _, err := w.dest.Write(sealed); err != nil {
		return fmt.Errorf("write backup payload: %w", err)
	}
	return nil
}
