package backup

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

type Writer struct {
	dest      io.Writer
	password  string
	encrypted bool
	encoder   *json.Encoder
	rowBuf    bytes.Buffer
	closed    bool
}

var ErrWriterClosed = errors.New("backup writer is closed")

func NewWriter(dest io.Writer, header Header, password string) (*Writer, error) {
	if dest == nil {
		return nil, errors.New("backup writer destination is required")
	}

	writer := &Writer{
		dest:      dest,
		password:  password,
		encrypted: password != "",
	}

	// Header is always stored as plaintext JSON so restore can read it without
	// a password and decide whether subsequent rows are encrypted.
	header.Encrypted = writer.encrypted
	writer.encoder = json.NewEncoder(dest)
	if err := writer.encoder.Encode(header); err != nil {
		return nil, fmt.Errorf("write backup header: %w", err)
	}

	if writer.encrypted {
		// We still use an encoder for the rare case an error happens while
		// serializing a row into a temporary buffer, but the real output is
		// written line-by-line as base64 ciphertext.
		writer.encoder = json.NewEncoder(&writer.rowBuf)
	}

	return writer, nil
}

func (w *Writer) WriteRow(row Row) error {
	if w == nil || w.closed {
		return ErrWriterClosed
	}

	if !w.encrypted {
		return w.encoder.Encode(row)
	}

	w.rowBuf.Reset()
	if err := w.encoder.Encode(row); err != nil {
		return fmt.Errorf("serialize backup row: %w", err)
	}

	line, err := EncryptLine(w.rowBuf.Bytes(), w.password)
	if err != nil {
		return fmt.Errorf("encrypt backup row: %w", err)
	}

	if _, err := io.WriteString(w.dest, line+"\n"); err != nil {
		return fmt.Errorf("write encrypted backup row: %w", err)
	}
	return nil
}

func (w *Writer) Close() error {
	if w == nil || w.closed {
		return ErrWriterClosed
	}
	w.closed = true
	return nil
}
