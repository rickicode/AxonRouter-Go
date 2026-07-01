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
	// 1. Recover cooldown-expired connections (in-memory)
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
		}
		recovered += len(toRecover)
	}

	// 3. Proactive quota fetch for OAuth connections
	qs.checkQuotas()

	if recovered > 0 {
		log.Printf("background: recovered %d connections", recovered)
		qs.elig.Update(qs.store)
	}
}

// checkQuotas fetches quota from upstream provider APIs and updates connection status.
func (qs *QuotaSchedulerDB) checkQuotas() {
	results, err := quota.FetchAllQuota(qs.db)
	if err != nil {
		log.Printf("background: quota fetch error: %v", err)
		return
	}

	statusChanged := false
	for _, provider := range results {
		for _, conn := range provider.Connections {
			if conn.Error != "" {
				continue
			}
			newStatus := evaluateQuotaStatus(conn.Quotas)
			qs.applyQuotaStatus(conn.ConnectionID, newStatus, &statusChanged)
		}
	}

	if statusChanged {
		qs.elig.Update(qs.store)
	}
}

// evaluateQuotaStatus determines the connection status from quota items.
// Returns "ready" if all non-unlimited quotas have remaining > 0,
// "quota_exhausted" if any non-unlimited quota is at 0%.
func evaluateQuotaStatus(quotas []quota.QuotaItem) string {
	if len(quotas) == 0 {
		return "ready"
	}
	hasExhausted := false
	for _, q := range quotas {
		if q.Unlimited {
			continue
		}
		if q.RemainingPct <= 0 {
			hasExhausted = true
		}
	}
	if hasExhausted {
		return "quota_exhausted"
	}
	return "ready"
}

// applyQuotaStatus updates DB and connstate if status changed.
func (qs *QuotaSchedulerDB) applyQuotaStatus(connID, newStatus string, changed *bool) {
	// Get current DB status
	var currentStatus string
	err := qs.db.QueryRow(`SELECT status FROM connections WHERE id = ?`, connID).Scan(&currentStatus)
	if err != nil {
		return
	}

	// Only update if status actually changed
	if currentStatus == newStatus {
		return
	}

	// Skip if connection is in a manual-only state (don't override user actions)
	switch currentStatus {
	case "disabled", "auth_failed", "suspended", "balance_empty":
		return
	}

	_, err = qs.db.Exec(`UPDATE connections SET status = ?, updated_at = ? WHERE id = ?`,
		newStatus, time.Now().Unix(), connID)
	if err != nil {
		log.Printf("background: failed to update connection %s status: %v", connID, err)
		return
	}

	// Sync connstate
	if cs := qs.store.Get(connID); cs != nil {
		cs.SetStatus(connstate.Status(newStatus), "")
	}
	*changed = true
	log.Printf("background: connection %s status: %s → %s", connID, currentStatus, newStatus)
}

// Stop signals the scheduler to stop.
func (qs *QuotaSchedulerDB) Stop() {
	close(qs.stopCh)
}
