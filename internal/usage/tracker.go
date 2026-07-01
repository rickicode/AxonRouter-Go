package usage

import (
	"database/sql"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/rickicode/AxonRouter-Go/internal/db"
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
type Tracker struct {
	buffer       chan *LogEntry
	db           *sql.DB
	flushTicker  *time.Ticker
	batchSize    int
	stopCh       chan struct{}
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
	if entry.CostUsd == 0 {
		entry.CostUsd = EstimateCost(entry.ModelID, entry.InputTokens, entry.OutputTokens, entry.ReasoningTokens)
	}
	select {
	case t.buffer <- entry:
	default:
		// Buffer full, drop
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
func (t *Tracker) flushBatch(batch []*LogEntry) {
	if len(batch) == 0 {
		return
	}

	tx, err := t.db.Begin()
	if err != nil {
		log.Printf("usage: begin tx: %v", err)
		return
	}

	stmt, err := tx.Prepare(`
		INSERT INTO request_logs (id, timestamp, connection_id, provider_type_id, model_id, combo_id,
			modality, input_tokens, output_tokens, reasoning_tokens, cached_tokens, latency_ms, status_code,
			error_message, cost_usd, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		tx.Rollback()
		log.Printf("usage: prepare: %v", err)
		return
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

		stmt.Exec(uuid.New().String(), e.Timestamp, connID, providerID, modelID, comboID,
			e.Modality, e.InputTokens, e.OutputTokens, e.ReasoningTokens, e.CachedTokens,
			latency, statusCode, errMsg, e.CostUsd, now)
	}

	if err := tx.Commit(); err != nil {
		log.Printf("usage: commit: %v", err)
	}
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

func toNullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
