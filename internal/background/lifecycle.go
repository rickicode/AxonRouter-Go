package background

import (
	"context"
	"database/sql"
	"log"
	"sync"
	"time"
)

const defaultLifecycleCleanupInterval = 60 * time.Minute
const defaultLifecycleRetention = 7 * 24 * time.Hour

// LifecycleManager periodically garbage-collects terminal connection rows from
// the database. A connection is eligible for removal when it has been soft
// deleted (is_active=0), its terminal status is one that will not recover
// automatically ('disabled' or 'auth_failed'), and it has not been modified
// for the configured retention period. Each run issues a single DELETE so no
// row locks are held during iteration and no cross-table transactions are used.
type LifecycleManager struct {
	once      sync.Once
	stopOnce  sync.Once
	db        *sql.DB
	interval  time.Duration
	retention time.Duration
	stopCh    chan struct{}
}

// NewLifecycleManager creates a new connection lifecycle manager.
// intervalMin controls how often the cleanup runs; zero or negative defaults to 60.
func NewLifecycleManager(database *sql.DB, intervalMin int) *LifecycleManager {
	if intervalMin <= 0 {
		intervalMin = 60
	}
	return &LifecycleManager{
		db:        database,
		interval:  time.Duration(intervalMin) * time.Minute,
		retention: defaultLifecycleRetention,
		stopCh:    make(chan struct{}),
	}
}

// Start launches the background goroutine (sync.Once).
func (lm *LifecycleManager) Start(ctx context.Context) {
	lm.once.Do(func() {
		go lm.run(ctx)
	})
}

func (lm *LifecycleManager) run(ctx context.Context) {
	log.Println("background: lifecycle manager started")

	ticker := time.NewTicker(lm.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			lm.Cleanup()
		case <-ctx.Done():
			log.Println("background: lifecycle manager stopped")
			return
		case <-lm.stopCh:
			log.Println("background: lifecycle manager stopped")
			return
		}
	}
}

// Cleanup runs a single DELETE for connections that have been soft-deleted
// with a terminal status and are older than the retention window. It returns
// the number of rows removed and any database error.
func (lm *LifecycleManager) Cleanup() (int64, error) {
	if lm.db == nil {
		return 0, nil
	}
	result, err := lm.db.Exec(`
		DELETE FROM connections
		WHERE is_active = 0
		  AND status IN ('disabled', 'auth_failed')
		  AND updated_at < unixepoch() - ?
	`, int64(lm.retention.Seconds()))
	if err != nil {
		log.Printf("background: connection lifecycle cleanup error: %v", err)
		return 0, err
	}
	deleted, _ := result.RowsAffected()
	if deleted > 0 {
		log.Printf("background: deleted %d stale disabled/auth_failed connections", deleted)
	}
	return deleted, nil
}

// Stop signals the lifecycle manager to stop. Safe to call more than once.
func (lm *LifecycleManager) Stop() {
	lm.stopOnce.Do(func() {
		close(lm.stopCh)
	})
}
