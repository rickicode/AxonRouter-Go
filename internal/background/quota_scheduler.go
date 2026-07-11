package background

import (
	"context"
	"database/sql"
	"log"
	"sync"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/quota"
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
// Default interval: 1 minute (matches OmniRoute REFRESH_INTERVAL_MS).
func NewQuotaScheduler(store *connstate.Store, elig *connstate.EligibilityManager, intervalMin int) *QuotaScheduler {
	if intervalMin <= 0 {
		intervalMin = 1
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
// Runs cooldown recovery + proactive quota fetching from provider APIs (Codex, Antigravity, Kiro).
// Default interval: 1 minute (matches OmniRoute REFRESH_INTERVAL_MS).
type QuotaSchedulerDB struct {
	once       sync.Once
	db         *sql.DB
	store      *connstate.Store
	elig       *connstate.EligibilityManager
	exhaustion *quota.ExhaustionCache
	interval   time.Duration
	stopCh     chan struct{}
}

// NewQuotaSchedulerDB creates a DB-aware quota scheduler.
// Default interval: 1 minute (matches OmniRoute REFRESH_INTERVAL_MS).
func NewQuotaSchedulerDB(database *sql.DB, store *connstate.Store, elig *connstate.EligibilityManager, intervalMin int, exhaustionCache *quota.ExhaustionCache) *QuotaSchedulerDB {
	if intervalMin <= 0 {
		intervalMin = 1
	}
	return &QuotaSchedulerDB{
		db:         database,
		store:      store,
		elig:       elig,
		exhaustion: exhaustionCache,
		interval:   time.Duration(intervalMin) * time.Minute,
		stopCh:     make(chan struct{}),
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
	// 1. Recover cooldown-expired connections (in-memory)
	now := time.Now()
	recovered := 0
	qs.store.RangeByConnID(func(connID string, cs *connstate.ConnectionState) bool {
		if cs.IsCooldownExpired() {
			cs.SetStatus(connstate.StatusReady, "")
			if qs.exhaustion != nil {
				qs.exhaustion.Clear(connID)
			}
			recovered++
		}
		return true
	})

	// 2. Query DB for connections with cooldown_until in the past
	rows, err := qs.db.Query(`
		SELECT id FROM connections
	WHERE status IN ('rate_limited', 'quota_exhausted', 'cooldown')
		  AND cooldown_until IS NOT NULL
		  AND cooldown_until <= ?
	`, now.Unix())
	if err == nil {
		defer rows.Close()
		var toRecover []string
		for rows.Next() {
			var id string
			rows.Scan(&id)
			toRecover = append(toRecover, id)
		}
		for _, id := range toRecover {
			qs.db.Exec(`UPDATE connections SET status = 'ready', cooldown_until = NULL, updated_at = ? WHERE id = ?`,
				now.Unix(), id)
			qs.store.UpdateStatus(id, connstate.StatusReady)
			if qs.exhaustion != nil {
				qs.exhaustion.Clear(id)
			}
		}
		recovered += len(toRecover)
	}

	// 3. Proactive quota fetch for OAuth connections
	qs.checkQuotas()

	// 4. Cleanup expired exhaustion marks
	if qs.exhaustion != nil {
		qs.exhaustion.Cleanup()
	}

	if recovered > 0 {
		log.Printf("background: recovered %d connections", recovered)
		qs.elig.Update(qs.store)
	}
}

// checkQuotas fetches quota from upstream provider APIs, saves to DB cache, and updates connection status.
func (qs *QuotaSchedulerDB) checkQuotas() {
	results, err := quota.FetchAllQuota(qs.db)
	if err != nil {
		log.Printf("background: quota fetch error: %v", err)
		return
	}

	// Save to quota_cache table for the UI
	quota.SaveQuotaCache(qs.db, results)

	// Update connection statuses based on quota data
	statusChanged := false
	for _, provider := range results {
		for _, conn := range provider.Connections {
			quota.UpdateConnectionQuotaStatus(qs.db, qs.store, qs.exhaustion, conn.ConnectionID, conn.Quotas, conn.Error, &statusChanged)
		}
	}

	if statusChanged {
		qs.elig.Update(qs.store)
	}
}

// Stop signals the scheduler to stop.
func (qs *QuotaSchedulerDB) Stop() {
	close(qs.stopCh)
}
