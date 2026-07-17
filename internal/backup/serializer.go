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
	buf      bytes.Buffer
	encoder  *json.Encoder
	closed   bool
}

var ErrWriterClosed = errors.New("backup writer is closed")

func NewWriter(dest io.Writer, header Header, password string) (*Writer, error) {
	if dest == nil {
		return nil, errors.New("backup writer destination is required")
	}
	writer := &Writer{dest: dest, password: password}
	writer.encoder = json.NewEncoder(&writer.buf)
	if err := writer.encoder.Encode(header); err != nil {
		return nil, fmt.Errorf("write backup header: %w", err)
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

	payload := w.buf.Bytes()
	if w.password != "" {
		sealed, err := Encrypt(payload, w.password)
		if err != nil {
			return fmt.Errorf("encrypt backup payload: %w", err)
		}
		payload = sealed
	}
	if _, err := w.dest.Write(payload); err != nil {
		return fmt.Errorf("write backup payload: %w", err)
	}
	return nil
}
