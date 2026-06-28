package background

import (
	"context"
	"database/sql"
	"log"
	"sync"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/connstate"
)

// QuotaScheduler periodically checks provider quotas and updates connection states.
type QuotaScheduler struct {
	once     sync.Once
	store    *connstate.Store
	elig     *connstate.EligibilityManager
	interval time.Duration
	stopCh   chan struct{}
}

// NewQuotaScheduler creates a new quota scheduler.
// Default interval: 30 minutes.
func NewQuotaScheduler(store *connstate.Store, elig *connstate.EligibilityManager, intervalMin int) *QuotaScheduler {
	if intervalMin <= 0 {
		intervalMin = 30
	}
	return &QuotaScheduler{
		store:    store,
		elig:     elig,
		interval: time.Duration(intervalMin) * time.Minute,
		stopCh:   make(chan struct{}),
	}
}

// Start launches the background goroutine (sync.Once).
func (qs *QuotaScheduler) Start(ctx context.Context) {
	qs.once.Do(func() {
		go qs.run(ctx)
	})
}

func (qs *QuotaScheduler) run(ctx context.Context) {
	log.Println("background: quota scheduler started")

	// Immediate first run
	qs.check()

	ticker := time.NewTicker(qs.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			qs.check()
		case <-ctx.Done():
			log.Println("background: quota scheduler stopped")
			return
		case <-qs.stopCh:
			log.Println("background: quota scheduler stopped")
			return
		}
	}
}

// check recovers cooldown-expired connections and recomputes eligibility.
func (qs *QuotaScheduler) check() {
	// Check for cooldown-expired connections
	now := time.Now()
	recovered := 0
	qs.store.RangeByConnID(func(connID string, cs *connstate.ConnectionState) bool {
		if cs.CooldownUntil != nil && now.After(*cs.CooldownUntil) {
			cs.SetStatus(connstate.StatusReady, "")
			recovered++
		}
		return true
	})

	if recovered > 0 {
		log.Printf("background: recovered %d connections from cooldown", recovered)
		qs.elig.Update(qs.store)
	}
}

// Stop signals the scheduler to stop.
func (qs *QuotaScheduler) Stop() {
	close(qs.stopCh)
}

// QuotaSchedulerDB is a version that also queries DB for proactive quota checks.
// ponytail: simple version that just does cooldown recovery.
// Real proactive quota checking (per-provider API calls) should be added when
// providers with quota APIs are integrated.
type QuotaSchedulerDB struct {
	once     sync.Once
	db       *sql.DB
	store    *connstate.Store
	elig     *connstate.EligibilityManager
	interval time.Duration
	stopCh   chan struct{}
}

// NewQuotaSchedulerDB creates a DB-aware quota scheduler.
func NewQuotaSchedulerDB(database *sql.DB, store *connstate.Store, elig *connstate.EligibilityManager, intervalMin int) *QuotaSchedulerDB {
	if intervalMin <= 0 {
		intervalMin = 30
	}
	return &QuotaSchedulerDB{
		db:       database,
		store:    store,
		elig:     elig,
		interval: time.Duration(intervalMin) * time.Minute,
		stopCh:   make(chan struct{}),
	}
}

// Start launches the background goroutine (sync.Once).
func (qs *QuotaSchedulerDB) Start(ctx context.Context) {
	qs.once.Do(func() {
		go qs.run(ctx)
	})
}

func (qs *QuotaSchedulerDB) run(ctx context.Context) {
	log.Println("background: quota scheduler (db) started")

	// Immediate first run
	qs.check()

	ticker := time.NewTicker(qs.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			qs.check()
		case <-ctx.Done():
			log.Println("background: quota scheduler (db) stopped")
			return
		case <-qs.stopCh:
			return
		}
	}
}

func (qs *QuotaSchedulerDB) check() {
	// 1. Recover cooldown-expired connections
	now := time.Now()
	recovered := 0
	qs.store.RangeByConnID(func(connID string, cs *connstate.ConnectionState) bool {
		if cs.CooldownUntil != nil && now.After(*cs.CooldownUntil) {
			cs.SetStatus(connstate.StatusReady, "")
			recovered++
		}
		return true
	})

	// 2. Query DB for connections with cooldown_until in the past
	rows, err := qs.db.Query(`
		SELECT id FROM connections 
		WHERE status IN ('rate_limited', 'quota_exhausted') 
		  AND cooldown_until IS NOT NULL 
		  AND cooldown_until <= ?
	`, time.Now().Unix())
	if err != nil {
		return
	}
	defer rows.Close()

	var toRecover []string
	for rows.Next() {
		var id string
		rows.Scan(&id)
		toRecover = append(toRecover, id)
	}

	for _, id := range toRecover {
		qs.db.Exec(`UPDATE connections SET status = 'ready', cooldown_until = NULL, updated_at = ? WHERE id = ?`,
			time.Now().Unix(), id)
	}

	totalRecovered := recovered + len(toRecover)
	if totalRecovered > 0 {
		log.Printf("background: recovered %d connections", totalRecovered)
		qs.elig.Update(qs.store)
	}
}

// Stop signals the scheduler to stop.
func (qs *QuotaSchedulerDB) Stop() {
	close(qs.stopCh)
}
