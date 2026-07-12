package db

import (
	"context"
	"database/sql"
	"log"
	"sync/atomic"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/logging"
)

// WriteOp is a function that performs a single DB write.
// The queue executes these one at a time, which is exactly what SQLite wants
// (only one writer at a time regardless of WAL mode). By funnelling all
// non-critical writes through the queue, the request path never blocks on
// a DB write lock.
type WriteOp struct {
	fn      func(*sql.DB) error
	label   string // for logging on failure
}

// WriteQueue is a centralized, channel-based async writer.
// It removes ALL synchronous DB writes from the request path.
//
// Architecture:
//   - Producers (request handlers) call Enqueue() — non-blocking, drops on full.
//   - A single goroutine drains the channel and executes writes serially.
//   - Since it is the only writer, there is never write-lock contention.
//   - Readers (auth, health, connection loads) use the pool freely.
type WriteQueue struct {
	ch     chan WriteOp
	db     *sql.DB
	stop   chan struct{}
	done   chan struct{}
	dropped atomic.Int64
}

// NewWriteQueue creates a write queue backed by the given DB.
// Channel capacity is generous (5000) — writes are tiny (single-row UPDATEs)
// and the consumer is a tight loop with no I/O wait between writes.
func NewWriteQueue(database *sql.DB) *WriteQueue {
	wq := &WriteQueue{
		ch:   make(chan WriteOp, 5000),
		db:   database,
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
	go wq.loop()
	return wq
}

// Enqueue submits a write operation for async execution.
// Non-blocking: if the buffer is full the write is dropped (best-effort for
// cooldown/ban persistence — the in-memory state is already updated synchronously).
func (wq *WriteQueue) Enqueue(label string, fn func(*sql.DB) error) {
	if wq == nil || wq.db == nil {
		return // no write queue configured — in-memory state is already updated
	}
	op := WriteOp{fn: fn, label: label}
	select {
	case wq.ch <- op:
	default:
		wq.dropped.Add(1)
		logging.Logger.Warn("writequeue: buffer full, dropped write", "op", label)
	}
}
func (wq *WriteQueue) EnqueueOrBlock(ctx context.Context, label string, fn func(*sql.DB) error) {
	if wq == nil || wq.db == nil {
		return // no write queue configured
	}
	select {
	case wq.ch <- WriteOp{fn: fn, label: label}:
	case <-ctx.Done():
	}
}

// loop runs in a goroutine, draining the write channel.
func (wq *WriteQueue) loop() {
	defer close(wq.done)
	for {
		select {
		case op := <-wq.ch:
			if err := op.fn(wq.db); err != nil {
				logging.Logger.Error("writequeue: write failed",
					"op", op.label, "error", err.Error())
			}
		case <-wq.stop:
			// Drain remaining writes before exiting.
			for {
				select {
				case op := <-wq.ch:
					if err := op.fn(wq.db); err != nil {
						log.Printf("writequeue: write failed during shutdown: op=%s err=%v", op.label, err)
					}
				default:
					return
				}
			}
		}
	}
}

// Stop gracefully shuts down the queue, flushing all pending writes.
// Blocks until all buffered writes have been processed.
func (wq *WriteQueue) Stop() {
	close(wq.stop)
	<-wq.done
}

// Dropped returns the total number of writes dropped because the buffer was full.
func (wq *WriteQueue) Dropped() int64 {
	return wq.dropped.Load()
}

// Buffered returns the number of writes waiting in the buffer.
func (wq *WriteQueue) Buffered() int {
	return len(wq.ch)
}

// FlushIdle waits for the write queue to drain or times out.
// Useful for tests and graceful shutdown.
func (wq *WriteQueue) FlushIdle(timeout time.Duration) {
	deadline := time.After(timeout)
	for {
		if wq.Buffered() == 0 {
			return
		}
		select {
		case <-deadline:
			return
		case <-time.After(10 * time.Millisecond):
		}
	}
}
