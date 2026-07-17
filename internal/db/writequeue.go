package db

import (
	"context"
	"database/sql"
	"log"
	"sync"
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
	fn    func(*sql.DB) error
	label string // for logging on failure
}

// WriteQueue is a centralized, channel-based async writer.
// It removes ALL synchronous DB writes from the request path.
//
// Architecture:
// - Producers (request handlers) call Enqueue() — non-blocking unless paused, drops on full.
//   - A single goroutine drains the channel and executes writes serially.
//   - Since it is the only writer, there is never write-lock contention.
//   - Readers (auth, health, connection loads) use the pool freely.
type WriteQueue struct {
	ch      chan WriteOp
	db      *sql.DB
	stop    chan struct{}
	done    chan struct{}
	pauseMu sync.Mutex
	pause   *sync.Cond
	paused  bool
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
	wq.pause = sync.NewCond(&wq.pauseMu)
	go wq.loop()
	return wq
}

func (wq *WriteQueue) waitUntilResumed() {
	wq.pauseMu.Lock()
	for wq.paused {
		wq.pause.Wait()
	}
	wq.pauseMu.Unlock()
}

func (wq *WriteQueue) waitUntilResumedOrStopped() bool {
	wq.pauseMu.Lock()
	for wq.paused {
		select {
		case <-wq.stop:
			wq.pauseMu.Unlock()
			return false
		default:
		}
		wq.pause.Wait()
	}
	wq.pauseMu.Unlock()
	return true
}

// Pause prevents the worker from processing writes and makes Enqueue block.
func (wq *WriteQueue) Pause() {
	if wq == nil || wq.db == nil {
		return
	}
	wq.pauseMu.Lock()
	wq.paused = true
	wq.pauseMu.Unlock()
}

// Resume releases paused Enqueue calls and lets the worker process writes again.
func (wq *WriteQueue) Resume() {
	if wq == nil || wq.db == nil {
		return
	}
	wq.pauseMu.Lock()
	wq.paused = false
	wq.pause.Broadcast()
	wq.pauseMu.Unlock()
}

// Enqueue submits a write operation for async execution.
// Blocks while the queue is paused. Otherwise, if the buffer is full the write is dropped
// (best-effort for cooldown/ban persistence — the in-memory state is already updated synchronously).
func (wq *WriteQueue) Enqueue(label string, fn func(*sql.DB) error) {
	if wq == nil || wq.db == nil {
		return // no write queue configured — in-memory state is already updated
	}
	wq.waitUntilResumed()
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
	wq.waitUntilResumed()
	select {
	case wq.ch <- WriteOp{fn: fn, label: label}:
	case <-ctx.Done():
	}
}

// Do submits a write operation and BLOCKS until the single-writer goroutine
// executes it, returning the operation's error. Unlike Enqueue/EnqueueOrBlock
// (which only block until the op is buffered), Do waits for the result so
// callers can read written state and know whether the write succeeded.
func (wq *WriteQueue) Do(ctx context.Context, label string, fn func(*sql.DB) error) error {
	if wq == nil || wq.db == nil {
		return nil // no write queue configured — caller falls back to a direct tx
	}
	doneCh := make(chan error, 1)
	op := WriteOp{
		fn: func(d *sql.DB) error {
			err := fn(d)
			doneCh <- err
			return err
		},
		label: label,
	}
	select {
	case wq.ch <- op:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-doneCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// loop runs in a goroutine, draining the write channel.
func (wq *WriteQueue) loop() {
	defer close(wq.done)
	for {
		select {
		case op := <-wq.ch:
			if !wq.waitUntilResumedOrStopped() {
				return
			}
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
	wq.pauseMu.Lock()
	wq.paused = false
	wq.pause.Broadcast()
	wq.pauseMu.Unlock()
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
