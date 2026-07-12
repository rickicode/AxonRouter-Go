package usage

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/rickicode/AxonRouter-Go/internal/db"
	"github.com/rickicode/AxonRouter-Go/internal/errorcode"
)

// LogEntry represents a single request to be logged.
type LogEntry struct {
	Timestamp       int64
	ConnectionID    string
	ProviderTypeID  string
	ModelID         string
	ComboID         string
	Modality        string
	InputTokens     int64
	OutputTokens    int64
	ReasoningTokens int64
	CachedTokens    int64
	LatencyMs       int64
	StatusCode      int
	ErrorMessage    string
	CostUsd         float64
}

// Tracker is an async usage logger with channel-based buffering.
// When a WriteQueue is set via SetWriteQueue, all batch inserts are routed
// through the single writer goroutine, preserving the "one SQLite writer"
// invariant and preventing WAL write-lock contention with cooldown/ban writes.
type Tracker struct {
	buffer      chan *LogEntry
	db          *sql.DB
	writeQueue  *db.WriteQueue // optional; nil → direct DB writes (legacy)
	flushTicker *time.Ticker
	batchSize   int
	stopCh      chan struct{}
	dropped     atomic.Int64
}

// SetWriteQueue routes all batch inserts through the centralized WriteQueue.
// Must be called before any Log() calls to avoid race with flushLoop.
func (t *Tracker) SetWriteQueue(wq *db.WriteQueue) {
	t.writeQueue = wq
}

// NewTracker creates a new async usage tracker.
// Channel capacity: 10000, flush interval: 5s, batch size: 100.
func NewTracker(database *sql.DB) *Tracker {
	t := &Tracker{
		buffer:      make(chan *LogEntry, 10000),
		db:          database,
		flushTicker: time.NewTicker(5 * time.Second),
		batchSize:   100,
		stopCh:      make(chan struct{}),
	}
	go t.flushLoop()
	return t
}

// Log enqueues a log entry. Non-blocking — drops if buffer is full.
func (t *Tracker) Log(entry *LogEntry) {
	entry.Timestamp = time.Now().UnixMilli()
	if entry.Modality == "" {
		entry.Modality = "chat"
	}
	// Many error paths only know that the request failed. When the error text
	// carries a status code (e.g. "stream error 400: ..."), lift it into
	// StatusCode so filters and badges can display the real upstream code.
	if entry.StatusCode == 0 && entry.ErrorMessage != "" {
		entry.StatusCode = errorcode.FromString(entry.ErrorMessage)
	}
	if entry.CostUsd == 0 {
		entry.CostUsd = EstimateCost(entry.ModelID, entry.InputTokens, entry.OutputTokens, entry.ReasoningTokens, entry.CachedTokens)
	}
	select {
	case t.buffer <- entry:
	default:
		// Buffer full, drop
		t.dropped.Add(1)
	}
}

// flushLoop runs in a goroutine, flushing batches periodically.
func (t *Tracker) flushLoop() {
	batch := make([]*LogEntry, 0, t.batchSize)
	for {
		select {
		case entry := <-t.buffer:
			batch = append(batch, entry)
			if len(batch) >= t.batchSize {
				t.flushBatch(batch)
				batch = batch[:0]
			}
		case <-t.flushTicker.C:
			// Drain remaining
			for {
				select {
				case entry := <-t.buffer:
					batch = append(batch, entry)
					if len(batch) >= t.batchSize {
						t.flushBatch(batch)
						batch = batch[:0]
					}
				default:
					goto done
				}
			}
		done:
			if len(batch) > 0 {
				t.flushBatch(batch)
				batch = batch[:0]
			}
		case <-t.stopCh:
			// Drain and flush remaining
			for {
				select {
				case entry := <-t.buffer:
					batch = append(batch, entry)
				default:
					if len(batch) > 0 {
						t.flushBatch(batch)
					}
					return
				}
			}
		}
	}
}

// flushBatch writes a batch of entries to SQLite in a single transaction.
// When a WriteQueue is set, the entire batch is enqueued as a single WriteOp,
// preserving the "one SQLite writer at a time" invariant. Without a WriteQueue
// (legacy/tests), it writes directly.
func (t *Tracker) flushBatch(batch []*LogEntry) {
	if len(batch) == 0 {
		return
	}

	if t.writeQueue != nil {
		batchCopy := make([]*LogEntry, len(batch))
		copy(batchCopy, batch)
		t.writeQueue.EnqueueOrBlock(context.Background(), "tracker:flushBatch", func(d *sql.DB) error {
			return t.writeBatchDirect(d, batchCopy)
		})
		return
	}

	t.writeBatchDirect(t.db, batch)
}

// writeBatchDirect performs the actual batch insert in a single transaction.
func (t *Tracker) writeBatchDirect(database *sql.DB, batch []*LogEntry) error {
	tx, err := database.Begin()
	if err != nil {
		return fmt.Errorf("usage: begin tx: %w", err)
	}

	stmt, err := tx.Prepare(`INSERT INTO request_logs
		(id, timestamp, connection_id, provider_type_id, model_id, combo_id,
		 modality, input_tokens, output_tokens, reasoning_tokens, cached_tokens,
		 latency_ms, status_code, error_message, cost_usd, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("usage: prepare: %w", err)
	}
	defer stmt.Close()

	now := db.UnixNow()
	for _, e := range batch {
		connID := toNullString(e.ConnectionID)
		providerID := toNullString(e.ProviderTypeID)
		modelID := toNullString(e.ModelID)
		comboID := toNullString(e.ComboID)
		latency := sql.NullInt64{Int64: e.LatencyMs, Valid: e.LatencyMs > 0}
		statusCode := sql.NullInt64{Int64: int64(e.StatusCode), Valid: e.StatusCode > 0}
		errMsg := toNullString(e.ErrorMessage)

		if _, err := stmt.Exec(uuid.New().String(), e.Timestamp, connID, providerID, modelID, comboID,
			e.Modality, e.InputTokens, e.OutputTokens, e.ReasoningTokens, e.CachedTokens,
			latency, statusCode, errMsg, e.CostUsd, now); err != nil {
			log.Printf("usage: exec: %v", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("usage: commit: %w", err)
	}
	return nil
}

// Stop gracefully shuts down the tracker, flushing remaining entries.
func (t *Tracker) Stop() {
	close(t.stopCh)
	t.flushTicker.Stop()
}

// BufferLen returns the current buffer length (for monitoring).
func (t *Tracker) BufferLen() int {
	return len(t.buffer)
}

// Buffered returns the current number of usage events waiting to be flushed.
func (t *Tracker) Buffered() int {
	return len(t.buffer)
}

// Dropped returns the total number of usage events dropped because the buffer
// was full.
func (t *Tracker) Dropped() int64 {
	return t.dropped.Load()
}

func toNullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
