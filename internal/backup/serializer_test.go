package backup

import (
	"bufio"
	"bytes"
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
		Categories: []string{"core", "logs"},
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

func TestWriterEncryptsNDJSONWhenPasswordProvided(t *testing.T) {
	var buf bytes.Buffer
	header := Header{Format: FormatName, Version: FormatVersion, Categories: []string{"core"}, CreatedAt: 1700000000}
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

	if bytes.Contains(buf.Bytes(), []byte("settings")) || bytes.Contains(buf.Bytes(), []byte("api_url")) {
		t.Fatalf("encrypted backup contains plaintext row data: %q", buf.Bytes())
	}

	plaintext, err := Decrypt(buf.Bytes(), password)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	gotHeader, gotRows := decodeBackupNDJSON(t, plaintext)
	if !reflect.DeepEqual(gotHeader, header) {
		t.Fatalf("header = %#v, want %#v", gotHeader, header)
	}
	if !reflect.DeepEqual(gotRows, []Row{row}) {
		t.Fatalf("rows = %#v, want %#v", gotRows, []Row{row})
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
