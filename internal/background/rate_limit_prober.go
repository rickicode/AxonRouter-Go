package background

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/db"
	"github.com/rickicode/AxonRouter-Go/internal/quota"
)

// RateLimitProber periodically checks connections in cooldown and resets them
// when they become available again (cooldown expired + probe succeeds).
type RateLimitProber struct {
	once       sync.Once
	db         *sql.DB
	writeQueue *db.WriteQueue
	store      *connstate.Store
	elig       *connstate.EligibilityManager
	exhaustion *quota.ExhaustionCache
	stopCh     chan struct{}
}

func NewRateLimitProber(
	database *sql.DB,
	writeQueue *db.WriteQueue,
	store *connstate.Store,
	elig *connstate.EligibilityManager,
	exhaustion *quota.ExhaustionCache,
) *RateLimitProber {
	return &RateLimitProber{
		db:         database,
		writeQueue: writeQueue,
		store:      store,
		elig:       elig,
		exhaustion: exhaustion,
		stopCh:     make(chan struct{}),
	}
}

func (p *RateLimitProber) Start(ctx context.Context) {
	p.once.Do(func() {
		go p.run(ctx)
	})
}

func (p *RateLimitProber) Stop() {
	close(p.stopCh)
}

func (p *RateLimitProber) run(ctx context.Context) {
	log.Println("background: rate-limit prober started (1 min interval)")
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.check()
		}
	}
}

// check finds oc connections with expired cooldown and probes them.
// Rows are loaded into a slice and the cursor is closed BEFORE any HTTP probes
// to avoid holding a pooled DB connection across network I/O.
func (p *RateLimitProber) check() {
	now := time.Now().Unix()
	rows, err := p.db.Query(`
		SELECT id, name
		FROM connections
		WHERE provider_type_id = 'oc'
		AND is_active = 1
		AND status IN ('rate_limited', 'quota_exhausted')
		AND cooldown_until IS NOT NULL
		AND cooldown_until <= ?
	`, now)
	if err != nil {
		return
	}

	// Scan all rows into a slice, then close the cursor immediately.
	type connRow struct{ id, name string }
	var candidates []connRow
	for rows.Next() {
		var r connRow
		if err := rows.Scan(&r.id, &r.name); err != nil {
			continue
		}
		candidates = append(candidates, r)
	}
	rows.Close()

	if len(candidates) == 0 {
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}

	for _, r := range candidates {
		// Probe OpenCode Free endpoint
		req, err := http.NewRequest("POST", "https://opencode.ai/zen/v1/chat/completions", nil)
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			// Connection recovered — reset connection-wide exhaustion only.
			// Per-model scoped marks expire via their own TTL so other models stay blocked.
			p.exhaustion.Clear(quota.ExhaustKey(r.id, ""))
			p.store.UpdateStatus(r.id, connstate.StatusReady)
			connID := r.id
			updatedAt := now
			p.writeQueue.Enqueue("rateLimitProber:recover", func(d *sql.DB) error {
				_, err := d.Exec(`UPDATE connections SET status='ready', cooldown_until=NULL, last_error=NULL, updated_at=? WHERE id=?`,
					updatedAt, connID)
				return err
			})
			p.elig.Update(p.store)
			log.Printf("rate-limit prober: %s (%s) recovered → ready", r.name, r.id[:8])
		}
	}
}
