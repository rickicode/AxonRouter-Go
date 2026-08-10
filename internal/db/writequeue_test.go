package db

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/logging"

	_ "modernc.org/sqlite"
)

func TestMain(m *testing.M) {
	logging.SetLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))
	os.Exit(m.Run())
}

func TestWriteQueuePauseBlocksEnqueueUntilResume(t *testing.T) {
	dir := t.TempDir()
	d, err := sql.Open("sqlite", filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer d.Close()

	wq := NewWriteQueue(d)
	defer func() {
		wq.Resume()
		wq.Stop()
	}()
	wq.Pause()

	enqueued := make(chan struct{})
	go func() {
		wq.Enqueue("paused", func(db *sql.DB) error {
			return nil
		})
		close(enqueued)
	}()

	select {
	case <-enqueued:
		t.Fatal("Enqueue returned while queue was paused")
	case <-time.After(50 * time.Millisecond):
	}

	wq.Resume()
	select {
	case <-enqueued:
	case <-time.After(time.Second):
		t.Fatal("Enqueue did not return after Resume")
	}
}

func TestWriteQueuePauseStopsWorkerProcessingQueuedWrites(t *testing.T) {
	dir := t.TempDir()
	d, err := sql.Open("sqlite", filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer d.Close()

	wq := NewWriteQueue(d)
	defer func() {
		wq.Resume()
		wq.Stop()
	}()

	blockWorker := make(chan struct{})
	workerBlocked := make(chan struct{})
	wq.EnqueueOrBlock(context.Background(), "block-worker", func(db *sql.DB) error {
		close(workerBlocked)
		<-blockWorker
		return nil
	})
	select {
	case <-workerBlocked:
	case <-time.After(time.Second):
		t.Fatal("worker did not start blocking write")
	}

	wq.Pause()
	var ran atomic.Bool
	enqueued := make(chan struct{})
	go func() {
		wq.EnqueueOrBlock(context.Background(), "paused", func(db *sql.DB) error {
			ran.Store(true)
			return nil
		})
		close(enqueued)
	}()

	select {
	case <-enqueued:
		t.Fatal("EnqueueOrBlock returned while queue was paused")
	case <-time.After(50 * time.Millisecond):
	}
	close(blockWorker)

	time.Sleep(50 * time.Millisecond)
	if ran.Load() {
		t.Fatal("write ran while queue was paused")
	}

	wq.Resume()
	select {
	case <-enqueued:
	case <-time.After(time.Second):
		t.Fatal("EnqueueOrBlock did not return after Resume")
	}
	deadline := time.After(time.Second)
	for !ran.Load() {
		select {
		case <-deadline:
			t.Fatal("write did not run after Resume")
		case <-time.After(10 * time.Millisecond):
		}
	}
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

// TestWriteQueueStopIsIdempotent verifies Stop can be called more than once.
func TestWriteQueueStopIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	d, err := sql.Open("sqlite", filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer d.Close()

	wq := NewWriteQueue(d)
	wq.Stop()
	wq.Stop()
}
