package backup

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"reflect"
	"testing"
)

func TestWriterWritesHeaderAndRowsAsNDJSON(t *testing.T) {
	var buf bytes.Buffer
	header := Header{
		Format:     FormatName,
		Version:    FormatVersion,
		Categories: []string{"providers", "usage"},
		CreatedAt:  1700000000,
	}
	rows := []Row{
		{Table: "settings", Data: map[string]any{"key": "theme", "value": "dark"}},
		{Table: "request_logs", Data: map[string]any{"id": float64(42), "ok": true}},
	}

	writer, err := NewWriter(&buf, header, "")
	if err != nil {
		t.Fatalf("NewWriter() error = %v", err)
	}
	for _, row := range rows {
		if err := writer.WriteRow(row); err != nil {
			t.Fatalf("WriteRow() error = %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	gotHeader, gotRows := decodeBackupNDJSON(t, buf.Bytes())
	if !reflect.DeepEqual(gotHeader, header) {
		t.Fatalf("header = %#v, want %#v", gotHeader, header)
	}
	if !reflect.DeepEqual(gotRows, rows) {
		t.Fatalf("rows = %#v, want %#v", gotRows, rows)
	}
}

func TestWriterEncryptsRowsWhenPasswordProvided(t *testing.T) {
	var buf bytes.Buffer
	header := Header{Format: FormatName, Version: FormatVersion, Categories: []string{"providers"}, CreatedAt: 1700000000}
	row := Row{Table: "settings", Data: map[string]any{"key": "api_url", "value": "https://example.test"}}
	password := "correct horse battery staple"

	writer, err := NewWriter(&buf, header, password)
	if err != nil {
		t.Fatalf("NewWriter() error = %v", err)
	}
	if err := writer.WriteRow(row); err != nil {
		t.Fatalf("WriteRow() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	lines := splitLines(t, buf.Bytes())
	if len(lines) != 2 {
		t.Fatalf("expected header + 1 row, got %d lines", len(lines))
	}

	var gotHeader Header
	if err := json.Unmarshal([]byte(lines[0]), &gotHeader); err != nil {
		t.Fatalf("decode header: %v", err)
	}
	if !gotHeader.Encrypted {
		t.Fatal("expected header.Encrypted = true")
	}

	if bytes.Contains([]byte(lines[1]), []byte("settings")) || bytes.Contains([]byte(lines[1]), []byte("api_url")) {
		t.Fatalf("encrypted backup row contains plaintext data: %q", lines[1])
	}

	plaintext, err := DecryptLine(lines[1], password)
	if err != nil {
		t.Fatalf("DecryptLine() error = %v", err)
	}
	var gotRow Row
	if err := json.Unmarshal(plaintext, &gotRow); err != nil {
		t.Fatalf("decode decrypted row: %v", err)
	}
	if !reflect.DeepEqual(gotRow, row) {
		t.Fatalf("row = %#v, want %#v", gotRow, row)
	}

	// Encrypted row must be valid base64.
	if _, err := base64.StdEncoding.DecodeString(lines[1]); err != nil {
		t.Fatalf("encrypted row is not valid base64: %v", err)
	}
}

func TestWriterRejectsRowsAfterClose(t *testing.T) {
	var buf bytes.Buffer
	writer, err := NewWriter(&buf, Header{Format: FormatName, Version: FormatVersion}, "")
	if err != nil {
		t.Fatalf("NewWriter() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := writer.WriteRow(Row{Table: "settings", Data: map[string]any{"key": "value"}}); err == nil {
		t.Fatalf("WriteRow() after Close() error = nil, want error")
	}
}

func decodeBackupNDJSON(t *testing.T, data []byte) (Header, []Row) {
	t.Helper()

	scanner := bufio.NewScanner(bytes.NewReader(data))
	if !scanner.Scan() {
		t.Fatalf("missing header line")
	}
	var header Header
	if err := json.Unmarshal(scanner.Bytes(), &header); err != nil {
		t.Fatalf("decode header: %v", err)
	}

	var rows []Row
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var row Row
		if err := json.Unmarshal(line, &row); err != nil {
			t.Fatalf("decode row: %v", err)
		}
		rows = append(rows, row)
	}
	if err := scanner.Err(); err != nil && err != io.EOF {
		t.Fatalf("scan backup: %v", err)
	}
	return header, rows
}

func splitLines(t *testing.T, data []byte) []string {
	t.Helper()
	scanner := bufio.NewScanner(bytes.NewReader(data))
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan lines: %v", err)
	}
	return lines
}
