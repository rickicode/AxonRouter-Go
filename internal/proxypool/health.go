package proxypool

import (
	"context"
	"database/sql"
	"sync"
	"time"
)

const HealthInterval = 30 * time.Minute

type HealthChecker struct {
	db     *sql.DB
	mu     sync.Mutex
	run    bool
	last   string
	cancel context.CancelFunc
}

func NewHealthChecker(db *sql.DB) *HealthChecker { return &HealthChecker{db: db} }

func (h *HealthChecker) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	h.cancel = cancel
	go func() {
		t := time.NewTimer(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				h.RunNow()
				t.Reset(HealthInterval)
			}
		}
	}()
}

func (h *HealthChecker) Stop() {
	if h.cancel != nil {
		h.cancel()
	}
}

func (h *HealthChecker) Last() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.last
}

func (h *HealthChecker) RunNow() ([]TestResult, bool) {
	h.mu.Lock()
	if h.run {
		h.mu.Unlock()
		return nil, true
	}
	h.run = true
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		h.run = false
		h.last = time.Now().Format(time.RFC3339)
		h.mu.Unlock()
	}()

	rows, err := h.db.Query("SELECT id FROM proxy_pools WHERE is_active = 1")
	if err != nil {
		return nil, false
	}
	defer rows.Close()
	var out []TestResult
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			if res, err := TestPool(h.db, id); err == nil {
				out = append(out, res)
			}
		}
	}
	return out, false
}
