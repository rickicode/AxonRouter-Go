package db

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/rickicode/AxonRouter-Go/internal/logging"

	_ "modernc.org/sqlite"
)

func TestMain(m *testing.M) {
	logging.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	os.Exit(m.Run())
}

// TestWriteQueueDo executes a write synchronously through the queue.
func TestWriteQueueDo(t *testing.T) {
	dir := t.TempDir()
	d, err := sql.Open("sqlite", filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer d.Close()

	if _, err := d.Exec("CREATE TABLE seq (n INTEGER)"); err != nil {
		t.Fatalf("create table: %v", err)
	}

	wq := NewWriteQueue(d)
	defer wq.Stop()

	if err := wq.Do(context.Background(), "insert", func(db *sql.DB) error {
		_, err := db.Exec("INSERT INTO seq (n) VALUES (123)")
		return err
	}); err != nil {
		t.Fatalf("Do returned error: %v", err)
	}

	var n int
	if err := d.QueryRow("SELECT n FROM seq").Scan(&n); err != nil {
		t.Fatalf("read row: %v", err)
	}
	if n != 123 {
		t.Fatalf("expected 123, got %d", n)
	}
}

// TestWriteQueueDoError verifies Do propagates the operation's error.
func TestWriteQueueDoError(t *testing.T) {
	dir := t.TempDir()
	d, err := sql.Open("sqlite", filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer d.Close()

	wq := NewWriteQueue(d)
	defer wq.Stop()

	want := errors.New("boom")
	err = wq.Do(context.Background(), "fail", func(db *sql.DB) error {
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("expected %v, got %v", want, err)
	}
}

// TestWriteQueueDoNil verifies Do returns nil when no queue is configured.
func TestWriteQueueDoNil(t *testing.T) {
	var wq *WriteQueue
	called := false
	if err := wq.Do(context.Background(), "noop", func(db *sql.DB) error {
		called = true
		return nil
	}); err != nil {
		t.Fatalf("expected nil for nil queue, got %v", err)
	}
	if called {
		t.Fatal("expected fn not to be called on nil queue")
	}
}
