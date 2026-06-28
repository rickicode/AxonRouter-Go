package background

import (
	"context"
	"database/sql"
	"log"
	"sync"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/combo"
)

// Cleanup performs periodic cleanup tasks:
// - Circuit breaker cleanup (every 5 min)
// - Old log deletion (daily)
type Cleanup struct {
	once        sync.Once
	handler     *combo.Handler
	db          *sql.DB
	interval    time.Duration
	logDays     int
	stopCh      chan struct{}
}

// NewCleanup creates a new cleanup goroutine.
// Circuit breaker cleanup: every 5 min. Log retention: configurable days.
func NewCleanup(handler *combo.Handler, database *sql.DB, logRetentionDays int) *Cleanup {
	if logRetentionDays <= 0 {
		logRetentionDays = 30
	}
	return &Cleanup{
		handler:  handler,
		db:       database,
		interval: 5 * time.Minute,
		logDays:  logRetentionDays,
		stopCh:   make(chan struct{}),
	}
}

// Start launches the background goroutine (sync.Once).
func (cl *Cleanup) Start(ctx context.Context) {
	cl.once.Do(func() {
		go cl.run(ctx)
	})
}

func (cl *Cleanup) run(ctx context.Context) {
	log.Println("background: cleanup started")

	// Immediate first run
	cl.cleanup()

	ticker := time.NewTicker(cl.interval)
	defer ticker.Stop()

	logCleanupTicker := time.NewTicker(24 * time.Hour)
	defer logCleanupTicker.Stop()

	for {
		select {
		case <-ticker.C:
			cl.cleanup()
		case <-logCleanupTicker.C:
			cl.cleanupOldLogs()
		case <-ctx.Done():
			log.Println("background: cleanup stopped")
			return
		case <-cl.stopCh:
			return
		}
	}
}

// cleanup removes stale circuit breakers.
func (cl *Cleanup) cleanup() {
	cl.handler.CleanupBreakers()
}

// cleanupOldLogs deletes logs older than the retention period.
func (cl *Cleanup) cleanupOldLogs() {
	cutoff := time.Now().AddDate(0, 0, -cl.logDays).UnixMilli()
	result, err := cl.db.Exec(`DELETE FROM request_logs WHERE timestamp < ?`, cutoff)
	if err != nil {
		log.Printf("background: log cleanup error: %v", err)
		return
	}
	deleted, _ := result.RowsAffected()
	if deleted > 0 {
		log.Printf("background: deleted %d old log entries", deleted)
	}
}

// Stop signals the cleanup to stop.
func (cl *Cleanup) Stop() {
	close(cl.stopCh)
}
