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
// with a terminal status and are older than the manager's configured retention
// window. It returns the number of rows removed and any database error.
func (lm *LifecycleManager) Cleanup() (int64, error) {
	return lm.CleanupWithRetention(lm.retention)
}

// CleanupWithRetention runs a cleanup using the caller-provided retention
// window. A zero or negative retention deletes every eligible row regardless of
// age (use with caution). It returns the number of connection rows removed and
// any error. Child rows in model_rate_limits and combo_connections are removed
// first inside the same transaction so foreign-key constraints stay satisfied.
func (lm *LifecycleManager) CleanupWithRetention(retention time.Duration) (int64, error) {
	if lm.db == nil {
		return 0, nil
	}

	var where string
	args := []any{}
	if retention <= 0 {
		where = `is_active = 0 AND status IN ('disabled', 'auth_failed')`
	} else {
		where = `is_active = 0 AND status IN ('disabled', 'auth_failed') AND updated_at < unixepoch() - ?`
		args = append(args, int64(retention.Seconds()))
	}

	tx, err := lm.db.Begin()
	if err != nil {
		log.Printf("background: connection lifecycle cleanup tx begin error: %v", err)
		return 0, err
	}

	deleted, err := func() (int64, error) {
		// Delete child rows first to avoid FK violations.
		if _, err := tx.Exec(`
			DELETE FROM model_rate_limits
			WHERE connection_id IN (SELECT id FROM connections WHERE `+where+`)
		`, args...); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(`
			DELETE FROM combo_steps
			WHERE connection_id IN (SELECT id FROM connections WHERE `+where+`)
		`, args...); err != nil {
			return 0, err
		}

		result, err := tx.Exec(`DELETE FROM connections WHERE `+where, args...)
		if err != nil {
			return 0, err
		}
		return result.RowsAffected()
	}()

	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("background: connection lifecycle cleanup rollback error: %v", rbErr)
		}
		log.Printf("background: connection lifecycle cleanup error: %v", err)
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		log.Printf("background: connection lifecycle cleanup commit error: %v", err)
		return 0, err
	}

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
