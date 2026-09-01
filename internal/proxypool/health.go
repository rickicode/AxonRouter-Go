package proxypool

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"time"
)

const HealthInterval = 30 * time.Minute
const healthConcurrency = 4

// settingPoolGeoProbeEnabled gates whether the periodic health probe records
// egress geo (IP/country/org + flapping history) into the GeoCache. Manual
// "test pool" button always captures geo regardless of this setting.
const settingPoolGeoProbeEnabled = "pool_geo_probe_enabled"

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

// RunNow tests all active pools concurrently (bounded by healthConcurrency).
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

	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return []TestResult{}, false
	}

	// Parallel test with bounded concurrency
	sem := make(chan struct{}, healthConcurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	out := make([]TestResult, 0, len(ids))

	// The periodic probe records egress geo into the GeoCache only when the
	// operator enables it (default on). Manual test-pool always captures geo.
	recordGeo := h.geoProbeEnabled()

	for _, id := range ids {
		id := id
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			var (
				res TestResult
				err error
			)
			if recordGeo {
				res, err = TestPoolWithGeo(h.db, id)
			} else {
				res, err = TestPool(h.db, id)
			}
			if err == nil {
				mu.Lock()
				out = append(out, res)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return out, false
}

// geoProbeEnabled reads the pool_geo_probe_enabled setting via the checker's
// own DB handle (the package-level db.GetSetting only sees the global store).
func (h *HealthChecker) geoProbeEnabled() bool {
	if h.db == nil {
		return true
	}
	var v string
	err := h.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, settingPoolGeoProbeEnabled).Scan(&v)
	if err != nil {
		return true // default on
	}
	return strings.EqualFold(strings.TrimSpace(v), "true") || v == "1" || v == "yes"
}
